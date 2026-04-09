package coupang_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/shift-click/masterbot/internal/coupang"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/store"
)

type stubCoupangFetcher struct {
	product *providers.CoupangProduct
	err     error
}

func (s stubCoupangFetcher) FetchCurrent(context.Context, *providers.CoupangURL) (*providers.CoupangProduct, error) {
	return s.product, s.err
}

type countingCoupangFetcher struct {
	product *providers.CoupangProduct
	err     error
	calls   *int32
}

func (s countingCoupangFetcher) FetchCurrent(context.Context, *providers.CoupangURL) (*providers.CoupangProduct, error) {
	if s.calls != nil {
		atomic.AddInt32(s.calls, 1)
	}
	return s.product, s.err
}

type stubFallcentFetcher struct {
	resolved   *providers.FallcentProductData
	resolvedFn func(*providers.CoupangURL, []string) (*providers.FallcentProductData, error)
	detail     *providers.FallcentProductData
	detailFn   func(string) (*providers.FallcentProductData, error)
	err        error
}

func (s stubFallcentFetcher) ResolveProduct(_ context.Context, cu *providers.CoupangURL, keywords []string) (*providers.FallcentProductData, error) {
	if s.resolvedFn != nil {
		return s.resolvedFn(cu, keywords)
	}
	if s.resolved != nil {
		return s.resolved, nil
	}
	return nil, s.err
}

func (s stubFallcentFetcher) FetchProduct(_ context.Context, fallcentProductID string) (*providers.FallcentProductData, error) {
	if s.detailFn != nil {
		return s.detailFn(fallcentProductID)
	}
	if s.detail != nil {
		return s.detail, nil
	}
	return nil, s.err
}

func (s stubFallcentFetcher) FetchChart(_ context.Context, _ string) ([]int, error) {
	return nil, fmt.Errorf("chart not available in stub")
}

func (s stubFallcentFetcher) LookupByCoupangID(_ context.Context, _, _ string) (*providers.FallcentProductData, error) {
	return nil, fmt.Errorf("direct lookup not available in stub")
}

type budgetAwareFallcentFetcher struct {
	resolveDelay time.Duration
	resolved     *providers.FallcentProductData
	chart        []int
}

