package coupang

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shift-click/masterbot/internal/scraper/providers"
)

func (t *CoupangTracker) ensureTracked(ctx context.Context, cu *providers.CoupangURL, attachmentTitle string) (*CoupangProductRecord, bool, registrationSummary, error) {
	trackID := trackProductID(cu)
	product, err := t.loadTrackedProduct(ctx, trackID, cu)
	if err != nil {
		return nil, false, registrationSummary{}, err
	}
	if product != nil {
		return product, false, registrationSummary{ResponseMode: CoupangResponseModeStandard}, nil
	}

	if !t.coalescingEnabled() {
		return t.ensureTrackedDirect(ctx, trackID, cu, attachmentTitle)
	}
	return t.ensureTrackedWithCoalescing(ctx, trackID, cu, attachmentTitle)
}

func (t *CoupangTracker) loadTrackedProduct(ctx context.Context, trackID string, cu *providers.CoupangURL) (*CoupangProductRecord, error) {
	product, err := t.store.GetProduct(ctx, trackID)
	if err != nil {
		return nil, err
	}
	if product == nil && cu != nil && cu.VendorItemID == "" && cu.ItemID == "" {
		// Mobile URLs omit itemId/vendorItemId, producing a bare productID TrackID that
		// won't match a record registered via a desktop URL with a suffixed TrackID.
		// Fall back to base_product_id lookup to avoid duplicate registration.
		product, err = t.store.GetProductByBaseProductID(ctx, cu.ProductID)
		if err != nil {
			return nil, err
		}
	}
	if product == nil {
		return nil, nil
	}
	t.mergeTrackedProductIdentifiers(product, cu)
	if err := t.store.UpdateProductMetadata(ctx, *product); err != nil {
		return nil, err
	}
	return product, nil
}

func (t *CoupangTracker) ensureTrackedWithCoalescing(ctx context.Context, trackID string, cu *providers.CoupangURL, attachmentTitle string) (*CoupangProductRecord, bool, registrationSummary, error) {
	flight, leader := t.beginRegistrationFlight(trackID)
	if !leader {
		return t.awaitTrackedRegistrationFlight(ctx, trackID, flight)
	}

	t.recordCoalescingSignal(ctx, trackID, "registration", true, 0, false)
	createdProduct, created, result, err := t.ensureTrackedDirect(ctx, trackID, cu, attachmentTitle)
	if err != nil {
		t.completeRegistrationFlight(trackID, nil, result, err)
		return nil, false, result, err
	}
	if result.RegistrationDeferred {
		return createdProduct, created, result, nil
	}
	t.completeRegistrationFlight(trackID, createdProduct, result, nil)
	return createdProduct, created, result, nil
}

func (t *CoupangTracker) awaitTrackedRegistrationFlight(ctx context.Context, trackID string, flight *registrationFlight) (*CoupangProductRecord, bool, registrationSummary, error) {
	waitStart := time.Now()
	product, result, err := t.waitRegistrationFlight(ctx, trackID, flight)
	waited := time.Since(waitStart)
	t.recordCoalescingSignal(ctx, trackID, "registration", false, waited, result.RegistrationDeferred && product == nil)
	if err != nil {
		return nil, false, result, err
	}
	return product, false, result, nil
}

func (t *CoupangTracker) ensureTrackedDirect(ctx context.Context, trackID string, cu *providers.CoupangURL, attachmentTitle string) (*CoupangProductRecord, bool, registrationSummary, error) {
	product, err := t.loadTrackedProduct(ctx, trackID, cu)
	if err != nil {
		return nil, false, registrationSummary{}, err
	}
	if product != nil {
		return product, false, registrationSummary{ResponseMode: CoupangResponseModeStandard}, nil
	}

	if !t.regs.Allow(time.Now()) {
		return nil, false, registrationSummary{}, ErrCoupangRegistrationLimited
	}
	if err := t.enforceCapacity(ctx, 1); err != nil {
		return nil, false, registrationSummary{}, err
	}

	created, result, err := t.bootstrapProduct(ctx, trackID, cu, attachmentTitle)
	if err != nil {
		return nil, false, result, err
	}
	return created, true, result, nil
}

