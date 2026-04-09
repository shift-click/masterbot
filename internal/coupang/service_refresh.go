package coupang

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/shift-click/masterbot/internal/metrics"
	"github.com/shift-click/masterbot/internal/scraper/providers"
)

func (t *CoupangTracker) ScheduleRefresh(productID, reason string) bool {
	if !t.beginRefresh(productID) {
		return false
	}
	if !t.refreshes.Allow(time.Now()) {
		t.finishRefresh(productID)
		t.logger.Debug("refresh skipped: budget exhausted", "product_id", productID, "reason", reason)
		return false
	}

	t.goAsync(func() {
		ctx, cancel := context.WithTimeout(t.asyncContext(), 20*time.Second)
		defer cancel()
		if err := t.refreshProduct(ctx, productID, t.config.AllowAuxiliaryFallback, reason); err != nil {
			t.logger.Warn("async refresh failed", "product_id", productID, "reason", reason, "error", err)
		}
	})
	return true
}

func (t *CoupangTracker) RefreshDue(ctx context.Context) error {
	return t.refreshSweep(ctx, func(product CoupangProductRecord, now time.Time) bool {
		return t.dueForRefresh(product, now)
	}, "scheduled")
}

func (t *CoupangTracker) ClassifyTier(product CoupangProductRecord, now time.Time) CoupangRefreshTier {
	return t.classifyTier(product, now)
}

func (t *CoupangTracker) RefreshProduct(ctx context.Context, trackID string, allowFallback bool, reason string) error {
	if !t.beginRefresh(trackID) {
		return nil
	}
	return t.refreshProduct(ctx, trackID, allowFallback, reason)
}

func (t *CoupangTracker) BackfillSeeds(ctx context.Context) error {
	if t.fallcent == nil {
		return nil
	}
	products, err := t.store.ListWatchedProducts(ctx, t.config.IdleTimeout)
	if err != nil {
		return err
	}
	var seeded int
	for _, product := range products {
		inserted, insertErr := t.backfillProductSeed(ctx, product)
		if insertErr != nil {
			t.logger.Debug("seed backfill failed", "product_id", product.ProductID, "error", insertErr)
			continue
		}
		if inserted {
			seeded++
			t.logger.Info("seed backfill succeeded", "product_id", product.ProductID)
		}
	}
	if seeded > 0 {
		t.logger.Info("seed backfill completed", "seeded", seeded, "total", len(products))
	}
	return nil
}

func (t *CoupangTracker) backfillProductSeed(ctx context.Context, product CoupangProductRecord) (bool, error) {
	if product.SourceMapping.FallcentProductID == "" || product.SourceMapping.State != CoupangSourceMappingVerified {
		return false, nil
	}
	hasSeed, err := t.store.HasSeedPrices(ctx, product.TrackID)
	if err != nil || hasSeed {
		return false, err
	}
	prices, err := t.fallcent.FetchChart(ctx, product.SourceMapping.FallcentProductID)
	if err != nil {
		return false, err
	}
	if len(prices) == 0 {
		return false, nil
	}
	if err := t.store.InsertSeedPrices(ctx, product.TrackID, prices); err != nil {
		return false, err
	}
	return true, nil
}

func (t *CoupangTracker) maybeBackfillChart(ctx context.Context, trackID string, product *CoupangProductRecord) {
	if t.fallcent == nil || t.config.ChartBackfillInterval <= 0 {
		return
	}
	if product == nil || product.SourceMapping.FallcentProductID == "" {
		return
	}
	if product.SourceMapping.State != CoupangSourceMappingVerified {
		return
	}
	if !product.SourceMapping.LastChartBackfillAt.IsZero() &&
		time.Since(product.SourceMapping.LastChartBackfillAt) < t.config.ChartBackfillInterval {
		return
	}

	t.goAsync(func() {
		bgCtx, cancel := context.WithTimeout(t.asyncContext(), 20*time.Second)
		defer cancel()

		prices, err := t.fallcent.FetchChart(bgCtx, product.SourceMapping.FallcentProductID)
		if err != nil {
			t.logger.Debug("chart backfill fetch failed", "track_id", trackID, "error", err)
			return
		}
		if len(prices) == 0 {
			return
		}
		if err := t.store.ReplaceSeedPrices(bgCtx, trackID, prices); err != nil {
			t.logger.Debug("chart backfill replace failed", "track_id", trackID, "error", err)
			return
		}
		if err := t.store.MarkChartBackfillAt(bgCtx, trackID, time.Now()); err != nil {
			t.logger.Debug("chart backfill mark failed", "track_id", trackID, "error", err)
			return
		}
		t.logger.Info("chart backfill succeeded", "track_id", trackID, "points", len(prices))
	})
}