func (f budgetAwareFallcentFetcher) ResolveProduct(ctx context.Context, _ *providers.CoupangURL, _ []string) (*providers.FallcentProductData, error) {
	select {
	case <-time.After(f.resolveDelay):
		return f.resolved, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f budgetAwareFallcentFetcher) FetchProduct(context.Context, string) (*providers.FallcentProductData, error) {
	return nil, fmt.Errorf("detail not available")
}

func (f budgetAwareFallcentFetcher) FetchChart(context.Context, string) ([]int, error) {
	return f.chart, nil
}

func (f budgetAwareFallcentFetcher) LookupByCoupangID(context.Context, string, string) (*providers.FallcentProductData, error) {
	return nil, fmt.Errorf("direct lookup not available")
}

func TestCoupangTrackerLookupUsesDBFirstForTrackedProducts(t *testing.T) {
	t.Parallel()

	priceStore := newTrackerTestStore(t)
	ctx := context.Background()
	now := time.Now()
	if err := priceStore.UpsertProduct(ctx, store.CoupangProductRecord{
		TrackID:   "9334776688#i:20787679097",
		ProductID: "9334776688",
		ItemID:    "20787679097",
		Name:      "numeric tracked",
		Snapshot:  store.CoupangSnapshot{Tier: store.CoupangTierWarm},
		SourceMapping: store.CoupangSourceMapping{
			TrackID:             "9334776688#i:20787679097",
			FallcentProductID:   "fc1",
			SearchKeyword:       "numeric tracked",
			State:               store.CoupangSourceMappingVerified,
			VerifiedAt:          now,
			ComparativeMinPrice: 10500,
		},
	}); err != nil {
		t.Fatalf("upsert numeric product: %v", err)
	}
	if err := priceStore.UpdateSnapshot(ctx, store.CoupangSnapshot{
		TrackID:              "9334776688#i:20787679097",
		Price:                11000,
		LastSeenAt:           now,
		LastRefreshAttemptAt: now,
		RefreshSource:        "fallcent",
		Tier:                 store.CoupangTierWarm,
	}); err != nil {
		t.Fatalf("update snapshot numeric: %v", err)
	}
	if err := priceStore.InsertPrice(ctx, "9334776688#i:20787679097", 11000, false); err != nil {
		t.Fatalf("insert price numeric: %v", err)
	}

	tracker := NewCoupangTracker(priceStore, nil, nil, nil, trackerTestConfig(), nil)
	result, err := tracker.Lookup(ctx, "https://www.coupang.com/vp/products/9334776688?itemId=20787679097")
	if err != nil {
		t.Fatalf("lookup tracked product: %v", err)
	}
	if result.Product.Snapshot.Price != 11000 {
		t.Fatalf("snapshot price = %d, want 11000", result.Product.Snapshot.Price)
	}
	if result.Product.SourceMapping.FallcentProductID != "fc1" {
		t.Fatalf("fallcent mapping = %q", result.Product.SourceMapping.FallcentProductID)
	}
	if result.IsStale {
		t.Fatal("expected tracked lookup to be fresh")
	}
	if result.ReadRefresh != CoupangReadRefreshNotAttempted {
		t.Fatalf("read refresh = %s, want not_attempted", result.ReadRefresh)
	}
}

func TestCoupangTrackerLookupKeepsSameDayDuplicatesIneligibleForStats(t *testing.T) {
	t.Parallel()

	priceStore := newTrackerTestStore(t)
	ctx := context.Background()
	now := time.Now()
	trackID := "8106313335#v:90861669040"
	if err := priceStore.UpsertProduct(ctx, store.CoupangProductRecord{
		TrackID:      trackID,
		ProductID:    "8106313335",
		VendorItemID: "90861669040",
		ItemID:       "19704418892",
		Name:         "파워에이드 퍼플스톰, 600ml, 20개",
		Snapshot:     store.CoupangSnapshot{Tier: store.CoupangTierWarm},
		SourceMapping: store.CoupangSourceMapping{
			TrackID:             trackID,
			FallcentProductID:   "fc-powerade",
			State:               store.CoupangSourceMappingVerified,
			VerifiedAt:          now,
			ComparativeMinPrice: 18240,
		},
	}); err != nil {
		t.Fatalf("upsert product: %v", err)
	}
	if err := priceStore.UpdateSnapshot(ctx, store.CoupangSnapshot{
		TrackID:              trackID,
		Price:                19300,
		LastSeenAt:           now,
		LastRefreshAttemptAt: now,
		RefreshSource:        "fallcent",
		Tier:                 store.CoupangTierWarm,
	}); err != nil {
		t.Fatalf("update snapshot: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := priceStore.InsertPrice(ctx, trackID, 19300, false); err != nil {
			t.Fatalf("insert duplicate price %d: %v", i, err)
		}
	}

	tracker := NewCoupangTracker(priceStore, nil, nil, nil, trackerTestConfig(), nil)
	result, err := tracker.Lookup(ctx, "https://www.coupang.com/vp/products/8106313335?itemId=19704418892&vendorItemId=90861669040")
	if err != nil {
		t.Fatalf("lookup tracked product: %v", err)
	}
	if result.SampleCount != 3 {
		t.Fatalf("sample count = %d, want 3", result.SampleCount)
	}
	if result.DistinctDays != 1 {
		t.Fatalf("distinct days = %d, want 1", result.DistinctDays)
	}
	if result.HistorySpanDays != 1 {
		t.Fatalf("history span days = %d, want 1", result.HistorySpanDays)
	}
	if result.StatsEligible {
		t.Fatal("expected same-day duplicates to remain ineligible for stats")
	}
}

func TestCoupangTrackerLookupMarksThreeDayHistoryEligibleForStats(t *testing.T) {
	t.Parallel()

	priceStore := newTrackerTestStore(t)
	ctx := context.Background()
	now := time.Now()
	trackID := "9334776688#i:20787679097"
	if err := priceStore.UpsertProduct(ctx, store.CoupangProductRecord{
		TrackID:   trackID,
		ProductID: "9334776688",
		ItemID:    "20787679097",
		Name:      "three-day tracked",
		Snapshot:  store.CoupangSnapshot{Tier: store.CoupangTierWarm},
		SourceMapping: store.CoupangSourceMapping{
			TrackID:           trackID,
			FallcentProductID: "fc-three-day",
			State:             store.CoupangSourceMappingVerified,
			VerifiedAt:        now,
		},
	}); err != nil {
		t.Fatalf("upsert product: %v", err)
	}
	if err := priceStore.UpdateSnapshot(ctx, store.CoupangSnapshot{
		TrackID:              trackID,
		Price:                21900,
		LastSeenAt:           now,
		LastRefreshAttemptAt: now,
		RefreshSource:        "fallcent",
		Tier:                 store.CoupangTierWarm,
	}); err != nil {
		t.Fatalf("update snapshot: %v", err)
	}
	if err := priceStore.InsertSeedPrices(ctx, trackID, []int{25000, 23000, 21900}); err != nil {
		t.Fatalf("insert seed prices: %v", err)
	}

	tracker := NewCoupangTracker(priceStore, nil, nil, nil, trackerTestConfig(), nil)
	result, err := tracker.Lookup(ctx, "https://www.coupang.com/vp/products/9334776688?itemId=20787679097")
	if err != nil {
		t.Fatalf("lookup tracked product: %v", err)
	}
	if result.SampleCount != 3 {
		t.Fatalf("sample count = %d, want 3", result.SampleCount)
	}
	if result.DistinctDays != 3 {
		t.Fatalf("distinct days = %d, want 3", result.DistinctDays)
	}
	if result.HistorySpanDays != 3 {
		t.Fatalf("history span days = %d, want 3", result.HistorySpanDays)
	}
	if !result.StatsEligible {
		t.Fatal("expected three-day history to be eligible for stats")
	}
}

func TestCoupangTrackerLookupReturnsStaleWithoutRefreshWhenBudgetExhausted(t *testing.T) {
	t.Parallel()

	priceStore := newTrackerTestStore(t)
	ctx := context.Background()
	staleAt := time.Now().Add(-2 * time.Hour)
	if err := priceStore.UpsertProduct(ctx, store.CoupangProductRecord{
		TrackID:   "9334776688#i:20787679097",
		ProductID: "9334776688",
		ItemID:    "20787679097",
		Name:      "stale tracked",
		Snapshot:  store.CoupangSnapshot{Tier: store.CoupangTierWarm},
		SourceMapping: store.CoupangSourceMapping{
			TrackID:           "9334776688#i:20787679097",
			FallcentProductID: "fc1",
			State:             store.CoupangSourceMappingVerified,
			VerifiedAt:        staleAt,
		},
	}); err != nil {
		t.Fatalf("upsert product: %v", err)
	}
	if err := priceStore.UpdateSnapshot(ctx, store.CoupangSnapshot{
		TrackID:              "9334776688#i:20787679097",
		Price:                13000,
		LastSeenAt:           staleAt,
		LastRefreshAttemptAt: staleAt,
		RefreshSource:        "fallcent",
		Tier:                 store.CoupangTierWarm,
	}); err != nil {
		t.Fatalf("update snapshot: %v", err)
	}
	if err := priceStore.InsertPrice(ctx, "9334776688#i:20787679097", 13000, false); err != nil {
		t.Fatalf("insert price: %v", err)
	}

	cfg := trackerTestConfig()
	cfg.RefreshBudgetPerHour = 0
	tracker := NewCoupangTracker(priceStore, nil, stubFallcentFetcher{
		detail: &providers.FallcentProductData{
			FallcentProductID: "fc1",
			ProductID:         "9334776688",
			ItemID:            "20787679097",
			Name:              "stale tracked",
			Price:             12900,
		},
	}, nil, cfg, nil)

	result, err := tracker.Lookup(ctx, "https://www.coupang.com/vp/products/9334776688?itemId=20787679097")
	if err != nil {
		t.Fatalf("lookup stale tracked product: %v", err)
	}
	if !result.IsStale {
		t.Fatal("expected stale result")
	}
	if result.RefreshRequested {
		t.Fatal("expected refresh to be skipped when budget is exhausted")
	}
	if result.ReadRefresh != CoupangReadRefreshBudgetExhausted {
		t.Fatalf("read refresh = %s, want budget_exhausted", result.ReadRefresh)
	}
}

func TestCoupangTrackerLookupUsesReadThroughRefreshForStaleProducts(t *testing.T) {
	t.Parallel()

	priceStore := newTrackerTestStore(t)
	ctx := context.Background()
	staleAt := time.Now().Add(-2 * time.Hour)
	if err := priceStore.UpsertProduct(ctx, store.CoupangProductRecord{
		TrackID:   "9334776688#i:20787679097",
		ProductID: "9334776688",
		ItemID:    "20787679097",
		Name:      "stale tracked",
		Snapshot:  store.CoupangSnapshot{Tier: store.CoupangTierWarm},
		SourceMapping: store.CoupangSourceMapping{
			TrackID:           "9334776688#i:20787679097",
			FallcentProductID: "fc1",
			State:             store.CoupangSourceMappingVerified,
			VerifiedAt:        staleAt,
		},
	}); err != nil {
		t.Fatalf("upsert product: %v", err)
	}
	if err := priceStore.UpdateSnapshot(ctx, store.CoupangSnapshot{
		TrackID:              "9334776688#i:20787679097",
		Price:                13000,
		LastSeenAt:           staleAt,
		LastRefreshAttemptAt: staleAt,
		RefreshSource:        "fallcent",
		Tier:                 store.CoupangTierWarm,
	}); err != nil {
		t.Fatalf("update snapshot: %v", err)
	}

	tracker := NewCoupangTracker(
		priceStore,
		nil,
		stubFallcentFetcher{
			detail: &providers.FallcentProductData{
				FallcentProductID: "fc1",
				ProductID:         "9334776688",
				ItemID:            "20787679097",
				Name:              "stale tracked",
				Price:             12900,
			},
		},
		nil, trackerTestConfig(),
		nil,
	)

	result, err := tracker.Lookup(ctx, "https://www.coupang.com/vp/products/9334776688?itemId=20787679097")
	if err != nil {
		t.Fatalf("lookup stale tracked product: %v", err)
	}
	if result.IsStale {
		t.Fatal("expected read-through refresh to return fresh result")
	}
	if result.RefreshRequested {
		t.Fatal("expected no async refresh after successful read-through refresh")
	}
	if result.ReadRefresh != CoupangReadRefreshSucceeded {
		t.Fatalf("read refresh = %s, want succeeded", result.ReadRefresh)
	}
	if result.Product.Snapshot.Price != 12900 {
		t.Fatalf("snapshot price = %d, want 12900", result.Product.Snapshot.Price)
	}
	if result.SampleCount != 1 {
		t.Fatalf("sample count = %d, want 1", result.SampleCount)
	}
	product, err := priceStore.GetProduct(ctx, "9334776688#i:20787679097")
	if err != nil {
		t.Fatalf("get product after read-through refresh: %v", err)
	}
	if product == nil || product.Snapshot.Price != 12900 {
		t.Fatalf("product after read-through refresh = %#v", product)
	}
}

func TestCoupangTrackerLookupFallsBackToStaleWhenReadThroughTimesOut(t *testing.T) {
	t.Parallel()

	priceStore := newTrackerTestStore(t)
	ctx := context.Background()
	staleAt := time.Now().Add(-2 * time.Hour)
	if err := priceStore.UpsertProduct(ctx, store.CoupangProductRecord{
		TrackID:   "9334776688#i:20787679097",
		ProductID: "9334776688",
		ItemID:    "20787679097",
		Name:      "stale tracked",
		Snapshot:  store.CoupangSnapshot{Tier: store.CoupangTierWarm},
		SourceMapping: store.CoupangSourceMapping{
			TrackID:           "9334776688#i:20787679097",
			FallcentProductID: "fc1",
			State:             store.CoupangSourceMappingVerified,
			VerifiedAt:        staleAt,
		},
	}); err != nil {
		t.Fatalf("upsert product: %v", err)
	}
	if err := priceStore.UpdateSnapshot(ctx, store.CoupangSnapshot{
		TrackID:              "9334776688#i:20787679097",
		Price:                13000,
		LastSeenAt:           staleAt,
		LastRefreshAttemptAt: staleAt,
		RefreshSource:        "fallcent",
		Tier:                 store.CoupangTierWarm,
	}); err != nil {
		t.Fatalf("update snapshot: %v", err)
	}
	if err := priceStore.InsertPrice(ctx, "9334776688#i:20787679097", 13000, false); err != nil {
		t.Fatalf("insert price: %v", err)
	}

	cfg := trackerTestConfig()
	cfg.ReadRefreshTimeout = 5 * time.Millisecond
	tracker := NewCoupangTracker(
		priceStore,
		nil,
		stubFallcentFetcher{
			detailFn: func(string) (*providers.FallcentProductData, error) {
				time.Sleep(20 * time.Millisecond)
				return &providers.FallcentProductData{
					FallcentProductID: "fc1",
					ProductID:         "9334776688",
					ItemID:            "20787679097",
					Name:              "stale tracked",
					Price:             12900,
				}, nil
			},
		},
		nil, cfg,
		nil,
	)

	result, err := tracker.Lookup(ctx, "https://www.coupang.com/vp/products/9334776688?itemId=20787679097")
	if err != nil {
		t.Fatalf("lookup stale tracked product: %v", err)
	}
	if !result.IsStale {
		t.Fatal("expected stale fallback after read-through timeout")
	}
	if result.ReadRefresh != CoupangReadRefreshTimedOut {
		t.Fatalf("read refresh = %s, want timed_out", result.ReadRefresh)
	}
	if !result.RefreshRequested {
		t.Fatal("expected async refresh to be scheduled after stale fallback")
	}
	if result.Product.Snapshot.Price != 13000 {
		t.Fatalf("snapshot price = %d, want stale 13000", result.Product.Snapshot.Price)
	}
}

func TestCoupangTrackerClassifyTier(t *testing.T) {
	t.Parallel()

	tracker := NewCoupangTracker(nil, nil, nil, nil, trackerTestConfig(), nil)
	now := time.Now()

	if got := tracker.ClassifyTier(store.CoupangProductRecord{
		LastQueried:      now.Add(-time.Hour),
		RecentQueryCount: 4,
	}, now); got != store.CoupangTierHot {
		t.Fatalf("hot tier = %s", got)
	}
	if got := tracker.ClassifyTier(store.CoupangProductRecord{
		LastQueried:      now.Add(-time.Hour),
		RecentQueryCount: 1,
	}, now); got != store.CoupangTierWarm {
		t.Fatalf("warm tier = %s", got)
	}
	if got := tracker.ClassifyTier(store.CoupangProductRecord{
		LastQueried:      now.Add(-72 * time.Hour),
		RecentQueryCount: 10,
	}, now); got != store.CoupangTierCold {
		t.Fatalf("cold tier = %s", got)
	}
}

func TestCoupangTrackerEnforceCapacityEvictsOldest(t *testing.T) {
	t.Parallel()

	priceStore := newTrackerTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"1001", "1002"} {
		if err := priceStore.UpsertProduct(ctx, store.CoupangProductRecord{
			ProductID: id,
			Name:      id,
			Snapshot:  store.CoupangSnapshot{Tier: store.CoupangTierWarm},
		}); err != nil {
			t.Fatalf("upsert product %s: %v", id, err)
		}
	}
	time.Sleep(10 * time.Millisecond)
	if err := priceStore.TouchProduct(ctx, "1002", 24*time.Hour); err != nil {
		t.Fatalf("touch product 1002: %v", err)
	}

	cfg := trackerTestConfig()
	cfg.MaxProducts = 1
	tracker := NewCoupangTracker(priceStore, nil, nil, nil, cfg, nil)
	if err := tracker.EnforceCapacity(ctx); err != nil {
		t.Fatalf("enforce capacity: %v", err)
	}
	remaining, err := priceStore.CountTrackedProducts(ctx)
	if err != nil {
		t.Fatalf("count tracked products: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("remaining products = %d, want 1", remaining)
	}
	oldest, err := priceStore.GetProduct(ctx, "1001")
	if err != nil {
		t.Fatalf("get oldest product: %v", err)
	}
	if oldest != nil {
		t.Fatal("expected oldest product to be evicted")
	}
}

func TestCoupangTrackerRegistersProductViaFallcentResolution(t *testing.T) {
	t.Parallel()

	priceStore := newTrackerTestStore(t)
	tracker := NewCoupangTracker(
		priceStore,
		nil,
		stubFallcentFetcher{
			resolved: &providers.FallcentProductData{
				FallcentProductID: "fc-product",
				ProductID:         "9334776688",
				ItemID:            "20787679097",
				Name:              "bootstrap product",
				Price:             9900,
				ImageURL:          "https://img",
				LowestPrice:       9500,
				SearchKeyword:     "bootstrap product",
			},
		},
		nil, trackerTestConfig(),
		nil,
	)

	result, err := tracker.Lookup(context.Background(), "https://www.coupang.com/vp/products/9334776688?itemId=20787679097")
	if err != nil {
		t.Fatalf("lookup bootstrap product: %v", err)
	}
	if result.Product.Name != "bootstrap product" {
		t.Fatalf("product name = %q", result.Product.Name)
	}
	if result.Product.SourceMapping.FallcentProductID != "fc-product" {
		t.Fatalf("fallcent mapping = %q", result.Product.SourceMapping.FallcentProductID)
	}
	if result.Product.SourceMapping.ComparativeMinPrice != 9500 {
		t.Fatalf("comparative min = %d", result.Product.SourceMapping.ComparativeMinPrice)
	}
}

func TestCoupangTrackerLookupReusesTrackedProductAfterInitialRegistration(t *testing.T) {
	t.Parallel()

	priceStore := newTrackerTestStore(t)
	resolveCalls := 0
	tracker := NewCoupangTracker(
		priceStore,
		nil,
		stubFallcentFetcher{
			resolvedFn: func(_ *providers.CoupangURL, _ []string) (*providers.FallcentProductData, error) {
				resolveCalls++
				return &providers.FallcentProductData{
					FallcentProductID: "fc-reuse",
					ProductID:         "9334776688",
					ItemID:            "20787679097",
					Name:              "reused tracked product",
					Price:             10300,
					SearchKeyword:     "reused tracked product",
				}, nil
			},
		},
		nil,
		trackerTestConfig(),
		nil,
	)

	url := "https://www.coupang.com/vp/products/9334776688?itemId=20787679097"
	first, err := tracker.Lookup(context.Background(), url)
	if err != nil {
		t.Fatalf("first lookup: %v", err)
	}
	second, err := tracker.Lookup(context.Background(), url)
	if err != nil {
		t.Fatalf("second lookup: %v", err)
	}

	if resolveCalls != 1 {
		t.Fatalf("resolve calls = %d, want 1", resolveCalls)
	}
	if first.Product.TrackID == "" || second.Product.TrackID == "" {
		t.Fatalf("track id should not be empty: first=%q second=%q", first.Product.TrackID, second.Product.TrackID)
	}
	if first.Product.TrackID != second.Product.TrackID {
		t.Fatalf("track id changed: first=%q second=%q", first.Product.TrackID, second.Product.TrackID)
	}
	if second.ReadRefresh != CoupangReadRefreshNotAttempted {
		t.Fatalf("second read refresh = %s, want not_attempted", second.ReadRefresh)
	}
}

func TestCoupangTrackerLookupCoalescesConcurrentRegistration(t *testing.T) {
	t.Parallel()

	priceStore := newTrackerTestStore(t)
	var resolveCalls int32
	tracker := NewCoupangTracker(
		priceStore,
		nil,
		stubFallcentFetcher{
			resolvedFn: func(_ *providers.CoupangURL, _ []string) (*providers.FallcentProductData, error) {
				atomic.AddInt32(&resolveCalls, 1)
				time.Sleep(40 * time.Millisecond)
				return &providers.FallcentProductData{
					FallcentProductID: "fc-concurrent",
					ProductID:         "9334776688",
					ItemID:            "20787679097",
					Name:              "concurrent register",
					Price:             10300,
					SearchKeyword:     "concurrent register",
				}, nil
			},
		},
		nil,
		trackerTestConfig(),
		nil,
	)

	url := "https://www.coupang.com/vp/products/9334776688?itemId=20787679097"
	start := make(chan struct{})
	results := make([]*CoupangLookupResult, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			results[idx], errs[idx] = lookupWithBusyRetry(tracker, url)
		}(i)
	}
	close(start)
	wg.Wait()

	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("lookup %d error = %v", i, errs[i])
		}
	}
	if got := atomic.LoadInt32(&resolveCalls); got != 1 {
		t.Fatalf("resolve calls = %d, want 1", got)
	}
	if results[0] == nil || results[1] == nil {
		t.Fatalf("lookup results should not be nil: %#v %#v", results[0], results[1])
	}
	if results[0].Product.TrackID != results[1].Product.TrackID {
		t.Fatalf("track IDs differ: %q vs %q", results[0].Product.TrackID, results[1].Product.TrackID)
	}
}