func (t *CoupangTracker) bootstrapProduct(ctx context.Context, trackID string, cu *providers.CoupangURL, attachmentTitle string) (*CoupangProductRecord, registrationSummary, error) {
	budgetCtx, cancel := context.WithTimeout(ctx, t.registrationLatencyBudget())
	defer cancel()
	now := time.Now()
	resolved, err := t.resolveForTracking(budgetCtx, cu, nil, "registration", true, attachmentTitle)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(budgetCtx.Err(), context.DeadlineExceeded) {
			result := registrationSummary{
				Stage:                CoupangRegistrationStageDeferred,
				ResponseMode:         CoupangResponseModeRegistrationDeferred,
				RegistrationDeferred: true,
				BudgetExhausted:      true,
				RescueDeferred:       true,
			}
			t.recordRegistrationPathEvent(ctx, trackID, result, "sync")
			t.startAsyncRegistrationBootstrap(trackID, cu, attachmentTitle)
			return nil, result, nil
		}
		return nil, registrationSummary{}, err
	}

	record := buildBootstrapRecord(trackID, cu, resolved, now)
	if err := t.persistBootstrapRecord(ctx, trackID, record); err != nil {
		return nil, registrationSummary{}, err
	}
	result := registrationSummary{
		Stage:        registrationStageFromResolved(resolved),
		ResponseMode: CoupangResponseModeTextFirst,
		SeedDeferred: resolved.FallcentProductID != "" && t.fallcent != nil,
	}
	t.recordRegistrationPathEvent(ctx, trackID, result, "sync")
	t.seedBootstrapChart(trackID, cu.ProductID, resolved.FallcentProductID)

	product, err := t.store.GetProduct(ctx, trackID)
	return product, result, err
}

func buildBootstrapRecord(trackID string, cu *providers.CoupangURL, resolved *resolvedProduct, now time.Time) CoupangProductRecord {
	record := CoupangProductRecord{
		TrackID:      trackID,
		ProductID:    cu.ProductID,
		VendorItemID: firstNonEmpty(cu.VendorItemID, resolved.VendorItemID),
		ItemID:       firstNonEmpty(cu.ItemID, resolved.ItemID),
		Name:         resolved.Name,
		ImageURL:     resolved.ImageURL,
		Snapshot: CoupangSnapshot{
			TrackID:              trackID,
			Price:                resolved.Price,
			LastSeenAt:           now,
			LastRefreshAttemptAt: now,
			RefreshSource:        resolved.RefreshSource,
			Tier:                 CoupangTierWarm,
		},
		SourceMapping: CoupangSourceMapping{
			TrackID:             trackID,
			FallcentProductID:   resolved.FallcentProductID,
			SearchKeyword:       resolved.SearchKeyword,
			State:               resolved.MappingState,
			VerifiedAt:          now,
			ComparativeMinPrice: resolved.ComparativeMin,
		},
	}
	if record.SourceMapping.State != CoupangSourceMappingVerified {
		record.SourceMapping.VerifiedAt = time.Time{}
	}
	return record
}

func (t *CoupangTracker) persistBootstrapRecord(ctx context.Context, trackID string, record CoupangProductRecord) error {
	if err := t.store.UpsertProduct(ctx, record); err != nil {
		return err
	}
	if err := t.store.UpdateSnapshot(ctx, record.Snapshot); err != nil {
		return err
	}
	if err := t.store.UpsertSourceMapping(ctx, record.SourceMapping); err != nil {
		return err
	}
	if record.Snapshot.Price > 0 {
		if err := t.store.InsertPrice(ctx, trackID, record.Snapshot.Price, false); err != nil {
			return err
		}
	}
	return nil
}

func (t *CoupangTracker) seedBootstrapChart(trackID, productID, fallcentProductID string) {
	if fallcentProductID == "" || t.fallcent == nil {
		return
	}
	t.goAsync(func() {
		t.runAsyncSeedBootstrap(trackID, productID, fallcentProductID)
	})
}

