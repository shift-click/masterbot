package coupang

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/metrics"
	"github.com/shift-click/masterbot/internal/scraper/providers"
)

type fakeRepository struct {
	product        *CoupangProductRecord
	mapping        *CoupangSourceMapping
	watched        []CoupangProductRecord
	hasSeed        bool
	insertSeedArgs []int
	evicted        int
}

func (f *fakeRepository) UpsertProduct(context.Context, CoupangProductRecord) error { return nil }
func (f *fakeRepository) UpdateProductMetadata(context.Context, CoupangProductRecord) error {
	return nil
}
func (f *fakeRepository) GetProduct(context.Context, string) (*CoupangProductRecord, error) {
	return f.product, nil
}
func (f *fakeRepository) GetProductByBaseProductID(context.Context, string) (*CoupangProductRecord, error) {
	return f.product, nil
}
func (f *fakeRepository) GetSourceMapping(context.Context, string) (*CoupangSourceMapping, error) {
	return f.mapping, nil
}
func (f *fakeRepository) UpsertSourceMapping(context.Context, CoupangSourceMapping) error { return nil }
func (f *fakeRepository) MarkSourceMappingState(_ context.Context, _ string, state CoupangSourceMappingState, failureReason string) error {
	if f.mapping == nil {
		f.mapping = &CoupangSourceMapping{}
	}
	f.mapping.State = state
	f.mapping.LastFailureReason = failureReason
	return nil
}
func (f *fakeRepository) TouchProduct(context.Context, string, time.Duration) error { return nil }
func (f *fakeRepository) ListWatchedProducts(context.Context, time.Duration) ([]CoupangProductRecord, error) {
	return f.watched, nil
}
func (f *fakeRepository) EvictStaleProducts(context.Context, time.Duration) (int, error) {
	return f.evicted, nil
}
func (f *fakeRepository) DeleteProducts(context.Context, []string) (int, error) { return 0, nil }
func (f *fakeRepository) CountTrackedProducts(context.Context) (int, error)     { return 0, nil }
func (f *fakeRepository) InsertPrice(context.Context, string, int, bool) error  { return nil }
func (f *fakeRepository) InsertSeedPrices(_ context.Context, _ string, prices []int) error {
	f.insertSeedArgs = append([]int(nil), prices...)
	return nil
}
func (f *fakeRepository) ReplaceSeedPrices(context.Context, string, []int) error { return nil }
func (f *fakeRepository) HasSeedPrices(context.Context, string) (bool, error)    { return f.hasSeed, nil }
func (f *fakeRepository) MarkChartBackfillAt(context.Context, string, time.Time) error {
	return nil
}
func (f *fakeRepository) GetPriceHistory(context.Context, string, time.Time) ([]PricePoint, error) {
	return nil, nil
}
func (f *fakeRepository) GetPriceStats(context.Context, string) (*PriceStats, error) { return nil, nil }
func (f *fakeRepository) UpdateSnapshot(context.Context, CoupangSnapshot) error      { return nil }
func (f *fakeRepository) MarkRefreshState(context.Context, string, time.Time, bool) error {
	return nil
}
func (f *fakeRepository) SetProductTier(context.Context, string, CoupangRefreshTier) error {
	return nil
}
func (f *fakeRepository) Close() error { return nil }

func TestPrepareSweepRefreshBudgetExhausted(t *testing.T) {
	t.Parallel()

	tracker := NewCoupangTracker(&fakeRepository{}, nil, nil, nil, internalTrackerTestConfig(), nil)
	tracker.refreshes = newBudgetWindow(0, time.Hour)

	ok := tracker.prepareSweepRefresh(time.Now(), CoupangProductRecord{TrackID: "track-1"}, "test")
	if ok {
		t.Fatal("expected sweep refresh to be rejected when budget is exhausted")
	}
	if leader, _ := tracker.beginRefreshFlight("track-1"); !leader {
		t.Fatal("expected refresh slot to be released after budget exhaustion")
	}
	tracker.finishRefresh("track-1")
}