func TestCoupangTrackerLookupReturnsDeferredWhenRegistrationBudgetExpires(t *testing.T) {
	t.Parallel()

	priceStore := newTrackerTestStore(t)
	cfg := trackerTestConfig()
	cfg.RegistrationLatencyBudget = 5 * time.Millisecond
	cfg.RegistrationJoinWait = 5 * time.Millisecond

	tracker := NewCoupangTracker(
		priceStore,
		nil,
		budgetAwareFallcentFetcher{
			resolveDelay: 30 * time.Millisecond,
			resolved: &providers.FallcentProductData{
				FallcentProductID: "fc-deferred",
				ProductID:         "9334776688",
				ItemID:            "20787679097",
				Name:              "deferred product",
				Price:             9900,
				SearchKeyword:     "deferred product",
			},
		},
		nil,
		cfg,
		nil,
	)

	result, err := tracker.Lookup(context.Background(), "https://www.coupang.com/vp/products/9334776688?itemId=20787679097")
	if err != nil {
		t.Fatalf("lookup deferred product: %v", err)
	}
	if !result.RegistrationDeferred {
		t.Fatalf("expected registration deferred result: %#v", result)
	}
	if result.ResponseMode != CoupangResponseModeRegistrationDeferred {
		t.Fatalf("response mode = %s, want registration_deferred", result.ResponseMode)
	}
	if !result.BudgetExhausted || !result.RescueDeferred {
		t.Fatalf("expected deferred budget flags: %#v", result)
	}
}