func (t *CoupangTracker) startAsyncRegistrationBootstrap(trackID string, cu *providers.CoupangURL, attachmentTitle string) {
	t.goAsync(func() {
		ctx, cancel := context.WithTimeout(t.asyncContext(), 20*time.Second)
		defer cancel()

		started := registrationSummary{
			Stage:                CoupangRegistrationStageAsyncStarted,
			ResponseMode:         CoupangResponseModeRegistrationDeferred,
			RegistrationDeferred: true,
			BudgetExhausted:      true,
			RescueDeferred:       true,
		}
		t.recordRegistrationPathEvent(ctx, trackID, started, "async")

		now := time.Now()
		resolved, err := t.resolveForTracking(ctx, cu, nil, "registration_async", true, attachmentTitle)
		if err != nil {
			result := registrationSummary{
				Stage:                CoupangRegistrationStageAsyncFailed,
				ResponseMode:         CoupangResponseModeRegistrationDeferred,
				RegistrationDeferred: true,
				BudgetExhausted:      true,
				RescueDeferred:       true,
			}
			t.recordRegistrationPathEvent(ctx, trackID, result, "async")
			t.completeRegistrationFlight(trackID, nil, result, err)
			t.logger.Warn("async registration bootstrap failed", "track_id", trackID, "product_id", cu.ProductID, "error", err)
			return
		}

		record := buildBootstrapRecord(trackID, cu, resolved, now)
		if err := t.persistBootstrapRecord(ctx, trackID, record); err != nil {
			result := registrationSummary{
				Stage:                CoupangRegistrationStageAsyncFailed,
				ResponseMode:         CoupangResponseModeRegistrationDeferred,
				RegistrationDeferred: true,
				BudgetExhausted:      true,
				RescueDeferred:       true,
			}
			t.recordRegistrationPathEvent(ctx, trackID, result, "async")
			t.completeRegistrationFlight(trackID, nil, result, err)
			t.logger.Warn("async registration persist failed", "track_id", trackID, "product_id", cu.ProductID, "error", err)
			return
		}

		result := registrationSummary{
			Stage:        CoupangRegistrationStageAsyncSucceeded,
			ResponseMode: CoupangResponseModeTextFirst,
			SeedDeferred: resolved.FallcentProductID != "" && t.fallcent != nil,
		}
		t.recordRegistrationPathEvent(ctx, trackID, result, "async")
		t.seedBootstrapChart(trackID, cu.ProductID, resolved.FallcentProductID)

		product, err := t.store.GetProduct(ctx, trackID)
		t.completeRegistrationFlight(trackID, product, result, err)
		if err != nil {
			t.logger.Warn("async registration load failed", "track_id", trackID, "product_id", cu.ProductID, "error", err)
			return
		}
		if product != nil {
			t.logger.Info("async registration bootstrap succeeded", "track_id", trackID, "product_id", product.ProductID)
		}
	})
}

func (t *CoupangTracker) runAsyncSeedBootstrap(trackID, productID, fallcentProductID string) {
	ctx, cancel := context.WithTimeout(t.asyncContext(), 20*time.Second)
	defer cancel()

	t.recordRegistrationPathEvent(ctx, trackID, registrationSummary{
		Stage:        CoupangRegistrationStageSeedStarted,
		ResponseMode: CoupangResponseModeTextFirst,
		SeedDeferred: true,
	}, "seed")

	hasSeed, err := t.store.HasSeedPrices(ctx, trackID)
	if err != nil {
		t.recordRegistrationPathEvent(ctx, trackID, registrationSummary{
			Stage:        CoupangRegistrationStageSeedFailed,
			ResponseMode: CoupangResponseModeTextFirst,
			SeedDeferred: true,
		}, "seed")
		t.logger.Debug("seed existence check failed", "product_id", productID, "track_id", trackID, "error", err)
		return
	}
	if hasSeed {
		t.recordRegistrationPathEvent(ctx, trackID, registrationSummary{
			Stage:        CoupangRegistrationStageSeedSucceeded,
			ResponseMode: CoupangResponseModeTextFirst,
			SeedDeferred: true,
		}, "seed")
		return
	}

	seedPrices, chartErr := t.fallcent.FetchChart(ctx, fallcentProductID)
	if chartErr != nil {
		t.recordRegistrationPathEvent(ctx, trackID, registrationSummary{
			Stage:        CoupangRegistrationStageSeedFailed,
			ResponseMode: CoupangResponseModeTextFirst,
			SeedDeferred: true,
		}, "seed")
		t.logger.Debug("fallcent chart seed skipped", "product_id", productID, "error", chartErr)
		return
	}
	if len(seedPrices) == 0 {
		t.recordRegistrationPathEvent(ctx, trackID, registrationSummary{
			Stage:        CoupangRegistrationStageSeedSucceeded,
			ResponseMode: CoupangResponseModeTextFirst,
			SeedDeferred: true,
		}, "seed")
		return
	}
	if seedErr := t.store.InsertSeedPrices(ctx, trackID, seedPrices); seedErr != nil {
		t.recordRegistrationPathEvent(ctx, trackID, registrationSummary{
			Stage:        CoupangRegistrationStageSeedFailed,
			ResponseMode: CoupangResponseModeTextFirst,
			SeedDeferred: true,
		}, "seed")
		t.logger.Debug("seed price insertion failed", "product_id", productID, "error", seedErr)
		return
	}

	t.recordRegistrationPathEvent(ctx, trackID, registrationSummary{
		Stage:        CoupangRegistrationStageSeedSucceeded,
		ResponseMode: CoupangResponseModeTextFirst,
		SeedDeferred: true,
	}, "seed")
	t.logger.Info("fallcent chart seed inserted", "product_id", productID, "points", len(seedPrices))
}

