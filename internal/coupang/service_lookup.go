package coupang

import (
	"context"
	"time"

	"github.com/shift-click/masterbot/internal/scraper/providers"
)

func (t *CoupangTracker) Lookup(ctx context.Context, rawURL string, opts ...LookupOption) (*CoupangLookupResult, error) {
	var options LookupOptions
	for _, o := range opts {
		o(&options)
	}

	cu, err := providers.ParseCoupangURL(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	product, readRefresh, registration, err := t.prepareLookupProduct(ctx, cu, options.AttachmentTitle)
	if err != nil {
		return nil, err
	}
	if product == nil {
		return &CoupangLookupResult{
			ReadRefresh:            readRefresh,
			RegistrationStage:      registration.Stage,
			ResponseMode:           registration.ResponseMode,
			RegistrationDeferred:   registration.RegistrationDeferred,
			RegistrationDeferredUI: registration.RegistrationDeferred || registration.SeedDeferred || registration.RescueDeferred,
			BudgetExhausted:        registration.BudgetExhausted,
			SeedDeferred:           registration.SeedDeferred,
			RescueDeferred:         registration.RescueDeferred,
		}, nil
	}

	// If seeding is in progress (async FetchChart), wait briefly so the first
	// response already has full chart history instead of showing empty stats.
	if registration.SeedDeferred {
		t.awaitSeedPrices(ctx, product.TrackID, 5*time.Second)
	}

	history, err := t.store.GetPriceHistory(ctx, product.TrackID, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		return nil, err
	}
	stats, err := t.store.GetPriceStats(ctx, product.TrackID)
	if err != nil {
		return nil, err
	}
	summary := summarizeHistory(history)

	result := &CoupangLookupResult{
		Product:                *product,
		History:                history,
		Stats:                  stats,
		SampleCount:            summary.SampleCount,
		DistinctDays:           summary.DistinctDays,
		HistorySpanDays:        summary.HistorySpanDays,
		StatsEligible:          summary.StatsEligible,
		IsStale:                t.isStale(*product, time.Now()),
		LastObservedAt:         product.Snapshot.LastSeenAt,
		ReadRefresh:            readRefresh,
		RegistrationStage:      registration.Stage,
		ResponseMode:           registration.ResponseMode,
		RegistrationDeferred:   registration.RegistrationDeferred,
		RegistrationDeferredUI: registration.RegistrationDeferred || registration.SeedDeferred || registration.RescueDeferred,
		BudgetExhausted:        registration.BudgetExhausted,
		SeedDeferred:           registration.SeedDeferred,
		RescueDeferred:         registration.RescueDeferred,
	}
	if result.IsStale {
		result.RefreshRequested = t.ScheduleRefresh(product.TrackID, "read_stale")
	}
	return result, nil
}

func (t *CoupangTracker) prepareLookupProduct(ctx context.Context, cu *providers.CoupangURL, attachmentTitle string) (*CoupangProductRecord, CoupangReadRefreshStatus, registrationSummary, error) {
	product, created, registration, err := t.ensureTracked(ctx, cu, attachmentTitle)
	if err != nil {
		return nil, CoupangReadRefreshNone, registration, err
	}
	if product == nil {
		return nil, CoupangReadRefreshNone, registration, nil
	}
	product, err = t.loadLookupProduct(ctx, cu, product, created)
	if err != nil {
		return nil, CoupangReadRefreshNone, registration, err
	}
	t.syncLookupProductTier(ctx, product)
	return t.finishLookupProduct(ctx, cu, product, registration)
}

func (t *CoupangTracker) loadLookupProduct(ctx context.Context, cu *providers.CoupangURL, product *CoupangProductRecord, created bool) (*CoupangProductRecord, error) {
	if created {
		return product, nil
	}
	if err := t.store.TouchProduct(ctx, product.TrackID, t.config.TierWindow); err != nil {
		return nil, err
	}
	return t.requireTrackedProduct(ctx, product.TrackID, "tracked product disappeared", cu)
}

func (t *CoupangTracker) syncLookupProductTier(ctx context.Context, product *CoupangProductRecord) {
	updatedTier := t.classifyTier(*product, time.Now())
	if updatedTier != product.Snapshot.Tier {
		if err := t.store.SetProductTier(ctx, product.TrackID, updatedTier); err == nil {
			product.Snapshot.Tier = updatedTier
		}
	}
}

func (t *CoupangTracker) finishLookupProduct(ctx context.Context, cu *providers.CoupangURL, product *CoupangProductRecord, registration registrationSummary) (*CoupangProductRecord, CoupangReadRefreshStatus, registrationSummary, error) {
	readRefresh := CoupangReadRefreshNotAttempted
	if !t.isStale(*product, time.Now()) {
		return product, readRefresh, registration, nil
	}

	readRefresh = t.tryReadThroughRefresh(ctx, product.TrackID)
	if readRefresh != CoupangReadRefreshSucceeded {
		return product, readRefresh, registration, nil
	}

	product, err := t.requireTrackedProduct(ctx, product.TrackID, "tracked product disappeared after read refresh", cu)
	if err != nil {
		return nil, CoupangReadRefreshNone, registration, err
	}
	return product, readRefresh, registration, nil
}

type historySummary struct {
	SampleCount     int
	DistinctDays    int
	HistorySpanDays int
	StatsEligible   bool
}

func summarizeHistory(history []PricePoint) historySummary {
	summary := historySummary{SampleCount: len(history)}
	if len(history) == 0 {
		return summary
	}

	seenDays := make(map[string]struct{}, len(history))
	firstDay := observationDay(history[0].FetchedAt)
	lastDay := firstDay
	for _, point := range history {
		day := observationDay(point.FetchedAt)
		dayKey := day.Format("2006-01-02")
		seenDays[dayKey] = struct{}{}
		if day.Before(firstDay) {
			firstDay = day
		}
		if day.After(lastDay) {
			lastDay = day
		}
	}

	summary.DistinctDays = len(seenDays)
	summary.HistorySpanDays = int(lastDay.Sub(firstDay).Hours()/24) + 1
	summary.StatsEligible = summary.SampleCount >= 3 && summary.DistinctDays >= 3 && summary.HistorySpanDays >= 3
	return summary
}

func observationDay(t time.Time) time.Time {
	utc := t.UTC()
	year, month, day := utc.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// awaitSeedPrices polls HasSeedPrices until seed chart data is available or
// timeout expires. Called after new registration with SeedDeferred=true so
// the first response already includes full historical stats.
func (t *CoupangTracker) awaitSeedPrices(ctx context.Context, trackID string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		has, err := t.store.HasSeedPrices(ctx, trackID)
		if err != nil || has {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}