func TestCoupangTrackerLookupSeedsChartAsynchronously(t *testing.T) {
	t.Parallel()

	priceStore := newTrackerTestStore(t)
	cfg := trackerTestConfig()
	cfg.RegistrationLatencyBudget = 50 * time.Millisecond

	tracker := NewCoupangTracker(
		priceStore,
		nil,
		budgetAwareFallcentFetcher{
			resolveDelay: 0,
			resolved: &providers.FallcentProductData{
				FallcentProductID: "fc-seed",
				ProductID:         "9334776688",
				ItemID:            "20787679097",
				Name:              "seed async product",
				Price:             11100,
				SearchKeyword:     "seed async product",
			},
			chart: []int{12000, 11500, 11100},
		},
		nil,
		cfg,
		nil,
	)

	result, err := tracker.Lookup(context.Background(), "https://www.coupang.com/vp/products/9334776688?itemId=20787679097")
	if err != nil {
		t.Fatalf("lookup seeded product: %v", err)
	}
	if !result.SeedDeferred || !result.RegistrationDeferredUI {
		t.Fatalf("expected seed deferred flags: %#v", result)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		hasSeed, seedErr := priceStore.HasSeedPrices(context.Background(), "9334776688#i:20787679097")
		if seedErr == nil && hasSeed {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected async seed insertion to complete")
}

func TestCoupangTrackerRescuesSearchKeywordViaAuxiliaryProvider(t *testing.T) {
	t.Parallel()

	priceStore := newTrackerTestStore(t)
	tracker := NewCoupangTracker(
		priceStore,
		stubCoupangFetcher{
			product: &providers.CoupangProduct{
				ProductID: "9334776688",
				ItemID:    "20787679097",
				Name:      "rescued title",
				Price:     10900,
			},
		},
		stubFallcentFetcher{
			resolvedFn: func(cu *providers.CoupangURL, keywords []string) (*providers.FallcentProductData, error) {
				if cu.ProductID != "9334776688" {
					t.Fatalf("unexpected product id: %s", cu.ProductID)
				}
				if len(keywords) != 1 || keywords[0] != "rescued title" {
					t.Fatalf("keywords = %#v, want rescued title", keywords)
				}
				return &providers.FallcentProductData{
					FallcentProductID: "fc-rescued",
					ProductID:         "9334776688",
					ItemID:            "20787679097",
					Name:              "rescued title",
					Price:             9900,
					LowestPrice:       9100,
					SearchKeyword:     "rescued title",
				}, nil
			},
		},
		nil, trackerTestConfig(),
		nil,
	)

	result, err := tracker.Lookup(context.Background(), "https://www.coupang.com/vp/products/9334776688?itemId=20787679097")
	if err != nil {
		t.Fatalf("lookup rescued product: %v", err)
	}
	if result.Product.Name != "rescued title" {
		t.Fatalf("product name = %q, want rescued title", result.Product.Name)
	}
	if result.Product.SourceMapping.SearchKeyword != "rescued title" {
		t.Fatalf("search keyword = %q, want rescued title", result.Product.SourceMapping.SearchKeyword)
	}
	if result.Product.SourceMapping.FallcentProductID != "fc-rescued" {
		t.Fatalf("fallcent mapping = %q, want fc-rescued", result.Product.SourceMapping.FallcentProductID)
	}
}

func TestCoupangTrackerRefreshUsesVerifiedFallcentDetail(t *testing.T) {
	t.Parallel()

	priceStore := newTrackerTestStore(t)
	ctx := context.Background()
	now := time.Now()
	if err := priceStore.UpsertProduct(ctx, store.CoupangProductRecord{
		TrackID:   "9334776688#i:20787679097",
		ProductID: "9334776688",
		ItemID:    "20787679097",
		Name:      "tracked fallcent",
		Snapshot:  store.CoupangSnapshot{Tier: store.CoupangTierWarm},
		SourceMapping: store.CoupangSourceMapping{
			TrackID:             "9334776688#i:20787679097",
			FallcentProductID:   "fc1",
			SearchKeyword:       "tracked fallcent",
			State:               store.CoupangSourceMappingVerified,
			VerifiedAt:          now,
			ComparativeMinPrice: 9800,
		},
	}); err != nil {
		t.Fatalf("upsert product: %v", err)
	}
	if err := priceStore.UpdateSnapshot(ctx, store.CoupangSnapshot{
		TrackID:              "9334776688#i:20787679097",
		Price:                11000,
		LastSeenAt:           now.Add(-2 * time.Hour),
		LastRefreshAttemptAt: now.Add(-2 * time.Hour),
		RefreshSource:        "fallcent",
		Tier:                 store.CoupangTierWarm,
	}); err != nil {
		t.Fatalf("update snapshot: %v", err)
	}

	tracker := NewCoupangTracker(
		priceStore,
		nil,
		stubFallcentFetcher{
			detail: &providers.FallcentProductData{
				FallcentProductID: "fc1",
				ProductID:         "9334776688",
				ItemID:            "20787679097",
				Name:              "tracked fallcent",
				Price:             8800,
				ImageURL:          "https://img",
				LowestPrice:       8700,
			},
		},
		nil, trackerTestConfig(),
		nil,
	)

	if err := tracker.RefreshProduct(ctx, "9334776688#i:20787679097", true, "test"); err != nil {
		t.Fatalf("refresh product: %v", err)
	}

	product, err := priceStore.GetProduct(ctx, "9334776688#i:20787679097")
	if err != nil {
		t.Fatalf("get product after refresh: %v", err)
	}
	if product.Snapshot.Price != 8800 {
		t.Fatalf("snapshot price = %d, want 8800", product.Snapshot.Price)
	}
	if product.Snapshot.RefreshSource != "fallcent" {
		t.Fatalf("refresh source = %q, want fallcent", product.Snapshot.RefreshSource)
	}
	if product.SourceMapping.ComparativeMinPrice != 9800 {
		t.Fatalf("expected comparative min to be preserved or updated, got %d", product.SourceMapping.ComparativeMinPrice)
	}
}

func TestCoupangTrackerLookupCoalescesConcurrentReadRefresh(t *testing.T) {
	t.Parallel()

	priceStore := newTrackerTestStore(t)
	ctx := context.Background()
	staleAt := time.Now().Add(-2 * time.Hour)
	trackID := "9334776688#i:20787679097"
	if err := priceStore.UpsertProduct(ctx, store.CoupangProductRecord{
		TrackID:   trackID,
		ProductID: "9334776688",
		ItemID:    "20787679097",
		Name:      "stale tracked",
		Snapshot:  store.CoupangSnapshot{Tier: store.CoupangTierWarm},
		SourceMapping: store.CoupangSourceMapping{
			TrackID:           trackID,
			FallcentProductID: "fc1",
			State:             store.CoupangSourceMappingVerified,
			VerifiedAt:        staleAt,
		},
	}); err != nil {
		t.Fatalf("upsert product: %v", err)
	}
	if err := priceStore.UpdateSnapshot(ctx, store.CoupangSnapshot{
		TrackID:              trackID,
		Price:                13000,
		LastSeenAt:           staleAt,
		LastRefreshAttemptAt: staleAt,
		RefreshSource:        "fallcent",
		Tier:                 store.CoupangTierWarm,
	}); err != nil {
		t.Fatalf("update snapshot: %v", err)
	}

	var detailCalls int32
	cfg := trackerTestConfig()
	cfg.ReadRefreshTimeout = 150 * time.Millisecond
	cfg.ReadRefreshJoinWait = 500 * time.Millisecond
	tracker := NewCoupangTracker(
		priceStore,
		nil,
		stubFallcentFetcher{
			detailFn: func(string) (*providers.FallcentProductData, error) {
				atomic.AddInt32(&detailCalls, 1)
				time.Sleep(40 * time.Millisecond)
				return &providers.FallcentProductData{
					FallcentProductID: "fc1",
					ProductID:         "9334776688",
					ItemID:            "20787679097",
					Name:              "stale tracked",
					Price:             12900,
				}, nil
			},
		},
		nil,
		cfg,
		nil,
	)

	url := "https://www.coupang.com/vp/products/9334776688?itemId=20787679097"
	start := make(chan struct{})
	results := make([]*CoupangLookupResult, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			results[idx], errs[idx] = lookupWithBusyRetry(tracker, url)
		}(i)
	}
	close(start)
	wg.Wait()

	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("lookup %d error = %v", i, errs[i])
		}
		if results[i] == nil {
			t.Fatalf("lookup %d result is nil", i)
		}
	}
	freshCount := 0
	for i := range results {
		if !results[i].IsStale {
			freshCount++
		}
	}
	if freshCount == 0 {
		t.Fatalf("expected at least one fresh response after coalesced read-refresh: %#v %#v", results[0], results[1])
	}
	if got := atomic.LoadInt32(&detailCalls); got != 1 {
		t.Fatalf("read refresh detail calls = %d, want 1", got)
	}
}