func (t *CoupangTracker) WarmupStale(ctx context.Context) error {
	return t.refreshSweep(ctx, func(product CoupangProductRecord, now time.Time) bool {
		if product.Snapshot.RefreshInFlight {
			return false
		}
		if t.mappingBackoffActive(product, now) {
			return false
		}
		if !product.Snapshot.LastRefreshAttemptAt.IsZero() && now.Sub(product.Snapshot.LastRefreshAttemptAt) < t.config.MinRefreshInterval {
			return false
		}
		return t.isStale(product, now)
	}, "startup_warmup")
}

func (t *CoupangTracker) refreshSweep(ctx context.Context, shouldRefresh func(CoupangProductRecord, time.Time) bool, reason string) error {
	products, err := t.store.ListWatchedProducts(ctx, t.config.IdleTimeout)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, product := range products {
		product = t.syncProductTier(ctx, product, now)
		if !shouldRefresh(product, now) {
			continue
		}
		if !t.prepareSweepRefresh(now, product, reason) {
			continue
		}
		if err := t.refreshProduct(ctx, product.TrackID, t.config.AllowAuxiliaryFallback, reason); err != nil {
			t.reportSweepRefreshError(reason, product, err)
		}
	}
	return nil
}

func (t *CoupangTracker) syncProductTier(ctx context.Context, product CoupangProductRecord, now time.Time) CoupangProductRecord {
	tier := t.classifyTier(product, now)
	if tier != product.Snapshot.Tier {
		if err := t.store.SetProductTier(ctx, product.TrackID, tier); err == nil {
			product.Snapshot.Tier = tier
		}
	}
	return product
}

func (t *CoupangTracker) prepareSweepRefresh(now time.Time, product CoupangProductRecord, reason string) bool {
	if !t.beginRefresh(product.TrackID) {
		return false
	}
	if t.refreshes.Allow(now) {
		return true
	}
	t.finishRefresh(product.TrackID)
	t.logger.Debug(reason+" refresh skipped: budget exhausted", "track_id", product.TrackID, "product_id", product.ProductID)
	return false
}

func (t *CoupangTracker) reportSweepRefreshError(reason string, product CoupangProductRecord, err error) {
	if isCoupangAccessBlocked(err) {
		t.logger.Info(reason+" refresh deferred: coupang access blocked", "track_id", product.TrackID, "product_id", product.ProductID, "error", err, "hint", coupangProxyHint)
		return
	}
	t.logger.Warn(reason+" refresh failed", "track_id", product.TrackID, "product_id", product.ProductID, "error", err)
}

func (t *CoupangTracker) EvictStale(ctx context.Context) (int, error) {
	return t.store.EvictStaleProducts(ctx, t.config.IdleTimeout)
}

func (t *CoupangTracker) EnforceCapacity(ctx context.Context) error {
	return t.enforceCapacity(ctx, 0)
}

func (t *CoupangTracker) refreshProduct(ctx context.Context, trackID string, allowFallback bool, reason string) error {
	defer t.finishRefresh(trackID)
	t.recordRefreshEvent(ctx, metrics.EventCoupangRefreshStarted, trackID, reason, "", nil)

	product, err := t.loadRefreshProduct(ctx, trackID, reason)
	if err != nil {
		return err
	}

	now := time.Now()
	if err := t.markRefreshState(ctx, trackID, now, true, reason); err != nil {
		return err
	}
	defer t.clearRefreshState(trackID)

	cu := refreshURLForProduct(product)
	refreshed, err := t.fetchRefresh(ctx, cu, product, allowFallback)
	if err != nil {
		return t.handleRefreshFetchError(ctx, trackID, reason, product, err)
	}

	if err := t.applyRefreshedProduct(ctx, trackID, product, refreshed, now); err != nil {
		t.recordRefreshFailure(ctx, trackID, reason, err)
		return err
	}
	t.recordRefreshSuccess(ctx, trackID, reason, refreshed)
	t.logger.Info("refresh succeeded", "track_id", trackID, "product_id", product.ProductID, "reason", reason, "source", refreshed.RefreshSource, "price", refreshed.Price)
	if refreshed.MappingState == CoupangSourceMappingVerified {
		t.maybeBackfillChart(ctx, trackID, product)
	}
	return nil
}