func (t *CoupangTracker) beginRegistrationFlight(trackID string) (*registrationFlight, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if flight, exists := t.registrationFlights[trackID]; exists {
		return flight, false
	}
	flight := &registrationFlight{done: make(chan struct{})}
	t.registrationFlights[trackID] = flight
	return flight, true
}

func (t *CoupangTracker) completeRegistrationFlight(trackID string, product *CoupangProductRecord, result registrationSummary, err error) {
	t.mu.Lock()
	flight, exists := t.registrationFlights[trackID]
	if exists {
		flight.product = product
		flight.result = result
		flight.err = err
		delete(t.registrationFlights, trackID)
	}
	t.mu.Unlock()
	if exists {
		close(flight.done)
	}
}

func (t *CoupangTracker) waitRegistrationFlight(ctx context.Context, trackID string, flight *registrationFlight) (*CoupangProductRecord, registrationSummary, error) {
	if flight == nil {
		return nil, registrationSummary{}, fmt.Errorf("registration flight missing for %s", trackID)
	}
	waitErr := waitForDone(ctx, flight.done, t.registrationJoinWait())
	if waitErr == nil {
		return t.resolveCompletedRegistrationFlight(ctx, trackID, flight)
	}
	if errors.Is(waitErr, context.DeadlineExceeded) {
		return t.resolveTimedOutRegistrationFlight(ctx, trackID)
	}
	return nil, registrationSummary{}, waitErr
}

func (t *CoupangTracker) resolveCompletedRegistrationFlight(ctx context.Context, trackID string, flight *registrationFlight) (*CoupangProductRecord, registrationSummary, error) {
	if flight.err != nil {
		return nil, flight.result, flight.err
	}
	if flight.product != nil {
		return flight.product, flight.result, nil
	}
	product, err := t.store.GetProduct(ctx, trackID)
	if err != nil {
		return nil, flight.result, err
	}
	if product == nil {
		if flight.result.RegistrationDeferred {
			return nil, flight.result, nil
		}
		return nil, flight.result, fmt.Errorf("registration flight completed without product: %s", trackID)
	}
	return product, flight.result, nil
}

func (t *CoupangTracker) resolveTimedOutRegistrationFlight(ctx context.Context, trackID string) (*CoupangProductRecord, registrationSummary, error) {
	product, err := t.store.GetProduct(ctx, trackID)
	if err == nil && product != nil {
		return product, registrationSummary{ResponseMode: CoupangResponseModeTextFirst}, nil
	}
	return nil, registrationSummary{
		Stage:                CoupangRegistrationStageDeferred,
		ResponseMode:         CoupangResponseModeRegistrationDeferred,
		RegistrationDeferred: true,
		BudgetExhausted:      true,
		RescueDeferred:       true,
	}, nil
}

func (t *CoupangTracker) requireTrackedProduct(ctx context.Context, trackID, message string, cu *providers.CoupangURL) (*CoupangProductRecord, error) {
	product, err := t.store.GetProduct(ctx, trackID)
	if err != nil {
		return nil, err
	}
	if product == nil {
		return nil, fmt.Errorf("%s: %s", message, trackProductID(cu))
	}
	return product, nil
}

func (t *CoupangTracker) mergeTrackedProductIdentifiers(product *CoupangProductRecord, cu *providers.CoupangURL) {
	if cu.ItemID != "" && product.ItemID == "" {
		product.ItemID = cu.ItemID
	}
	if cu.VendorItemID != "" && product.VendorItemID == "" {
		product.VendorItemID = cu.VendorItemID
	}
}

func (t *CoupangTracker) coalescingEnabled() bool {
	return t.config.LookupCoalescingEnabled
}

func (t *CoupangTracker) registrationJoinWait() time.Duration {
	if t.config.RegistrationJoinWait > 0 {
		return t.config.RegistrationJoinWait
	}
	return 2 * time.Second
}

func (t *CoupangTracker) registrationLatencyBudget() time.Duration {
	if t.config.RegistrationLatencyBudget > 0 {
		return t.config.RegistrationLatencyBudget
	}
	return 2 * time.Second
}