func TestHandleRefreshFetchErrorMarksAccessBlocked(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		product: &CoupangProductRecord{TrackID: "track-2", ProductID: "product-2"},
		mapping: &CoupangSourceMapping{TrackID: "track-2", State: CoupangSourceMappingVerified},
	}
	tracker := NewCoupangTracker(repo, nil, nil, nil, internalTrackerTestConfig(), nil)

	err := tracker.handleRefreshFetchError(context.Background(), "track-2", "manual", repo.product, errors.New("fetch product page: HTTP 403 from https://www.coupang.com/vp/products/track-2"))
	if err != nil {
		t.Fatalf("handleRefreshFetchError: %v", err)
	}
	if repo.mapping.State != CoupangSourceMappingFailed {
		t.Fatalf("mapping state = %s, want failed", repo.mapping.State)
	}
	if repo.mapping.LastFailureReason != coupangAccessBlockedReason {
		t.Fatalf("mapping failure reason = %q", repo.mapping.LastFailureReason)
	}
}

func TestResolveReadRefreshWaitResult(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		product: &CoupangProductRecord{
			TrackID:   "track-3",
			ProductID: "product-3",
			Snapshot: CoupangSnapshot{
				LastSeenAt: time.Now(),
			},
		},
	}
	tracker := NewCoupangTracker(repo, nil, nil, nil, internalTrackerTestConfig(), nil)
	if got := tracker.resolveReadRefreshWaitResult(context.Background(), "track-3", nil, 10*time.Millisecond); got != CoupangReadRefreshSucceeded {
		t.Fatalf("nil wait error status = %s", got)
	}
	if got := tracker.resolveReadRefreshWaitResult(context.Background(), "track-3", context.DeadlineExceeded, 10*time.Millisecond); got != CoupangReadRefreshTimedOut {
		t.Fatalf("deadline wait error status = %s", got)
	}
	if got := tracker.resolveReadRefreshWaitResult(context.Background(), "track-3", errors.New("boom"), 10*time.Millisecond); got != CoupangReadRefreshFailed {
		t.Fatalf("generic wait error status = %s", got)
	}
}

func TestWaitRegistrationFlightReturnsDeferredWithoutProduct(t *testing.T) {
	t.Parallel()

	tracker := NewCoupangTracker(&fakeRepository{}, nil, nil, nil, internalTrackerTestConfig(), nil)
	flight := &registrationFlight{
		done: make(chan struct{}),
		result: registrationSummary{
			RegistrationDeferred: true,
			ResponseMode:         CoupangResponseModeRegistrationDeferred,
		},
	}
	close(flight.done)

	product, result, err := tracker.waitRegistrationFlight(context.Background(), "track-deferred", flight)
	if err != nil {
		t.Fatalf("waitRegistrationFlight: %v", err)
	}
	if product != nil {
		t.Fatalf("product = %#v, want nil", product)
	}
	if !result.RegistrationDeferred {
		t.Fatalf("result = %#v, want deferred", result)
	}
}

func TestWaitRegistrationFlightReturnsExistingProductAfterTimeout(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		product: &CoupangProductRecord{TrackID: "track-timeout", ProductID: "product-timeout"},
	}
	cfg := internalTrackerTestConfig()
	cfg.RegistrationJoinWait = time.Millisecond
	tracker := NewCoupangTracker(repo, nil, nil, nil, cfg, nil)
	flight := &registrationFlight{done: make(chan struct{})}

	product, result, err := tracker.waitRegistrationFlight(context.Background(), "track-timeout", flight)
	if err != nil {
		t.Fatalf("waitRegistrationFlight: %v", err)
	}
	if product == nil || product.TrackID != "track-timeout" {
		t.Fatalf("product = %#v, want tracked product", product)
	}
	if result.ResponseMode != CoupangResponseModeTextFirst {
		t.Fatalf("response mode = %s, want text_first", result.ResponseMode)
	}
}

type fakeBackfillRepository struct {
	fakeRepository
	replaceCalled    bool
	markBackfillTime time.Time
}

func (f *fakeBackfillRepository) ReplaceSeedPrices(_ context.Context, _ string, _ []int) error {
	f.replaceCalled = true
	return nil
}

func (f *fakeBackfillRepository) MarkChartBackfillAt(_ context.Context, _ string, at time.Time) error {
	f.markBackfillTime = at
	return nil
}

