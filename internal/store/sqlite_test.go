package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSQLitePriceStoreBackfillsSnapshotFromHistory(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "coupang.db")
	legacyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy sqlite: %v", err)
	}
	t.Cleanup(func() { _ = legacyDB.Close() })

	legacySchema := `
	CREATE TABLE coupang_products (
		product_id TEXT PRIMARY KEY,
		vendor_item_id TEXT,
		item_id TEXT,
		name TEXT,
		image_url TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_queried DATETIME DEFAULT CURRENT_TIMESTAMP,
		query_count INTEGER DEFAULT 0
	);
	CREATE TABLE coupang_price_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		product_id TEXT NOT NULL,
		price INTEGER NOT NULL,
		is_seed BOOLEAN DEFAULT FALSE,
		fetched_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := legacyDB.Exec(legacySchema); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	if _, err := legacyDB.Exec(
		`INSERT INTO coupang_products (product_id, name, query_count) VALUES ('p1', 'legacy', 5)`,
	); err != nil {
		t.Fatalf("insert legacy product: %v", err)
	}

	expectedAt := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	if _, err := legacyDB.Exec(
		`INSERT INTO coupang_price_history (product_id, price, is_seed, fetched_at) VALUES ('p1', 21900, FALSE, ?)`,
		expectedAt,
	); err != nil {
		t.Fatalf("insert history: %v", err)
	}
	_ = legacyDB.Close()

	store, err := NewSQLitePriceStore(dbPath)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	product, err := store.GetProduct(context.Background(), "p1")
	if err != nil {
		t.Fatalf("get product: %v", err)
	}
	if product == nil {
		t.Fatal("expected product after migration")
	}
	if product.Snapshot.Price != 21900 {
		t.Fatalf("snapshot price = %d, want 21900", product.Snapshot.Price)
	}
	if product.Snapshot.LastSeenAt.IsZero() {
		t.Fatal("expected snapshot timestamp to be backfilled")
	}
	if product.Snapshot.RefreshSource == "" {
		t.Fatal("expected snapshot source to be backfilled")
	}
	if product.RecentQueryCount == 0 {
		t.Fatal("expected recent query count to be initialized")
	}
}

func TestSQLitePriceStoreTouchProductResetsRecentQueryWindow(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "touch.db")
	store, err := NewSQLitePriceStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	if err := store.UpsertProduct(ctx, CoupangProductRecord{
		ProductID: "p2",
		Name:      "touch",
	}); err != nil {
		t.Fatalf("upsert product: %v", err)
	}

	oldWindow := time.Now().Add(-48 * time.Hour)
	if _, err := store.sdb.Write.Exec(
		`UPDATE coupang_products SET recent_query_count = 7, query_window_started_at = ? WHERE product_id = 'p2'`,
		oldWindow,
	); err != nil {
		t.Fatalf("force old query window: %v", err)
	}

	if err := store.TouchProduct(ctx, "p2", 24*time.Hour); err != nil {
		t.Fatalf("touch product: %v", err)
	}

	product, err := store.GetProduct(ctx, "p2")
	if err != nil {
		t.Fatalf("get product: %v", err)
	}
	if product.RecentQueryCount != 1 {
		t.Fatalf("recent query count = %d, want 1", product.RecentQueryCount)
	}
	if time.Since(product.QueryWindowStartedAt) > time.Minute {
		t.Fatalf("query window was not reset: %v", product.QueryWindowStartedAt)
	}
}

func TestSQLitePriceStoreSourceMappingLifecycle(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "mapping.db")
	store, err := NewSQLitePriceStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	if err := store.UpsertProduct(ctx, CoupangProductRecord{
		TrackID:   "9334776688#i:20787679097",
		ProductID: "9334776688",
		ItemID:    "20787679097",
		Name:      "mapping target",
	}); err != nil {
		t.Fatalf("upsert product: %v", err)
	}

	verifiedAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	if err := store.UpsertSourceMapping(ctx, CoupangSourceMapping{
		TrackID:             "9334776688#i:20787679097",
		FallcentProductID:   "fc-verified",
		SearchKeyword:       "mapping target",
		State:               CoupangSourceMappingVerified,
		VerifiedAt:          verifiedAt,
		ComparativeMinPrice: 9300,
	}); err != nil {
		t.Fatalf("upsert source mapping: %v", err)
	}

	mapping, err := store.GetSourceMapping(ctx, "9334776688#i:20787679097")
	if err != nil {
		t.Fatalf("get source mapping: %v", err)
	}
	if mapping == nil {
		t.Fatal("expected source mapping")
	}
	if mapping.FallcentProductID != "fc-verified" {
		t.Fatalf("fallcent product id = %q, want fc-verified", mapping.FallcentProductID)
	}
	if mapping.SearchKeyword != "mapping target" {
		t.Fatalf("search keyword = %q, want mapping target", mapping.SearchKeyword)
	}
	if mapping.State != CoupangSourceMappingVerified {
		t.Fatalf("state = %s, want verified", mapping.State)
	}
	if !mapping.VerifiedAt.Equal(verifiedAt) {
		t.Fatalf("verified at = %v, want %v", mapping.VerifiedAt, verifiedAt)
	}
	if mapping.ComparativeMinPrice != 9300 {
		t.Fatalf("comparative min = %d, want 9300", mapping.ComparativeMinPrice)
	}

	if err := store.MarkSourceMappingState(ctx, "9334776688#i:20787679097", CoupangSourceMappingNeedsRecheck, "detail verification failed"); err != nil {
		t.Fatalf("mark source mapping state needs_recheck: %v", err)
	}

	mapping, err = store.GetSourceMapping(ctx, "9334776688#i:20787679097")
	if err != nil {
		t.Fatalf("get source mapping after recheck: %v", err)
	}
	if mapping.State != CoupangSourceMappingNeedsRecheck {
		t.Fatalf("state = %s, want needs_recheck", mapping.State)
	}
	if mapping.FailureCount != 1 {
		t.Fatalf("failure count = %d, want 1", mapping.FailureCount)
	}
	if mapping.LastFailureReason != "detail verification failed" {
		t.Fatalf("failure reason = %q", mapping.LastFailureReason)
	}

	if err := store.MarkSourceMappingState(ctx, "9334776688#i:20787679097", CoupangSourceMappingVerified, ""); err != nil {
		t.Fatalf("mark source mapping state verified: %v", err)
	}

	mapping, err = store.GetSourceMapping(ctx, "9334776688#i:20787679097")
	if err != nil {
		t.Fatalf("get source mapping after verify: %v", err)
	}
	if mapping.State != CoupangSourceMappingVerified {
		t.Fatalf("state = %s, want verified", mapping.State)
	}
	if mapping.FailureCount != 0 {
		t.Fatalf("failure count = %d, want 0", mapping.FailureCount)
	}
	if mapping.LastFailureReason != "" {
		t.Fatalf("failure reason = %q, want empty", mapping.LastFailureReason)
	}
	if mapping.VerifiedAt.IsZero() {
		t.Fatal("expected verified_at to be updated")
	}
}

func TestSQLitePriceStoreRoundTripHistoryAndWatchedProducts(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "roundtrip.db")
	store, err := NewSQLitePriceStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	trackID := "9334776688#i:20787679097"
	if err := store.UpsertProduct(ctx, CoupangProductRecord{
		TrackID:   trackID,
		ProductID: "9334776688",
		ItemID:    "20787679097",
		Name:      "roundtrip target",
		Snapshot:  CoupangSnapshot{Tier: CoupangTierWarm},
	}); err != nil {
		t.Fatalf("upsert product: %v", err)
	}
	if err := store.UpsertSourceMapping(ctx, CoupangSourceMapping{
		TrackID:             trackID,
		FallcentProductID:   "fc-roundtrip",
		SearchKeyword:       "roundtrip target",
		State:               CoupangSourceMappingVerified,
		VerifiedAt:          time.Now().UTC().Truncate(time.Second),
		ComparativeMinPrice: 10100,
	}); err != nil {
		t.Fatalf("upsert source mapping: %v", err)
	}
	if err := store.UpdateSnapshot(ctx, CoupangSnapshot{
		TrackID:              trackID,
		Price:                10900,
		LastSeenAt:           time.Now().UTC().Truncate(time.Second),
		LastRefreshAttemptAt: time.Now().UTC().Truncate(time.Second),
		RefreshSource:        "fallcent",
		Tier:                 CoupangTierWarm,
	}); err != nil {
		t.Fatalf("update snapshot: %v", err)
	}

	if err := store.InsertPrice(ctx, trackID, 10900, false); err != nil {
		t.Fatalf("insert price #1: %v", err)
	}
	if err := store.InsertPrice(ctx, trackID, 10700, false); err != nil {
		t.Fatalf("insert price #2: %v", err)
	}

	history, err := store.GetPriceHistory(ctx, trackID, time.Now().Add(-365*24*time.Hour))
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history len = %d, want 2", len(history))
	}

	watched, err := store.ListWatchedProducts(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("list watched: %v", err)
	}
	if len(watched) != 1 {
		t.Fatalf("watched len = %d, want 1", len(watched))
	}
	if watched[0].TrackID != trackID {
		t.Fatalf("track id = %q, want %q", watched[0].TrackID, trackID)
	}
	if watched[0].SourceMapping.State != CoupangSourceMappingVerified {
		t.Fatalf("source mapping state = %s, want verified", watched[0].SourceMapping.State)
	}
	if watched[0].SourceMapping.FallcentProductID != "fc-roundtrip" {
		t.Fatalf("fallcent id = %q, want fc-roundtrip", watched[0].SourceMapping.FallcentProductID)
	}
}

func TestSQLitePriceStoreMaintenanceAndStatsPaths(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "maintenance.db")
	store, err := NewSQLitePriceStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()

	if err := store.UpsertProduct(ctx, CoupangProductRecord{
		TrackID:   "p1",
		ProductID: "base-1",
		Name:      "before",
		Snapshot:  CoupangSnapshot{Tier: CoupangTierWarm},
	}); err != nil {
		t.Fatalf("upsert product p1: %v", err)
	}
	if err := store.UpdateProductMetadata(ctx, CoupangProductRecord{
		TrackID:   "p1",
		ProductID: "base-1",
		Name:      "after",
		ImageURL:  "https://img",
		Snapshot:  CoupangSnapshot{Tier: CoupangTierHot},
	}); err != nil {
		t.Fatalf("update metadata: %v", err)
	}
	p1, err := store.GetProduct(ctx, "p1")
	if err != nil || p1 == nil {
		t.Fatalf("get p1 after metadata update = (%+v,%v)", p1, err)
	}
	if p1.Name != "after" || p1.ImageURL != "https://img" {
		t.Fatalf("metadata update not applied: %+v", p1)
	}

	if err := store.InsertSeedPrices(ctx, "p1", nil); err != nil {
		t.Fatalf("insert empty seed prices: %v", err)
	}
	if err := store.InsertSeedPrices(ctx, "p1", []int{12000, 11000, 10000}); err != nil {
		t.Fatalf("insert seed prices: %v", err)
	}
	if has, err := store.HasSeedPrices(ctx, "p1"); err != nil || !has {
		t.Fatalf("has seed prices = (%v,%v)", has, err)
	}

	if err := store.InsertPrice(ctx, "p1", 13000, false); err != nil {
		t.Fatalf("insert non-seed price: %v", err)
	}
	stats, err := store.GetPriceStats(ctx, "p1")
	if err != nil || stats == nil {
		t.Fatalf("get price stats = (%+v,%v)", stats, err)
	}
	if stats.TotalPoints != 4 || stats.MinPrice <= 0 || stats.MaxPrice <= 0 || stats.AvgPrice <= 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	none, err := store.GetPriceStats(ctx, "no-such-product")
	if err != nil || none != nil {
		t.Fatalf("expected nil stats for unknown product, got (%+v,%v)", none, err)
	}

	if err := store.MarkRefreshState(ctx, "p1", time.Time{}, true); err != nil {
		t.Fatalf("mark refresh in flight: %v", err)
	}
	if err := store.SetProductTier(ctx, "p1", CoupangTierHot); err != nil {
		t.Fatalf("set tier: %v", err)
	}
	p1, err = store.GetProduct(ctx, "p1")
	if err != nil || p1 == nil {
		t.Fatalf("get p1 after refresh/tier = (%+v,%v)", p1, err)
	}
	if !p1.Snapshot.RefreshInFlight {
		t.Fatalf("expected refresh_in_flight=true: %+v", p1.Snapshot)
	}
	if p1.Snapshot.Tier != CoupangTierHot {
		t.Fatalf("expected tier hot, got %s", p1.Snapshot.Tier)
	}

	if err := store.UpsertProduct(ctx, CoupangProductRecord{
		TrackID:   "old",
		ProductID: "old",
		Name:      "old",
		Snapshot:  CoupangSnapshot{Tier: CoupangTierCold},
	}); err != nil {
		t.Fatalf("upsert old product: %v", err)
	}
	if err := store.InsertPrice(ctx, "old", 9000, false); err != nil {
		t.Fatalf("insert old product price: %v", err)
	}
	if _, err := store.sdb.Write.ExecContext(ctx, `UPDATE coupang_products SET last_queried = ? WHERE product_id = ?`, time.Now().Add(-72*time.Hour), "old"); err != nil {
		t.Fatalf("force old last_queried: %v", err)
	}

	count, err := store.CountTrackedProducts(ctx)
	if err != nil || count != 2 {
		t.Fatalf("count tracked products = (%d,%v), want 2", count, err)
	}

	evicted, err := store.EvictStaleProducts(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("evict stale products: %v", err)
	}
	if evicted != 1 {
		t.Fatalf("evicted count = %d, want 1", evicted)
	}
	if prod, err := store.GetProduct(ctx, "old"); err != nil || prod != nil {
		t.Fatalf("expected old product removed, got (%+v,%v)", prod, err)
	}

	deleted, err := store.DeleteProducts(ctx, []string{"p1"})
	if err != nil {
		t.Fatalf("delete products: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted count = %d, want 1", deleted)
	}
	if deleted, err := store.DeleteProducts(ctx, nil); err != nil || deleted != 0 {
		t.Fatalf("delete empty products = (%d,%v)", deleted, err)
	}
}

func TestSQLitePriceStoreReplaceSeedPrices(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "replace_seed.db")
	store, err := NewSQLitePriceStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	trackID := "replace-test"
	if err := store.UpsertProduct(ctx, CoupangProductRecord{
		TrackID:   trackID,
		ProductID: trackID,
		Name:      "replace seed target",
		Snapshot:  CoupangSnapshot{Tier: CoupangTierWarm},
	}); err != nil {
		t.Fatalf("upsert product: %v", err)
	}

	// Insert initial seeds and a non-seed price
	if err := store.InsertSeedPrices(ctx, trackID, []int{10000, 11000, 12000}); err != nil {
		t.Fatalf("insert seed prices: %v", err)
	}
	if err := store.InsertPrice(ctx, trackID, 13000, false); err != nil {
		t.Fatalf("insert non-seed price: %v", err)
	}

	// Replace seeds with new data
	if err := store.ReplaceSeedPrices(ctx, trackID, []int{9000, 9500, 10000, 10500}); err != nil {
		t.Fatalf("replace seed prices: %v", err)
	}

	// Verify: 4 new seeds + 1 non-seed = 5 total
	stats, err := store.GetPriceStats(ctx, trackID)
	if err != nil || stats == nil {
		t.Fatalf("get stats: (%+v, %v)", stats, err)
	}
	if stats.TotalPoints != 5 {
		t.Fatalf("total points = %d, want 5", stats.TotalPoints)
	}
	if stats.MinPrice != 9000 {
		t.Fatalf("min price = %d, want 9000", stats.MinPrice)
	}

	// Verify non-seed price is preserved
	history, err := store.GetPriceHistory(ctx, trackID, time.Now().Add(-365*24*time.Hour))
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	nonSeedCount := 0
	for _, p := range history {
		if !p.IsSeed {
			nonSeedCount++
			if p.Price != 13000 {
				t.Fatalf("non-seed price = %d, want 13000", p.Price)
			}
		}
	}
	if nonSeedCount != 1 {
		t.Fatalf("non-seed count = %d, want 1", nonSeedCount)
	}
}

func TestSQLitePriceStoreReplaceSeedPricesEmpty(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "replace_seed_empty.db")
	store, err := NewSQLitePriceStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Empty prices should be a no-op
	if err := store.ReplaceSeedPrices(context.Background(), "no-product", nil); err != nil {
		t.Fatalf("replace empty: %v", err)
	}
}

func TestSQLitePriceStoreMarkChartBackfillAt(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "backfill_at.db")
	store, err := NewSQLitePriceStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	trackID := "backfill-test"
	if err := store.UpsertProduct(ctx, CoupangProductRecord{
		TrackID:   trackID,
		ProductID: trackID,
		Name:      "backfill target",
		Snapshot:  CoupangSnapshot{Tier: CoupangTierWarm},
	}); err != nil {
		t.Fatalf("upsert product: %v", err)
	}

	// Initially zero
	product, err := store.GetProduct(ctx, trackID)
	if err != nil || product == nil {
		t.Fatalf("get product: (%+v, %v)", product, err)
	}
	if !product.SourceMapping.LastChartBackfillAt.IsZero() {
		t.Fatalf("expected zero LastChartBackfillAt, got %v", product.SourceMapping.LastChartBackfillAt)
	}

	// Mark it
	now := time.Now().UTC().Truncate(time.Second)
	if err := store.MarkChartBackfillAt(ctx, trackID, now); err != nil {
		t.Fatalf("mark chart backfill: %v", err)
	}

	product, err = store.GetProduct(ctx, trackID)
	if err != nil || product == nil {
		t.Fatalf("get product after mark: (%+v, %v)", product, err)
	}
	if product.SourceMapping.LastChartBackfillAt.IsZero() {
		t.Fatal("expected non-zero LastChartBackfillAt after mark")
	}
}

func TestSQLitePriceStoreGetProductByBaseProductID(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "base_product_id.db")
	store, err := NewSQLitePriceStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()

	// Register a product with a suffixed TrackID (desktop URL registration).
	if err := store.UpsertProduct(ctx, CoupangProductRecord{
		TrackID:   "9334776688#v:91389430977",
		ProductID: "9334776688",
		ItemID:    "24374270398",
		Name:      "desktop registered product",
		Snapshot:  CoupangSnapshot{Tier: CoupangTierWarm},
	}); err != nil {
		t.Fatalf("upsert suffixed product: %v", err)
	}

	// Exact lookup by TrackID should work.
	got, err := store.GetProduct(ctx, "9334776688#v:91389430977")
	if err != nil || got == nil {
		t.Fatalf("GetProduct exact = (%+v, %v)", got, err)
	}

	// Bare TrackID exact lookup should return nil (no bare record exists).
	bare, err := store.GetProduct(ctx, "9334776688")
	if err != nil {
		t.Fatalf("GetProduct bare error: %v", err)
	}
	if bare != nil {
		t.Fatalf("GetProduct bare should return nil, got %+v", bare)
	}

	// base_product_id fallback should find the suffixed record.
	byBase, err := store.GetProductByBaseProductID(ctx, "9334776688")
	if err != nil {
		t.Fatalf("GetProductByBaseProductID error: %v", err)
	}
	if byBase == nil {
		t.Fatal("GetProductByBaseProductID returned nil, want suffixed record")
	}
	if byBase.TrackID != "9334776688#v:91389430977" {
		t.Fatalf("TrackID = %q, want 9334776688#v:91389430977", byBase.TrackID)
	}
	if byBase.Name != "desktop registered product" {
		t.Fatalf("Name = %q, want 'desktop registered product'", byBase.Name)
	}

	// Unknown base product ID returns nil without error.
	none, err := store.GetProductByBaseProductID(ctx, "0000000000")
	if err != nil {
		t.Fatalf("GetProductByBaseProductID unknown error: %v", err)
	}
	if none != nil {
		t.Fatalf("expected nil for unknown base product id, got %+v", none)
	}

	// When multiple records share the same base_product_id, GetProductByBaseProductID
	// returns one of them (ORDER BY last_queried DESC). We just verify a valid record is returned.
	if err := store.UpsertProduct(ctx, CoupangProductRecord{
		TrackID:   "9334776688#v:99999999",
		ProductID: "9334776688",
		Name:      "second option",
		Snapshot:  CoupangSnapshot{Tier: CoupangTierWarm},
	}); err != nil {
		t.Fatalf("upsert second option: %v", err)
	}

	multi, err := store.GetProductByBaseProductID(ctx, "9334776688")
	if err != nil {
		t.Fatalf("GetProductByBaseProductID multi-option error: %v", err)
	}
	if multi == nil {
		t.Fatal("GetProductByBaseProductID multi-option returned nil")
	}
	validTrackIDs := map[string]bool{
		"9334776688#v:91389430977": true,
		"9334776688#v:99999999":    true,
	}
	if !validTrackIDs[multi.TrackID] {
		t.Fatalf("unexpected TrackID = %q in multi-option result", multi.TrackID)
	}
}
