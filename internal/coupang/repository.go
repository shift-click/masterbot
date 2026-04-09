package coupang

import (
	"context"
	"time"
)

type Repository interface {
	UpsertProduct(ctx context.Context, p CoupangProductRecord) error
	UpdateProductMetadata(ctx context.Context, p CoupangProductRecord) error
	GetProduct(ctx context.Context, productID string) (*CoupangProductRecord, error)
	GetProductByBaseProductID(ctx context.Context, baseProductID string) (*CoupangProductRecord, error)
	GetSourceMapping(ctx context.Context, productID string) (*CoupangSourceMapping, error)
	UpsertSourceMapping(ctx context.Context, mapping CoupangSourceMapping) error
	MarkSourceMappingState(ctx context.Context, productID string, state CoupangSourceMappingState, failureReason string) error
	TouchProduct(ctx context.Context, productID string, queryWindow time.Duration) error
	ListWatchedProducts(ctx context.Context, since time.Duration) ([]CoupangProductRecord, error)
	EvictStaleProducts(ctx context.Context, olderThan time.Duration) (int, error)
	DeleteProducts(ctx context.Context, productIDs []string) (int, error)
	CountTrackedProducts(ctx context.Context) (int, error)

	InsertPrice(ctx context.Context, productID string, price int, isSeed bool) error
	InsertSeedPrices(ctx context.Context, productID string, prices []int) error
	ReplaceSeedPrices(ctx context.Context, productID string, prices []int) error
	HasSeedPrices(ctx context.Context, productID string) (bool, error)
	MarkChartBackfillAt(ctx context.Context, productID string, at time.Time) error
	GetPriceHistory(ctx context.Context, productID string, since time.Time) ([]PricePoint, error)
	GetPriceStats(ctx context.Context, productID string) (*PriceStats, error)

	UpdateSnapshot(ctx context.Context, snapshot CoupangSnapshot) error
	MarkRefreshState(ctx context.Context, productID string, attemptedAt time.Time, inFlight bool) error
	SetProductTier(ctx context.Context, productID string, tier CoupangRefreshTier) error

	Close() error
}
