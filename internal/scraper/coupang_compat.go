package scraper

import (
	"context"
	"log/slog"

	"github.com/shift-click/masterbot/internal/coupang"
	"github.com/shift-click/masterbot/internal/scraper/providers"
)

type CoupangTracker = coupang.CoupangTracker
type CoupangTrackerConfig = coupang.CoupangTrackerConfig
type CoupangLookupResult = coupang.CoupangLookupResult
type CoupangReadRefreshStatus = coupang.CoupangReadRefreshStatus

const (
	CoupangReadRefreshNone             = coupang.CoupangReadRefreshNone
	CoupangReadRefreshNotAttempted     = coupang.CoupangReadRefreshNotAttempted
	CoupangReadRefreshSucceeded        = coupang.CoupangReadRefreshSucceeded
	CoupangReadRefreshTimedOut         = coupang.CoupangReadRefreshTimedOut
	CoupangReadRefreshFailed           = coupang.CoupangReadRefreshFailed
	CoupangReadRefreshNoVerifiedSource = coupang.CoupangReadRefreshNoVerifiedSource
	CoupangReadRefreshBudgetExhausted  = coupang.CoupangReadRefreshBudgetExhausted
	CoupangReadRefreshInFlight         = coupang.CoupangReadRefreshInFlight
)

var (
	ErrCoupangRegistrationLimited = coupang.ErrCoupangRegistrationLimited
	ErrCoupangRefreshLimited      = coupang.ErrCoupangRefreshLimited
)

func NewCoupangTracker(
	priceStore coupang.Repository,
	coupangScraper interface {
		FetchCurrent(ctx context.Context, cu *providers.CoupangURL) (*providers.CoupangProduct, error)
	},
	fallcent interface {
		ResolveProduct(ctx context.Context, cu *providers.CoupangURL, keywords []string) (*providers.FallcentProductData, error)
		FetchProduct(ctx context.Context, fallcentProductID string) (*providers.FallcentProductData, error)
		FetchChart(ctx context.Context, fallcentProductID string) ([]int, error)
		LookupByCoupangID(ctx context.Context, productID, itemID string) (*providers.FallcentProductData, error)
	},
	naverTitle interface {
		ResolveTitle(ctx context.Context, productID string) (string, error)
	},
	cfg CoupangTrackerConfig,
	logger *slog.Logger,
) *CoupangTracker {
	return coupang.NewCoupangTracker(priceStore, coupangScraper, fallcent, naverTitle, cfg, logger)
}
