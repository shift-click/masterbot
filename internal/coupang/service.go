package coupang

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/shift-click/masterbot/internal/metrics"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"golang.org/x/sync/errgroup"
)

var (
	ErrCoupangRegistrationLimited = errors.New("coupang registration budget exceeded")
	ErrCoupangRefreshLimited      = errors.New("coupang refresh budget exceeded")
)

const coupangAccessBlockedReason = "coupang access blocked (HTTP 403)"
const coupangProxyHint = "configure coupang.scraper_proxy_url"

type CoupangTrackerConfig struct {
	CollectInterval           time.Duration
	IdleTimeout               time.Duration
	MaxProducts               int
	HotInterval               time.Duration
	WarmInterval              time.Duration
	ColdInterval              time.Duration
	Freshness                 time.Duration
	StaleThreshold            time.Duration
	MinRefreshInterval        time.Duration
	RefreshBudgetPerHour      int
	RegistrationBudgetPerHour int
	ResolutionBudgetPerHour   int
	TierWindow                time.Duration
	HotThreshold              int
	WarmThreshold             int
	CandidateFanout           int
	MappingRecheckBackoff     time.Duration
	AllowAuxiliaryFallback    bool
	RegistrationLatencyBudget time.Duration
	ReadRefreshTimeout        time.Duration
	LookupCoalescingEnabled   bool
	RegistrationJoinWait      time.Duration
	ReadRefreshJoinWait       time.Duration
	ChartBackfillInterval     time.Duration
}

type CoupangRegistrationStage string

const (
	CoupangRegistrationStageNone           CoupangRegistrationStage = ""
	CoupangRegistrationStageDirect         CoupangRegistrationStage = "direct_resolved"
	CoupangRegistrationStageRescue         CoupangRegistrationStage = "rescue_resolved"
	CoupangRegistrationStageDeferred       CoupangRegistrationStage = "deferred"
	CoupangRegistrationStageAsyncStarted   CoupangRegistrationStage = "async_started"
	CoupangRegistrationStageAsyncSucceeded CoupangRegistrationStage = "async_succeeded"
	CoupangRegistrationStageAsyncFailed    CoupangRegistrationStage = "async_failed"
	CoupangRegistrationStageSeedStarted    CoupangRegistrationStage = "seed_started"
	CoupangRegistrationStageSeedSucceeded  CoupangRegistrationStage = "seed_succeeded"
	CoupangRegistrationStageSeedFailed     CoupangRegistrationStage = "seed_failed"
)

type CoupangResponseMode string

const (
	CoupangResponseModeNone                 CoupangResponseMode = ""
	CoupangResponseModeStandard             CoupangResponseMode = "standard"
	CoupangResponseModeTextFirst            CoupangResponseMode = "text_first"
	CoupangResponseModeRegistrationDeferred CoupangResponseMode = "registration_deferred"
)

type CoupangReadRefreshStatus string

const (
	CoupangReadRefreshNone             CoupangReadRefreshStatus = ""
	CoupangReadRefreshNotAttempted     CoupangReadRefreshStatus = "not_attempted"
	CoupangReadRefreshSucceeded        CoupangReadRefreshStatus = "succeeded"
	CoupangReadRefreshTimedOut         CoupangReadRefreshStatus = "timed_out"
	CoupangReadRefreshFailed           CoupangReadRefreshStatus = "failed"
	CoupangReadRefreshNoVerifiedSource CoupangReadRefreshStatus = "no_verified_source"
	CoupangReadRefreshBudgetExhausted  CoupangReadRefreshStatus = "budget_exhausted"
	CoupangReadRefreshInFlight         CoupangReadRefreshStatus = "in_flight"
)

type CoupangTracker struct {
	store       Repository
	scraper     coupangAuxFetcher
	fallcent    fallcentFetcher
	naverTitle  naverTitleFetcher
	config      CoupangTrackerConfig
	logger      *slog.Logger
	refreshes   *budgetWindow
	regs        *budgetWindow
	resolutions *budgetWindow
	recorder    metrics.Recorder

	asyncGroup *errgroup.Group  // lifecycle-managed async work
	asyncCtx   context.Context  // derived from app context, cancelled on shutdown

	mu sync.Mutex
	// refreshFlights tracks in-flight refresh jobs (scheduled/read-through).
	refreshFlights map[string]*refreshFlight
	// registrationFlights coalesces first-time registration by track_id.
	registrationFlights map[string]*registrationFlight
}

type coupangAuxFetcher interface {
	FetchCurrent(ctx context.Context, cu *providers.CoupangURL) (*providers.CoupangProduct, error)
}