func (t *CoupangTracker) loadRefreshProduct(ctx context.Context, trackID, reason string) (*CoupangProductRecord, error) {
	product, err := t.store.GetProduct(ctx, trackID)
	if err != nil {
		t.recordRefreshFailure(ctx, trackID, reason, err)
		return nil, err
	}
	if product != nil {
		return product, nil
	}
	err = fmt.Errorf("refresh missing product: %s", trackID)
	t.recordRefreshFailure(ctx, trackID, reason, err)
	return nil, err
}

func (t *CoupangTracker) markRefreshState(ctx context.Context, trackID string, at time.Time, inFlight bool, reason string) error {
	if err := t.store.MarkRefreshState(ctx, trackID, at, inFlight); err != nil {
		t.recordRefreshFailure(ctx, trackID, reason, err)
		return err
	}
	return nil
}

func (t *CoupangTracker) clearRefreshState(trackID string) {
	_ = t.store.MarkRefreshState(context.Background(), trackID, time.Now(), false)
}

func refreshURLForProduct(product *CoupangProductRecord) *providers.CoupangURL {
	return &providers.CoupangURL{
		ProductID:    product.ProductID,
		ItemID:       product.ItemID,
		VendorItemID: product.VendorItemID,
	}
}

func (t *CoupangTracker) handleRefreshFetchError(ctx context.Context, trackID, reason string, product *CoupangProductRecord, err error) error {
	if !isCoupangAccessBlocked(err) {
		t.recordRefreshFailure(ctx, trackID, reason, err)
		return err
	}
	_ = t.store.MarkSourceMappingState(ctx, trackID, CoupangSourceMappingFailed, coupangAccessBlockedReason)
	t.recordRefreshEvent(ctx, metrics.EventCoupangRefreshFailed, trackID, reason, "access_blocked", map[string]any{
		"reason": reason,
		"error":  err.Error(),
		"hint":   coupangProxyHint,
	})
	t.logger.Info("refresh deferred: coupang access blocked", "track_id", trackID, "product_id", product.ProductID, "reason", reason, "error", err, "hint", coupangProxyHint)
	return nil
}

func (t *CoupangTracker) applyRefreshedProduct(ctx context.Context, trackID string, product *CoupangProductRecord, refreshed *resolvedProduct, now time.Time) error {
	product.Name = firstNonEmpty(refreshed.Name, product.Name)
	product.ImageURL = firstNonEmpty(refreshed.ImageURL, product.ImageURL)
	product.ItemID = firstNonEmpty(product.ItemID, refreshed.ItemID)
	product.VendorItemID = firstNonEmpty(product.VendorItemID, refreshed.VendorItemID)
	if err := t.store.UpdateProductMetadata(ctx, *product); err != nil {
		return err
	}
	if err := t.store.UpdateSnapshot(ctx, CoupangSnapshot{
		TrackID:              trackID,
		Price:                refreshed.Price,
		LastSeenAt:           now,
		LastRefreshAttemptAt: now,
		RefreshSource:        refreshed.RefreshSource,
		Tier:                 t.classifyTier(*product, now),
		RefreshInFlight:      false,
	}); err != nil {
		return err
	}
	if refreshed.Price > 0 {
		if err := t.store.InsertPrice(ctx, trackID, refreshed.Price, false); err != nil {
			return err
		}
	}
	if refreshed.FallcentProductID != "" || refreshed.SearchKeyword != "" || refreshed.ComparativeMin > 0 || refreshed.MappingState != CoupangSourceMappingUnknown {
		mapping := product.SourceMapping
		mapping.TrackID = trackID
		mapping.FallcentProductID = firstNonEmpty(refreshed.FallcentProductID, mapping.FallcentProductID)
		mapping.SearchKeyword = firstNonEmpty(refreshed.SearchKeyword, mapping.SearchKeyword)
		mapping.ComparativeMinPrice = maxInt(refreshed.ComparativeMin, mapping.ComparativeMinPrice)
		mapping.State = refreshed.MappingState
		if mapping.State == CoupangSourceMappingVerified {
			mapping.VerifiedAt = now
			mapping.FailureCount = 0
			mapping.LastFailureReason = ""
		}
		if err := t.store.UpsertSourceMapping(ctx, mapping); err != nil {
			return err
		}
	}
	return nil
}