func TestCoupangTrackerRefreshFallsBackToAuxiliaryProvider(t *testing.T) {
	t.Parallel()

	priceStore := newTrackerTestStore(t)
	ctx := context.Background()
	if err := priceStore.UpsertProduct(ctx, store.CoupangProductRecord{
		TrackID:   "9334776688#i:20787679097",
		ProductID: "9334776688",
		ItemID:    "20787679097",
		Name:      "fallback candidate",
		Snapshot:  store.CoupangSnapshot{Tier: store.CoupangTierWarm},
		SourceMapping: store.CoupangSourceMapping{
			TrackID: "9334776688#i:20787679097",
			State:   store.CoupangSourceMappingNeedsRecheck,
		},
	}); err != nil {
		t.Fatalf("upsert product: %v", err)
	}

	cfg := trackerTestConfig()
	cfg.AllowAuxiliaryFallback = true
	cfg.ResolutionBudgetPerHour = 1
	tracker := NewCoupangTracker(
		priceStore,
		stubCoupangFetcher{
			product: &providers.CoupangProduct{
				ProductID: "9334776688",
				ItemID:    "20787679097",
				Name:      "fallback candidate",
				Price:     8800,
				ImageURL:  "https://img",
			},
		},
		stubFallcentFetcher{err: errors.New("fallcent failed")},
		nil, cfg,
		nil,
	)
	if err := tracker.RefreshProduct(ctx, "9334776688#i:20787679097", true, "test"); err != nil {
		t.Fatalf("refresh product with fallback: %v", err)
	}

	product, err := priceStore.GetProduct(ctx, "9334776688#i:20787679097")
	if err != nil {
		t.Fatalf("get product after fallback refresh: %v", err)
	}
	if product.Snapshot.Price != 8800 {
		t.Fatalf("snapshot price = %d, want 8800", product.Snapshot.Price)
	}
	if product.Snapshot.RefreshSource != "coupang_aux_refresh" {
		t.Fatalf("refresh source = %q, want coupang_aux_refresh", product.Snapshot.RefreshSource)
	}
}

