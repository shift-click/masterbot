package scraper

import (
	"context"
	"log/slog"
	"time"

	"github.com/shift-click/masterbot/internal/coupang"
)

// CoupangWatchlistConfig holds background scheduling configuration for tracked products.
type CoupangWatchlistConfig struct {
	CollectInterval time.Duration
	IdleTimeout     time.Duration
	EvictInterval   time.Duration
	MaxProducts     int
}

// DefaultCoupangWatchlistConfig returns sensible defaults.
func DefaultCoupangWatchlistConfig() CoupangWatchlistConfig {
	return CoupangWatchlistConfig{
		CollectInterval: 15 * time.Minute,
		IdleTimeout:     30 * 24 * time.Hour,
		EvictInterval:   1 * time.Hour,
		MaxProducts:     10000,
	}
}

// CoupangWatchlist manages periodic refresh and eviction for tracked products.
type CoupangWatchlist struct {
	tracker *coupang.CoupangTracker
	config  CoupangWatchlistConfig
	logger  *slog.Logger
}

// NewCoupangWatchlist creates a new CoupangWatchlist.
func NewCoupangWatchlist(
	tracker *coupang.CoupangTracker,
	cfg CoupangWatchlistConfig,
	logger *slog.Logger,
) *CoupangWatchlist {
	if logger == nil {
		logger = slog.Default()
	}
	return &CoupangWatchlist{
		tracker: tracker,
		config:  cfg,
		logger:  logger.With("component", "coupang_watchlist"),
	}
}

// Start begins background collection and eviction loops. Blocks until ctx is cancelled.
func (w *CoupangWatchlist) Start(ctx context.Context) {
	w.logger.Info("coupang watchlist started",
		"collect_interval", w.config.CollectInterval,
		"idle_timeout", w.config.IdleTimeout,
		"max_products", w.config.MaxProducts,
	)
	if err := w.tracker.WarmupStale(ctx); err != nil {
		w.logger.Error("startup warmup sweep failed", "error", err)
	} else {
		w.logger.Info("startup warmup sweep completed")
	}
	if err := w.tracker.BackfillSeeds(ctx); err != nil {
		w.logger.Error("seed backfill failed", "error", err)
	}

	collectTicker := time.NewTicker(w.config.CollectInterval)
	evictTicker := time.NewTicker(w.config.EvictInterval)
	defer collectTicker.Stop()
	defer evictTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("coupang watchlist stopped")
			return

		case <-collectTicker.C:
			if err := w.tracker.RefreshDue(ctx); err != nil {
				w.logger.Error("scheduled refresh sweep failed", "error", err)
			}

		case <-evictTicker.C:
			count, err := w.tracker.EvictStale(ctx)
			if err != nil {
				w.logger.Error("failed to evict stale products", "error", err)
				continue
			}
			if err := w.tracker.EnforceCapacity(ctx); err != nil {
				w.logger.Error("failed to enforce capacity", "error", err)
			}
			if count > 0 {
				w.logger.Info("evicted stale products", "count", count)
			}
		}
	}
}