func (t *CoupangTracker) tryReadThroughRefresh(ctx context.Context, trackID string) CoupangReadRefreshStatus {
	product, err := t.store.GetProduct(ctx, trackID)
	if err != nil {
		t.logger.Warn("read-through refresh skipped: failed to load product", "track_id", trackID, "error", err)
		return CoupangReadRefreshFailed
	}
	if product == nil || !t.canReadThroughRefresh(*product) {
		return CoupangReadRefreshNoVerifiedSource
	}
	leader, done := t.beginRefreshFlight(trackID)
	if !leader {
		return t.handleReadRefreshFollower(ctx, trackID, done)
	}
	t.recordCoalescingSignal(ctx, trackID, "read_refresh", true, 0, false)
	return t.runReadRefreshLeader(ctx, trackID)
}

func (t *CoupangTracker) handleReadRefreshFollower(ctx context.Context, trackID string, done <-chan struct{}) CoupangReadRefreshStatus {
	if !t.coalescingEnabled() {
		return CoupangReadRefreshInFlight
	}
	waitStart := time.Now()
	waitErr := waitForDone(ctx, done, t.readRefreshJoinWait())
	waited := time.Since(waitStart)
	return t.resolveReadRefreshWaitResult(ctx, trackID, waitErr, waited)
}

func (t *CoupangTracker) resolveReadRefreshWaitResult(ctx context.Context, trackID string, waitErr error, waited time.Duration) CoupangReadRefreshStatus {
	switch {
	case waitErr == nil:
		t.recordCoalescingSignal(ctx, trackID, "read_refresh", false, waited, false)
		return t.readRefreshStatusFromLatest(ctx, trackID)
	case errors.Is(waitErr, context.DeadlineExceeded):
		t.logger.Warn("read-through join wait timed out", "track_id", trackID, "wait", waited)
		t.recordCoalescingSignal(ctx, trackID, "read_refresh", false, waited, true)
		return CoupangReadRefreshTimedOut
	default:
		t.logger.Warn("read-through join wait failed", "track_id", trackID, "error", waitErr)
		return CoupangReadRefreshFailed
	}
}

func (t *CoupangTracker) readRefreshStatusFromLatest(ctx context.Context, trackID string) CoupangReadRefreshStatus {
	latest, err := t.store.GetProduct(ctx, trackID)
	if err != nil {
		t.logger.Warn("read-through join refresh load failed", "track_id", trackID, "error", err)
		return CoupangReadRefreshFailed
	}
	if latest != nil && !t.isStale(*latest, time.Now()) {
		return CoupangReadRefreshSucceeded
	}
	return CoupangReadRefreshFailed
}

func (t *CoupangTracker) runReadRefreshLeader(ctx context.Context, trackID string) CoupangReadRefreshStatus {
	now := time.Now()
	if !t.refreshes.Allow(now) {
		t.finishRefresh(trackID)
		return CoupangReadRefreshBudgetExhausted
	}

	timeout := t.config.ReadRefreshTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	refreshCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	err := t.readThroughRefresh(refreshCtx, trackID)
	switch {
	case err == nil:
		return CoupangReadRefreshSucceeded
	case errors.Is(err, context.DeadlineExceeded), errors.Is(refreshCtx.Err(), context.DeadlineExceeded):
		t.logger.Warn("read-through refresh timed out", "track_id", trackID, "timeout", timeout)
		return CoupangReadRefreshTimedOut
	default:
		t.logger.Warn("read-through refresh failed", "track_id", trackID, "error", err)
		return CoupangReadRefreshFailed
	}
}

func (t *CoupangTracker) canReadThroughRefresh(product CoupangProductRecord) bool {
	return t.fallcent != nil &&
		product.SourceMapping.FallcentProductID != "" &&
		product.SourceMapping.State == CoupangSourceMappingVerified
}

