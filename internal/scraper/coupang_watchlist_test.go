package scraper

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/coupang"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/store"
)

func TestCoupangWatchlistStartRunsStartupWarmupSweep(t *testing.T) {
	t.Parallel()

	priceStore := newTrackerTestStore(t)
	ctx := context.Background()
	staleAt := time.Now().Add(-2 * time.Hour)
	trackID := "9334776688#i:20787679097"

	if err := priceStore.UpsertProduct(ctx, store.CoupangProductRecord{
		TrackID:   trackID,
		ProductID: "9334776688",
		ItemID:    "20787679097",
		Name:      "warmup tracked",
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
		LastRefreshAttemptAt: staleAt.Add(-time.Hour),
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
				Name:              "warmup tracked",
				Price:             12900,
			},
		},
		nil, trackerTestConfig(),
		nil,
	)
	watchlist := NewCoupangWatchlist(tracker, CoupangWatchlistConfig{
		CollectInterval: time.Hour,
		IdleTimeout:     30 * 24 * time.Hour,
		EvictInterval:   time.Hour,
		MaxProducts:     100,
	}, nil)

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchlist.Start(runCtx)
	}()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		product, err := priceStore.GetProduct(ctx, trackID)
		if err != nil {
			t.Fatalf("get product after startup warmup: %v", err)
		}
		if product != nil && product.Snapshot.Price == 12900 {
			cancel()
			<-done
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	<-done
	t.Fatal("startup warmup did not refresh stale product")
}

type stubFallcentFetcher struct {
	detail *providers.FallcentProductData
}

func (s stubFallcentFetcher) ResolveProduct(_ context.Context, _ *providers.CoupangURL, _ []string) (*providers.FallcentProductData, error) {
	return nil, nil
}

func (s stubFallcentFetcher) FetchProduct(_ context.Context, _ string) (*providers.FallcentProductData, error) {
	return s.detail, nil
}

func (s stubFallcentFetcher) FetchChart(_ context.Context, _ string) ([]int, error) {
	return nil, fmt.Errorf("chart not available in stub")
}

func (s stubFallcentFetcher) LookupByCoupangID(_ context.Context, _, _ string) (*providers.FallcentProductData, error) {
	return nil, fmt.Errorf("direct lookup not available in stub")
}

func trackerTestConfig() coupang.CoupangTrackerConfig {
	return coupang.CoupangTrackerConfig{
		CollectInterval:           time.Minute,
		IdleTimeout:               30 * 24 * time.Hour,
		MaxProducts:               100,
		HotInterval:               time.Minute,
		WarmInterval:              30 * time.Minute,
		ColdInterval:              time.Hour,
		Freshness:                 time.Hour,
		StaleThreshold:            2 * time.Hour,
		MinRefreshInterval:        time.Millisecond,
		RefreshBudgetPerHour:      60,
		RegistrationBudgetPerHour: 30,
		ResolutionBudgetPerHour:   30,
		TierWindow:                24 * time.Hour,
		HotThreshold:              3,
		WarmThreshold:             1,
		CandidateFanout:           3,
		MappingRecheckBackoff:     time.Hour,
		AllowAuxiliaryFallback:    true,
		ReadRefreshTimeout:        50 * time.Millisecond,
	}
}

func newTrackerTestStore(t *testing.T) *store.SQLitePriceStore {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "coupang.db")
	priceStore, err := store.NewSQLitePriceStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite price store: %v", err)
	}
	t.Cleanup(func() { _ = priceStore.Close() })
	return priceStore
}