type fakeChartFetcher struct {
	chartPrices []int
	chartErr    error
}

func (f *fakeChartFetcher) ResolveProduct(context.Context, *providers.CoupangURL, []string) (*providers.FallcentProductData, error) {
	return nil, nil
}
func (f *fakeChartFetcher) FetchProduct(context.Context, string) (*providers.FallcentProductData, error) {
	return nil, nil
}
func (f *fakeChartFetcher) FetchChart(_ context.Context, _ string) ([]int, error) {
	return f.chartPrices, f.chartErr
}
func (f *fakeChartFetcher) LookupByCoupangID(context.Context, string, string) (*providers.FallcentProductData, error) {
	return nil, nil
}

type fakeRecorder struct {
	events []metrics.Event
}

func (f *fakeRecorder) Record(_ context.Context, event metrics.Event) {
	f.events = append(f.events, event)
}

func TestMaybeBackfillChartTriggersWhenIntervalElapsed(t *testing.T) {
	t.Parallel()

	repo := &fakeBackfillRepository{
		fakeRepository: fakeRepository{
			product: &CoupangProductRecord{
				TrackID: "bf-1",
				SourceMapping: CoupangSourceMapping{
					TrackID:           "bf-1",
					FallcentProductID: "fc-bf-1",
					State:             CoupangSourceMappingVerified,
					// LastChartBackfillAt zero → should trigger
				},
			},
		},
	}
	fetcher := &fakeChartFetcher{chartPrices: []int{10000, 11000, 12000}}
	cfg := internalTrackerTestConfig()
	cfg.ChartBackfillInterval = time.Millisecond // very short for test

	tracker := NewCoupangTracker(repo, nil, fetcher, nil, cfg, nil)
	tracker.maybeBackfillChart(context.Background(), "bf-1", repo.product)

	// Wait briefly for the goroutine
	time.Sleep(100 * time.Millisecond)

	if !repo.replaceCalled {
		t.Fatal("expected ReplaceSeedPrices to be called")
	}
	if repo.markBackfillTime.IsZero() {
		t.Fatal("expected MarkChartBackfillAt to be called")
	}
}

func TestWithAttachmentTitleAppliesOption(t *testing.T) {
	t.Parallel()

	var opts LookupOptions
	WithAttachmentTitle("미리보기 제목")(&opts)
	if opts.AttachmentTitle != "미리보기 제목" {
		t.Fatalf("attachment title = %q", opts.AttachmentTitle)
	}
}

func TestSetMetricsRecorderReplacesRecorder(t *testing.T) {
	t.Parallel()

	tracker := NewCoupangTracker(&fakeRepository{}, nil, nil, nil, internalTrackerTestConfig(), nil)
	recorder := &fakeRecorder{}
	tracker.SetMetricsRecorder(recorder)
	if tracker.recorder != recorder {
		t.Fatal("expected metrics recorder to be replaced")
	}
}

func TestBackfillSeedsInsertsVerifiedMissingSeed(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		watched: []CoupangProductRecord{{
			TrackID: "track-seed",
			SourceMapping: CoupangSourceMapping{
				TrackID:           "track-seed",
				FallcentProductID: "fc-seed",
				State:             CoupangSourceMappingVerified,
			},
		}},
	}
	fetcher := &fakeChartFetcher{chartPrices: []int{10000, 11000, 12000}}
	tracker := NewCoupangTracker(repo, nil, fetcher, nil, internalTrackerTestConfig(), nil)

	if err := tracker.BackfillSeeds(context.Background()); err != nil {
		t.Fatalf("BackfillSeeds: %v", err)
	}
	if len(repo.insertSeedArgs) != 3 {
		t.Fatalf("inserted seed prices = %v, want 3 entries", repo.insertSeedArgs)
	}
}

func TestBackfillProductSeedSkipsWhenSeedAlreadyExists(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{hasSeed: true}
	fetcher := &fakeChartFetcher{chartPrices: []int{10000}}
	tracker := NewCoupangTracker(repo, nil, fetcher, nil, internalTrackerTestConfig(), nil)

	inserted, err := tracker.backfillProductSeed(context.Background(), CoupangProductRecord{
		TrackID: "track-seed",
		SourceMapping: CoupangSourceMapping{
			TrackID:           "track-seed",
			FallcentProductID: "fc-seed",
			State:             CoupangSourceMappingVerified,
		},
	})
	if err != nil {
		t.Fatalf("backfillProductSeed: %v", err)
	}
	if inserted {
		t.Fatal("expected seed backfill to skip existing seed history")
	}
}