func (t *CoupangTracker) readThroughRefresh(ctx context.Context, trackID string) error {
	defer t.finishRefresh(trackID)
	t.recordRefreshEvent(ctx, metrics.EventCoupangRefreshStarted, trackID, "read_through", "", nil)

	product, err := t.store.GetProduct(ctx, trackID)
	if err != nil {
		t.recordRefreshFailure(ctx, trackID, "read_through", err)
		return err
	}
	if product == nil {
		err := fmt.Errorf("read-through refresh missing product: %s", trackID)
		t.recordRefreshFailure(ctx, trackID, "read_through", err)
		return err
	}
	if !t.canReadThroughRefresh(*product) {
		err := fmt.Errorf("read-through refresh missing verified mapping: %s", trackID)
		t.recordRefreshFailure(ctx, trackID, "read_through", err)
		return err
	}

	now := time.Now()
	if err := t.store.MarkRefreshState(ctx, trackID, now, true); err != nil {
		t.recordRefreshFailure(ctx, trackID, "read_through", err)
		return err
	}
	defer func() {
		_ = t.store.MarkRefreshState(context.Background(), trackID, time.Now(), false)
	}()

	cu := refreshURLForProduct(product)
	refreshed, err := t.fetchVerifiedRefresh(ctx, cu, product)
	if err != nil {
		t.recordRefreshFailure(ctx, trackID, "read_through", err)
		return err
	}
	if err := t.applyRefreshedProduct(ctx, trackID, product, refreshed, now); err != nil {
		t.recordRefreshFailure(ctx, trackID, "read_through", err)
		return err
	}
	t.recordRefreshSuccess(ctx, trackID, "read_through", refreshed)
	if refreshed.MappingState == CoupangSourceMappingVerified {
		t.maybeBackfillChart(ctx, trackID, product)
	}
	return nil
}

func (t *CoupangTracker) fetchVerifiedRefresh(ctx context.Context, cu *providers.CoupangURL, product *CoupangProductRecord) (*resolvedProduct, error) {
	if product == nil || !t.canReadThroughRefresh(*product) {
		return nil, fmt.Errorf("verified read refresh unavailable")
	}

	detail, err := t.fallcent.FetchProduct(ctx, product.SourceMapping.FallcentProductID)
	if err == nil && detail != nil && providers.FallcentMatchesCoupang(cu, detail) {
		return &resolvedProduct{
			Name:              detail.Name,
			ImageURL:          detail.ImageURL,
			Price:             detail.Price,
			ItemID:            firstNonEmpty(product.ItemID, detail.ItemID),
			VendorItemID:      firstNonEmpty(product.VendorItemID, detail.VendorItemID),
			ComparativeMin:    detail.LowestPrice,
			RefreshSource:     "fallcent_read_through",
			FallcentProductID: detail.FallcentProductID,
			SearchKeyword:     product.SourceMapping.SearchKeyword,
			MappingState:      CoupangSourceMappingVerified,
		}, nil
	}
	if err == nil && detail == nil {
		err = fmt.Errorf("fallcent detail was empty")
	}
	t.logger.Warn("fallcent read-through verification failed", "track_id", product.TrackID, "fallcent_product_id", product.SourceMapping.FallcentProductID, "error", err)
	_ = t.store.MarkSourceMappingState(ctx, product.TrackID, CoupangSourceMappingNeedsRecheck, "read-through detail verification failed")
	return nil, err
}

func (t *CoupangTracker) isStale(product CoupangProductRecord, now time.Time) bool {
	if product.Snapshot.LastSeenAt.IsZero() {
		return true
	}
	return now.Sub(product.Snapshot.LastSeenAt) > t.config.Freshness
}

func (t *CoupangTracker) dueForRefresh(product CoupangProductRecord, now time.Time) bool {
	if product.Snapshot.RefreshInFlight {
		return false
	}
	if t.mappingBackoffActive(product, now) {
		return false
	}
	if !product.Snapshot.LastRefreshAttemptAt.IsZero() && now.Sub(product.Snapshot.LastRefreshAttemptAt) < t.config.MinRefreshInterval {
		return false
	}
	if product.Snapshot.LastSeenAt.IsZero() {
		return true
	}
	interval := t.intervalForTier(product.Snapshot.Tier)
	if product.SourceMapping.State == CoupangSourceMappingNeedsRecheck && t.config.MappingRecheckBackoff > interval {
		interval = t.config.MappingRecheckBackoff
	}
	return now.Sub(product.Snapshot.LastSeenAt) >= interval
}