type fallcentFetcher interface {
	ResolveProduct(ctx context.Context, cu *providers.CoupangURL, keywords []string) (*providers.FallcentProductData, error)
	FetchProduct(ctx context.Context, fallcentProductID string) (*providers.FallcentProductData, error)
	FetchChart(ctx context.Context, fallcentProductID string) ([]int, error)
	LookupByCoupangID(ctx context.Context, productID, itemID string) (*providers.FallcentProductData, error)
}

type naverTitleFetcher interface {
	ResolveTitle(ctx context.Context, productID string) (string, error)
}

type resolvedProduct struct {
	Name              string
	ImageURL          string
	Price             int
	ItemID            string
	VendorItemID      string
	ComparativeMin    int
	RefreshSource     string
	FallcentProductID string
	SearchKeyword     string
	MappingState      CoupangSourceMappingState
}

type refreshFlight struct {
	done chan struct{}
}

type registrationFlight struct {
	done    chan struct{}
	product *CoupangProductRecord
	result  registrationSummary
	err     error
}

type registrationSummary struct {
	Stage                CoupangRegistrationStage
	ResponseMode         CoupangResponseMode
	RegistrationDeferred bool
	BudgetExhausted      bool
	SeedDeferred         bool
	RescueDeferred       bool
}

func NewCoupangTracker(
	priceStore Repository,
	coupangScraper coupangAuxFetcher,
	fallcent fallcentFetcher,
	naverTitle naverTitleFetcher,
	cfg CoupangTrackerConfig,
	logger *slog.Logger,
) *CoupangTracker {
	if logger == nil {
		logger = slog.Default()
	}
	return &CoupangTracker{
		store:               priceStore,
		scraper:             coupangScraper,
		fallcent:            fallcent,
		naverTitle:          naverTitle,
		config:              cfg,
		logger:              logger.With("component", "coupang_tracker"),
		refreshes:           newBudgetWindow(cfg.RefreshBudgetPerHour, time.Hour),
		regs:                newBudgetWindow(cfg.RegistrationBudgetPerHour, time.Hour),
		resolutions:         newBudgetWindow(cfg.ResolutionBudgetPerHour, time.Hour),
		refreshFlights:      make(map[string]*refreshFlight),
		registrationFlights: make(map[string]*registrationFlight),
	}
}

func (t *CoupangTracker) SetMetricsRecorder(recorder metrics.Recorder) {
	t.recorder = recorder
}

// InitAsyncGroup sets up the errgroup for lifecycle-managed async work.
// The errgroup context is derived from the given parent context, so all
// async goroutines will observe cancellation on shutdown.
func (t *CoupangTracker) InitAsyncGroup(ctx context.Context) {
	t.asyncGroup, t.asyncCtx = errgroup.WithContext(ctx)
	t.asyncGroup.SetLimit(10)
}

// WaitAsync blocks until all async goroutines managed by the errgroup have
// completed. Safe to call even if InitAsyncGroup was never called.
func (t *CoupangTracker) WaitAsync() error {
	if t.asyncGroup == nil {
		return nil
	}
	return t.asyncGroup.Wait()
}

// goAsync launches fn in the errgroup if available, otherwise falls back to a
// plain goroutine. This ensures tests that don't call InitAsyncGroup still work.
func (t *CoupangTracker) goAsync(fn func()) {
	if t.asyncGroup != nil {
		t.asyncGroup.Go(func() error {
			fn()
			return nil // never propagate — avoid cancelling sibling work
		})
		return
	}
	go fn()
}

// asyncContext returns the errgroup-derived context if available, otherwise
// context.Background(). Used by async goroutines so they respect shutdown.
func (t *CoupangTracker) asyncContext() context.Context {
	if t.asyncCtx != nil {
		return t.asyncCtx
	}
	return context.Background()
}

func trackProductID(cu *providers.CoupangURL) string {
	if cu == nil {
		return ""
	}
	if cu.VendorItemID != "" {
		return cu.ProductID + "#v:" + cu.VendorItemID
	}
	if cu.ItemID != "" {
		return cu.ProductID + "#i:" + cu.ItemID
	}
	return cu.ProductID
}

// LookupOptions holds optional parameters for Lookup.
type LookupOptions struct {
	AttachmentTitle string // link preview title from KakaoTalk attachment
}

// LookupOption configures optional Lookup behavior.
type LookupOption func(*LookupOptions)

// WithAttachmentTitle provides a link preview title extracted from
// the KakaoTalk message attachment to use as a Fallcent search keyword.
func WithAttachmentTitle(title string) LookupOption {
	return func(o *LookupOptions) {
		o.AttachmentTitle = title
	}
}