func TestEvictStaleReturnsRepositoryCount(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{evicted: 4}
	tracker := NewCoupangTracker(repo, nil, nil, nil, internalTrackerTestConfig(), nil)

	count, err := tracker.EvictStale(context.Background())
	if err != nil {
		t.Fatalf("EvictStale: %v", err)
	}
	if count != 4 {
		t.Fatalf("evicted count = %d, want 4", count)
	}
}

func TestMaybeBackfillChartSkipsWhenIntervalNotElapsed(t *testing.T) {
	t.Parallel()

	repo := &fakeBackfillRepository{
		fakeRepository: fakeRepository{
			product: &CoupangProductRecord{
				TrackID: "bf-2",
				SourceMapping: CoupangSourceMapping{
					TrackID:             "bf-2",
					FallcentProductID:   "fc-bf-2",
					State:               CoupangSourceMappingVerified,
					LastChartBackfillAt: time.Now(), // just now → should skip
				},
			},
		},
	}
	fetcher := &fakeChartFetcher{chartPrices: []int{10000}}
	cfg := internalTrackerTestConfig()
	cfg.ChartBackfillInterval = 72 * time.Hour

	tracker := NewCoupangTracker(repo, nil, fetcher, nil, cfg, nil)
	tracker.maybeBackfillChart(context.Background(), "bf-2", repo.product)

	time.Sleep(100 * time.Millisecond)

	if repo.replaceCalled {
		t.Fatal("expected ReplaceSeedPrices NOT to be called when interval not elapsed")
	}
}

func TestMaybeBackfillChartDoesNotMarkOnFailure(t *testing.T) {
	t.Parallel()

	repo := &fakeBackfillRepository{
		fakeRepository: fakeRepository{
			product: &CoupangProductRecord{
				TrackID: "bf-3",
				SourceMapping: CoupangSourceMapping{
					TrackID:           "bf-3",
					FallcentProductID: "fc-bf-3",
					State:             CoupangSourceMappingVerified,
				},
			},
		},
	}
	fetcher := &fakeChartFetcher{chartErr: errors.New("chart api down")}
	cfg := internalTrackerTestConfig()
	cfg.ChartBackfillInterval = time.Millisecond

	tracker := NewCoupangTracker(repo, nil, fetcher, nil, cfg, nil)
	tracker.maybeBackfillChart(context.Background(), "bf-3", repo.product)

	time.Sleep(100 * time.Millisecond)

	if !repo.markBackfillTime.IsZero() {
		t.Fatal("expected MarkChartBackfillAt NOT to be called on failure")
	}
}

func internalTrackerTestConfig() CoupangTrackerConfig {
	return CoupangTrackerConfig{
		CollectInterval:           time.Minute,
		IdleTimeout:               24 * time.Hour,
		MaxProducts:               100,
		HotInterval:               time.Minute,
		WarmInterval:              2 * time.Minute,
		ColdInterval:              5 * time.Minute,
		Freshness:                 30 * time.Minute,
		StaleThreshold:            time.Hour,
		MinRefreshInterval:        time.Minute,
		RefreshBudgetPerHour:      10,
		RegistrationBudgetPerHour: 10,
		ResolutionBudgetPerHour:   10,
		TierWindow:                time.Hour,
		HotThreshold:              10,
		WarmThreshold:             3,
		CandidateFanout:           3,
		MappingRecheckBackoff:     time.Hour,
		AllowAuxiliaryFallback:    true,
		RegistrationLatencyBudget: 200 * time.Millisecond,
		ReadRefreshTimeout:        200 * time.Millisecond,
		LookupCoalescingEnabled:   true,
		RegistrationJoinWait:      100 * time.Millisecond,
		ReadRefreshJoinWait:       100 * time.Millisecond,
		ChartBackfillInterval:     72 * time.Hour,
	}
}