func (t *CoupangTracker) mappingBackoffActive(product CoupangProductRecord, now time.Time) bool {
	if product.SourceMapping.State != CoupangSourceMappingFailed || product.SourceMapping.FailureCount <= 0 {
		return false
	}
	if product.Snapshot.LastRefreshAttemptAt.IsZero() {
		return false
	}
	backoff := t.config.MappingRecheckBackoff
	if backoff <= 0 {
		return false
	}
	return now.Sub(product.Snapshot.LastRefreshAttemptAt) < backoff
}

func (t *CoupangTracker) intervalForTier(tier CoupangRefreshTier) time.Duration {
	switch tier {
	case CoupangTierHot:
		return t.config.HotInterval
	case CoupangTierCold:
		return t.config.ColdInterval
	default:
		return t.config.WarmInterval
	}
}

func (t *CoupangTracker) classifyTier(product CoupangProductRecord, now time.Time) CoupangRefreshTier {
	if product.LastQueried.IsZero() || now.Sub(product.LastQueried) > 2*t.config.TierWindow {
		return CoupangTierCold
	}
	if product.RecentQueryCount >= t.config.HotThreshold {
		return CoupangTierHot
	}
	if product.RecentQueryCount >= t.config.WarmThreshold {
		return CoupangTierWarm
	}
	return CoupangTierCold
}

func (t *CoupangTracker) enforceCapacity(ctx context.Context, reserve int) error {
	count, err := t.store.CountTrackedProducts(ctx)
	if err != nil {
		return err
	}
	if count+reserve <= t.config.MaxProducts {
		return nil
	}

	products, err := t.store.ListWatchedProducts(ctx, 365*24*time.Hour)
	if err != nil {
		return err
	}
	excess := count + reserve - t.config.MaxProducts
	if excess <= 0 {
		return nil
	}

	var evictIDs []string
	for i := len(products) - 1; i >= 0 && len(evictIDs) < excess; i-- {
		evictIDs = append(evictIDs, products[i].TrackID)
	}
	if len(evictIDs) == 0 {
		return fmt.Errorf("capacity exceeded and no products available to evict")
	}
	_, err = t.store.DeleteProducts(ctx, evictIDs)
	return err
}

func (t *CoupangTracker) beginRefresh(productID string) bool {
	leader, _ := t.beginRefreshFlight(productID)
	return leader
}

func (t *CoupangTracker) beginRefreshFlight(productID string) (bool, <-chan struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if flight, exists := t.refreshFlights[productID]; exists {
		return false, flight.done
	}
	flight := &refreshFlight{done: make(chan struct{})}
	t.refreshFlights[productID] = flight
	return true, flight.done
}

func (t *CoupangTracker) finishRefresh(productID string) {
	t.mu.Lock()
	flight, exists := t.refreshFlights[productID]
	if exists {
		delete(t.refreshFlights, productID)
	}
	t.mu.Unlock()
	if exists {
		close(flight.done)
	}
}

func (t *CoupangTracker) readRefreshJoinWait() time.Duration {
	if t.config.ReadRefreshJoinWait > 0 {
		return t.config.ReadRefreshJoinWait
	}
	if t.config.ReadRefreshTimeout > 0 {
		return t.config.ReadRefreshTimeout
	}
	return 2 * time.Second
}

func waitForDone(ctx context.Context, done <-chan struct{}, wait time.Duration) error {
	if done == nil {
		return nil
	}
	if wait <= 0 {
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-done:
		return nil
	case <-timer.C:
		return context.DeadlineExceeded
	case <-ctx.Done():
		return ctx.Err()
	}
}

type budgetWindow struct {
	limit   int
	window  time.Duration
	mu      sync.Mutex
	history []time.Time
}

func newBudgetWindow(limit int, window time.Duration) *budgetWindow {
	return &budgetWindow{limit: limit, window: window}
}

func (b *budgetWindow) Allow(now time.Time) bool {
	if b.limit <= 0 {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	cutoff := now.Add(-b.window)
	kept := b.history[:0]
	for _, ts := range b.history {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	b.history = kept
	if len(b.history) >= b.limit {
		return false
	}
	b.history = append(b.history, now)
	return true
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