func TestCoupangTrackerMarksMappingForRecheckOnDetailMismatch(t *testing.T) {
	t.Parallel()

	priceStore := newTrackerTestStore(t)
	ctx := context.Background()
	now := time.Now()
	if err := priceStore.UpsertProduct(ctx, store.CoupangProductRecord{
		TrackID:   "9334776688#i:20787679097",
		ProductID: "9334776688",
		ItemID:    "20787679097",
		Name:      "recheck candidate",
		Snapshot:  store.CoupangSnapshot{Tier: store.CoupangTierWarm},
		SourceMapping: store.CoupangSourceMapping{
			TrackID:           "9334776688#i:20787679097",
			FallcentProductID: "fc1",
			SearchKeyword:     "recheck candidate",
			State:             store.CoupangSourceMappingVerified,
			VerifiedAt:        now,
		},
	}); err != nil {
		t.Fatalf("upsert product: %v", err)
	}

	cfg := trackerTestConfig()
	cfg.AllowAuxiliaryFallback = false
	tracker := NewCoupangTracker(
		priceStore,
		nil,
		stubFallcentFetcher{
			detail: &providers.FallcentProductData{
				FallcentProductID: "fc1",
				ProductID:         "1111111111",
				ItemID:            "999",
				Name:              "other product",
				Price:             12000,
			},
		},
		nil, cfg,
		nil,
	)

	if err := tracker.RefreshProduct(ctx, "9334776688#i:20787679097", false, "test"); !errors.Is(err, ErrCoupangRefreshLimited) {
		t.Fatalf("refresh product error = %v, want ErrCoupangRefreshLimited", err)
	}

	mapping, err := priceStore.GetSourceMapping(ctx, "9334776688#i:20787679097")
	if err != nil {
		t.Fatalf("get source mapping: %v", err)
	}
	if mapping.State != store.CoupangSourceMappingNeedsRecheck {
		t.Fatalf("mapping state = %s, want needs_recheck", mapping.State)
	}
}

func TestCoupangTrackerBacksOffWhenCoupangAccessIsBlocked(t *testing.T) {
	t.Parallel()

	priceStore := newTrackerTestStore(t)
	ctx := context.Background()
	trackID := "2438266#v:3059524073"
	if err := priceStore.UpsertProduct(ctx, store.CoupangProductRecord{
		TrackID:      trackID,
		ProductID:    "2438266",
		ItemID:       "17432050974",
		VendorItemID: "3059524073",
		Name:         "blocked product",
		Snapshot:     store.CoupangSnapshot{Tier: store.CoupangTierCold},
		SourceMapping: store.CoupangSourceMapping{
			TrackID: trackID,
			State:   store.CoupangSourceMappingNeedsRecheck,
		},
	}); err != nil {
		t.Fatalf("upsert product: %v", err)
	}

	cfg := trackerTestConfig()
	cfg.MinRefreshInterval = time.Millisecond
	cfg.MappingRecheckBackoff = time.Hour

	var calls int32
	tracker := NewCoupangTracker(
		priceStore,
		countingCoupangFetcher{
			err:   errors.New("fetch product page: HTTP 403 from https://www.coupang.com/vp/products/2438266?itemId=17432050974&vendorItemId=3059524073"),
			calls: &calls,
		},
		stubFallcentFetcher{err: errors.New("fallcent failed")},
		nil, cfg,
		nil,
	)

	if err := tracker.RefreshProduct(ctx, trackID, true, "startup_warmup"); err != nil {
		t.Fatalf("refresh product with access block: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("fetch calls = %d, want 1", got)
	}

	mapping, err := priceStore.GetSourceMapping(ctx, trackID)
	if err != nil {
		t.Fatalf("get source mapping: %v", err)
	}
	if mapping == nil {
		t.Fatal("expected source mapping")
	}
	if mapping.State != store.CoupangSourceMappingFailed {
		t.Fatalf("mapping state = %s, want failed", mapping.State)
	}
	if mapping.FailureCount != 1 {
		t.Fatalf("failure count = %d, want 1", mapping.FailureCount)
	}

	time.Sleep(5 * time.Millisecond)
	if err := tracker.RefreshDue(ctx); err != nil {
		t.Fatalf("refresh due: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("fetch calls after backoff = %d, want 1", got)
	}
}

func trackerTestConfig() CoupangTrackerConfig {
	return CoupangTrackerConfig{
		CollectInterval:           15 * time.Minute,
		IdleTimeout:               30 * 24 * time.Hour,
		MaxProducts:               100,
		HotInterval:               time.Hour,
		WarmInterval:              6 * time.Hour,
		ColdInterval:              24 * time.Hour,
		Freshness:                 time.Hour,
		StaleThreshold:            24 * time.Hour,
		MinRefreshInterval:        30 * time.Minute,
		RefreshBudgetPerHour:      120,
		RegistrationBudgetPerHour: 30,
		ResolutionBudgetPerHour:   60,
		TierWindow:                24 * time.Hour,
		HotThreshold:              3,
		WarmThreshold:             1,
		CandidateFanout:           3,
		MappingRecheckBackoff:     6 * time.Hour,
		AllowAuxiliaryFallback:    true,
		RegistrationLatencyBudget: 2 * time.Second,
		ReadRefreshTimeout:        2 * time.Second,
		LookupCoalescingEnabled:   true,
		RegistrationJoinWait:      2 * time.Second,
		ReadRefreshJoinWait:       2 * time.Second,
		ChartBackfillInterval:     72 * time.Hour,
	}
}

func newTrackerTestStore(t *testing.T) *store.SQLitePriceStore {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "tracker.db")
	priceStore, err := store.NewSQLitePriceStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite price store: %v", err)
	}
	t.Cleanup(func() { _ = priceStore.Close() })
	return priceStore
}

// TestCoupangTrackerMobileURLFallback verifies that a mobile URL (no itemId/vendorItemId)
// resolves to an already-registered product that was registered via a desktop URL
// (suffixed TrackID), preventing duplicate registration and "보강중" notice.
func TestCoupangTrackerMobileURLFallback(t *testing.T) {
	t.Parallel()

	priceStore := newTrackerTestStore(t)
	ctx := context.Background()
	now := time.Now()

	// Pre-register a product as it would have been registered via desktop URL.
	// TrackID has vendorItemId suffix.
	if err := priceStore.UpsertProduct(ctx, store.CoupangProductRecord{
		TrackID:   "9334776688#v:91389430977",
		ProductID: "9334776688",
		ItemID:    "24374270398",
		Name:      "모바일 테스트 상품",
		Snapshot:  store.CoupangSnapshot{Tier: store.CoupangTierWarm},
		SourceMapping: store.CoupangSourceMapping{
			TrackID:             "9334776688#v:91389430977",
			FallcentProductID:   "fc-mobile-test",
			SearchKeyword:       "모바일 테스트 상품",
			State:               store.CoupangSourceMappingVerified,
			VerifiedAt:          now,
			ComparativeMinPrice: 25000,
		},
	}); err != nil {
		t.Fatalf("upsert desktop-registered product: %v", err)
	}
	if err := priceStore.UpdateSnapshot(ctx, store.CoupangSnapshot{
		TrackID:              "9334776688#v:91389430977",
		Price:                27000,
		LastSeenAt:           now,
		LastRefreshAttemptAt: now,
		RefreshSource:        "fallcent",
		Tier:                 store.CoupangTierWarm,
	}); err != nil {
		t.Fatalf("update snapshot: %v", err)
	}
	// Insert enough price history to make StatsEligible true.
	for i := 0; i < 3; i++ {
		ts := now.AddDate(0, 0, -(i + 1))
		if err := priceStore.InsertSeedPrices(ctx, "9334776688#v:91389430977", []int{26000}); err != nil {
			_ = ts
			t.Fatalf("insert seed price: %v", err)
		}
	}

	tracker := NewCoupangTracker(priceStore, nil, nil, nil, trackerTestConfig(), nil)

	// Lookup via mobile URL — bare productID, no itemId or vendorItemId.
	// This simulates m.coupang.com/vm/products/9334776688 arriving from KakaoTalk attachment.
	result, err := lookupWithBusyRetry(tracker, "https://m.coupang.com/vm/products/9334776688")
	if err != nil {
		t.Fatalf("Lookup mobile URL: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result for mobile URL lookup")
	}

	// Must NOT trigger "보강중" — the product is already registered.
	if result.RegistrationDeferredUI {
		t.Error("RegistrationDeferredUI = true, want false: mobile URL should reuse existing record")
	}

	// Must return the existing product data, not an empty record.
	if result.Product.TrackID == "" {
		t.Error("Product.TrackID is empty: expected existing product to be found")
	}
	if result.Product.Name != "모바일 테스트 상품" {
		t.Errorf("Product.Name = %q, want '모바일 테스트 상품'", result.Product.Name)
	}
	if result.Product.SourceMapping.ComparativeMinPrice != 25000 {
		t.Errorf("ComparativeMinPrice = %d, want 25000", result.Product.SourceMapping.ComparativeMinPrice)
	}

	// Must NOT have registered a duplicate bare-TrackID record.
	bareRecord, err := priceStore.GetProduct(ctx, "9334776688")
	if err != nil {
		t.Fatalf("GetProduct bare: %v", err)
	}
	if bareRecord != nil {
		t.Error("a duplicate bare TrackID record was created; expected no new registration")
	}
}

func lookupWithBusyRetry(tracker *CoupangTracker, rawURL string) (*CoupangLookupResult, error) {
	const maxAttempts = 5
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		result, err := tracker.Lookup(context.Background(), rawURL)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !strings.Contains(strings.ToUpper(err.Error()), "SQLITE_BUSY") {
			return nil, err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil, lastErr
}
