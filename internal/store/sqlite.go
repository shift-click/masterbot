package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/shift-click/masterbot/internal/coupang"
	"github.com/shift-click/masterbot/internal/sqliteutil"
)

type PriceStore = coupang.Repository
type CoupangRefreshTier = coupang.CoupangRefreshTier
type CoupangSourceMappingState = coupang.CoupangSourceMappingState
type CoupangSnapshot = coupang.CoupangSnapshot
type CoupangSourceMapping = coupang.CoupangSourceMapping
type CoupangProductRecord = coupang.CoupangProductRecord
type PricePoint = coupang.PricePoint
type PriceStats = coupang.PriceStats

const (
	CoupangTierHot                   = coupang.CoupangTierHot
	CoupangTierWarm                  = coupang.CoupangTierWarm
	CoupangTierCold                  = coupang.CoupangTierCold
	CoupangSourceMappingUnknown      = coupang.CoupangSourceMappingUnknown
	CoupangSourceMappingVerified     = coupang.CoupangSourceMappingVerified
	CoupangSourceMappingNeedsRecheck = coupang.CoupangSourceMappingNeedsRecheck
	CoupangSourceMappingFailed       = coupang.CoupangSourceMappingFailed
)

// SQLitePriceStore implements PriceStore using SQLite.
type SQLitePriceStore struct {
	sdb *sqliteutil.SQLiteDB
}

// NewSQLitePriceStore opens or creates a SQLite database at dbPath.
func NewSQLitePriceStore(dbPath string) (*SQLitePriceStore, error) {
	sdb, err := sqliteutil.OpenSQLiteDB(dbPath)
	if err != nil {
		return nil, err
	}

	store := &SQLitePriceStore{sdb: sdb}
	if err := store.migrate(); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}

	if err := sdb.OptimizeFull(); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("initial optimize: %w", err)
	}

	return store, nil
}

func (s *SQLitePriceStore) Close() error {
	_ = s.sdb.Optimize()
	return s.sdb.Close()
}

// UpsertProduct inserts or updates a product record and counts it as a user query.
func (s *SQLitePriceStore) UpsertProduct(ctx context.Context, p CoupangProductRecord) error {
	trackID := trackIDForRecord(p)
	query := `
	INSERT INTO coupang_products (
		product_id, base_product_id, vendor_item_id, item_id, name, image_url,
		last_queried, query_count, recent_query_count, query_window_started_at, refresh_tier,
		fallcent_product_id, fallcent_search_keyword, fallcent_mapping_state, fallcent_verified_at,
		fallcent_failure_count, fallcent_failure_reason, fallcent_lowest_price
	)
	VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, 1, 1, CURRENT_TIMESTAMP, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(product_id) DO UPDATE SET
		base_product_id = COALESCE(NULLIF(excluded.base_product_id, ''), base_product_id),
		vendor_item_id = COALESCE(NULLIF(excluded.vendor_item_id, ''), vendor_item_id),
		item_id = COALESCE(NULLIF(excluded.item_id, ''), item_id),
		name = COALESCE(NULLIF(excluded.name, ''), name),
		image_url = COALESCE(NULLIF(excluded.image_url, ''), image_url),
		fallcent_product_id = COALESCE(NULLIF(excluded.fallcent_product_id, ''), fallcent_product_id),
		fallcent_search_keyword = COALESCE(NULLIF(excluded.fallcent_search_keyword, ''), fallcent_search_keyword),
		fallcent_mapping_state = COALESCE(NULLIF(excluded.fallcent_mapping_state, ''), fallcent_mapping_state),
		fallcent_verified_at = COALESCE(excluded.fallcent_verified_at, fallcent_verified_at),
		fallcent_failure_count = CASE
			WHEN excluded.fallcent_mapping_state <> '' THEN excluded.fallcent_failure_count
			ELSE fallcent_failure_count
		END,
		fallcent_failure_reason = CASE
			WHEN excluded.fallcent_mapping_state <> '' THEN excluded.fallcent_failure_reason
			ELSE fallcent_failure_reason
		END,
		fallcent_lowest_price = CASE
			WHEN excluded.fallcent_lowest_price > 0 THEN excluded.fallcent_lowest_price
			ELSE fallcent_lowest_price
		END,
		last_queried = CURRENT_TIMESTAMP,
		query_count = query_count + 1,
		recent_query_count = recent_query_count + 1
	`
	state := normalizeSourceMappingState(p.SourceMapping.State)
	var verifiedAt interface{}
	if !p.SourceMapping.VerifiedAt.IsZero() {
		verifiedAt = p.SourceMapping.VerifiedAt
	}
	_, err := s.sdb.Write.ExecContext(
		ctx,
		query,
		trackID, p.ProductID, p.VendorItemID, p.ItemID, p.Name, p.ImageURL,
		normalizeTier(p.Snapshot.Tier),
		p.SourceMapping.FallcentProductID,
		p.SourceMapping.SearchKeyword,
		state,
		verifiedAt,
		p.SourceMapping.FailureCount,
		p.SourceMapping.LastFailureReason,
		p.SourceMapping.ComparativeMinPrice,
	)
	return err
}

func (s *SQLitePriceStore) UpdateProductMetadata(ctx context.Context, p CoupangProductRecord) error {
	trackID := trackIDForRecord(p)
	query := `
	INSERT INTO coupang_products (product_id, base_product_id, vendor_item_id, item_id, name, image_url, refresh_tier)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(product_id) DO UPDATE SET
		base_product_id = COALESCE(NULLIF(excluded.base_product_id, ''), base_product_id),
		vendor_item_id = COALESCE(NULLIF(excluded.vendor_item_id, ''), vendor_item_id),
		item_id = COALESCE(NULLIF(excluded.item_id, ''), item_id),
		name = COALESCE(NULLIF(excluded.name, ''), name),
		image_url = COALESCE(NULLIF(excluded.image_url, ''), image_url),
		refresh_tier = COALESCE(NULLIF(excluded.refresh_tier, ''), refresh_tier)
	`
	_, err := s.sdb.Write.ExecContext(ctx, query, trackID, p.ProductID, p.VendorItemID, p.ItemID, p.Name, p.ImageURL, normalizeTier(p.Snapshot.Tier))
	return err
}

// GetProduct retrieves a product by its ID.
func (s *SQLitePriceStore) GetProduct(ctx context.Context, productID string) (*CoupangProductRecord, error) {
	query := `
		SELECT product_id, base_product_id, vendor_item_id, item_id, name, image_url, created_at, last_queried, query_count,
		       recent_query_count, query_window_started_at, last_seen_price, last_seen_at,
		       last_refresh_attempt_at, refresh_source, refresh_tier, refresh_in_flight,
		       fallcent_product_id, fallcent_search_keyword, fallcent_mapping_state, fallcent_verified_at,
		       fallcent_failure_count, fallcent_failure_reason, fallcent_lowest_price,
		       fallcent_last_chart_backfill_at
		FROM coupang_products
		WHERE product_id = ?
	`

	row := s.sdb.Read.QueryRowContext(ctx, query, productID)
	record, err := scanProduct(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// GetProductByBaseProductID retrieves the most recently queried product matching the given base product ID.
// Used as a fallback when a bare productID TrackID (from mobile URLs) does not match an existing record
// that was registered with a suffixed TrackID (e.g. "12345#v:67890").
func (s *SQLitePriceStore) GetProductByBaseProductID(ctx context.Context, baseProductID string) (*CoupangProductRecord, error) {
	query := `
		SELECT product_id, base_product_id, vendor_item_id, item_id, name, image_url, created_at, last_queried, query_count,
		       recent_query_count, query_window_started_at, last_seen_price, last_seen_at,
		       last_refresh_attempt_at, refresh_source, refresh_tier, refresh_in_flight,
		       fallcent_product_id, fallcent_search_keyword, fallcent_mapping_state, fallcent_verified_at,
		       fallcent_failure_count, fallcent_failure_reason, fallcent_lowest_price,
		       fallcent_last_chart_backfill_at
		FROM coupang_products
		WHERE base_product_id = ?
		ORDER BY last_queried DESC
		LIMIT 1
	`
	row := s.sdb.Read.QueryRowContext(ctx, query, baseProductID)
	record, err := scanProduct(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *SQLitePriceStore) GetSourceMapping(ctx context.Context, productID string) (*CoupangSourceMapping, error) {
	record, err := s.GetProduct(ctx, productID)
	if err != nil || record == nil {
		return nil, err
	}
	if record.SourceMapping.TrackID == "" {
		record.SourceMapping.TrackID = record.TrackID
	}
	return &record.SourceMapping, nil
}

func (s *SQLitePriceStore) UpsertSourceMapping(ctx context.Context, mapping CoupangSourceMapping) error {
	if mapping.TrackID == "" {
		return fmt.Errorf("source mapping track id is required")
	}
	state := normalizeSourceMappingState(mapping.State)
	var verifiedAt interface{}
	if !mapping.VerifiedAt.IsZero() {
		verifiedAt = mapping.VerifiedAt
	}
	_, err := s.sdb.Write.ExecContext(ctx, `
		UPDATE coupang_products
		SET fallcent_product_id = CASE
		        WHEN ? <> '' THEN ?
		        ELSE fallcent_product_id
		    END,
		    fallcent_search_keyword = CASE
		        WHEN ? <> '' THEN ?
		        ELSE fallcent_search_keyword
		    END,
		    fallcent_mapping_state = ?,
		    fallcent_verified_at = CASE
		        WHEN ? IS NOT NULL THEN ?
		        ELSE fallcent_verified_at
		    END,
		    fallcent_failure_count = ?,
		    fallcent_failure_reason = ?,
		    fallcent_lowest_price = ?
		WHERE product_id = ?
	`, mapping.FallcentProductID, mapping.FallcentProductID,
		mapping.SearchKeyword, mapping.SearchKeyword,
		state,
		verifiedAt, verifiedAt,
		mapping.FailureCount,
		mapping.LastFailureReason,
		mapping.ComparativeMinPrice,
		mapping.TrackID,
	)
	return err
}

func (s *SQLitePriceStore) MarkSourceMappingState(ctx context.Context, productID string, state CoupangSourceMappingState, failureReason string) error {
	normalized := normalizeSourceMappingState(state)
	var query string
	var args []interface{}
	switch CoupangSourceMappingState(normalized) {
	case CoupangSourceMappingVerified:
		query = `
			UPDATE coupang_products
			SET fallcent_mapping_state = ?,
			    fallcent_failure_count = 0,
			    fallcent_failure_reason = '',
			    fallcent_verified_at = CURRENT_TIMESTAMP
			WHERE product_id = ?
		`
		args = []interface{}{normalized, productID}
	default:
		query = `
			UPDATE coupang_products
			SET fallcent_mapping_state = ?,
			    fallcent_failure_count = fallcent_failure_count + 1,
			    fallcent_failure_reason = ?
			WHERE product_id = ?
		`
		args = []interface{}{normalized, failureReason, productID}
	}
	_, err := s.sdb.Write.ExecContext(ctx, query, args...)
	return err
}

// TouchProduct updates last_queried timestamp and query counters for an existing product.
func (s *SQLitePriceStore) TouchProduct(ctx context.Context, productID string, queryWindow time.Duration) error {
	cutoff := time.Now().Add(-queryWindow)
	_, err := s.sdb.Write.ExecContext(ctx, `
		UPDATE coupang_products
		SET last_queried = CURRENT_TIMESTAMP,
		    query_count = query_count + 1,
		    recent_query_count = CASE
		        WHEN query_window_started_at IS NULL OR query_window_started_at < ? THEN 1
		        ELSE recent_query_count + 1
		    END,
		    query_window_started_at = CASE
		        WHEN query_window_started_at IS NULL OR query_window_started_at < ? THEN CURRENT_TIMESTAMP
		        ELSE query_window_started_at
		    END
		WHERE product_id = ?
	`, cutoff, cutoff, productID)
	return err
}

// InsertPrice records a single price observation.
func (s *SQLitePriceStore) InsertPrice(ctx context.Context, productID string, price int, isSeed bool) error {
	_, err := s.sdb.Write.ExecContext(ctx,
		`INSERT INTO coupang_price_history (product_id, price, is_seed) VALUES (?, ?, ?)`,
		productID, price, isSeed,
	)
	return err
}

// InsertSeedPrices bulk-inserts historical seed prices with back-calculated timestamps.
func (s *SQLitePriceStore) InsertSeedPrices(ctx context.Context, productID string, prices []int) error {
	if len(prices) == 0 {
		return nil
	}

	tx, err := s.sdb.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := insertSeedPricesTx(ctx, tx, productID, prices); err != nil {
		return err
	}
	return tx.Commit()
}

// ReplaceSeedPrices deletes existing seed prices and inserts new ones in a single transaction.
// Non-seed (is_seed=FALSE) price history is preserved.
func (s *SQLitePriceStore) ReplaceSeedPrices(ctx context.Context, productID string, prices []int) error {
	if len(prices) == 0 {
		return nil
	}

	tx, err := s.sdb.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM coupang_price_history WHERE product_id = ? AND is_seed = TRUE`,
		productID,
	); err != nil {
		return err
	}

	if err := insertSeedPricesTx(ctx, tx, productID, prices); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLitePriceStore) HasSeedPrices(ctx context.Context, productID string) (bool, error) {
	var count int
	err := s.sdb.Read.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM coupang_price_history WHERE product_id = ? AND is_seed = TRUE`,
		productID,
	).Scan(&count)
	return count > 0, err
}

// GetPriceHistory returns price points for a product since the given time.
func (s *SQLitePriceStore) GetPriceHistory(ctx context.Context, productID string, since time.Time) ([]PricePoint, error) {
	query := `SELECT price, is_seed, fetched_at FROM coupang_price_history
	          WHERE product_id = ? AND fetched_at >= ?
	          ORDER BY fetched_at ASC`

	rows, err := s.sdb.Read.QueryContext(ctx, query, productID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []PricePoint
	for rows.Next() {
		var p PricePoint
		if err := rows.Scan(&p.Price, &p.IsSeed, &p.FetchedAt); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

// GetPriceStats calculates aggregate price statistics for a product.
func (s *SQLitePriceStore) GetPriceStats(ctx context.Context, productID string) (*PriceStats, error) {
	var stats PriceStats
	var avgPrice float64
	err := s.sdb.Read.QueryRowContext(ctx,
		`SELECT COALESCE(MIN(price),0), COALESCE(MAX(price),0), COALESCE(AVG(price),0), COUNT(*)
		 FROM coupang_price_history WHERE product_id = ?`,
		productID,
	).Scan(&stats.MinPrice, &stats.MaxPrice, &avgPrice, &stats.TotalPoints)
	if err != nil {
		return nil, err
	}
	if stats.TotalPoints == 0 {
		return nil, nil
	}
	stats.AvgPrice = int(avgPrice)

	_ = s.sdb.Read.QueryRowContext(ctx,
		`SELECT fetched_at FROM coupang_price_history WHERE product_id = ? AND price = ? ORDER BY fetched_at DESC LIMIT 1`,
		productID, stats.MinPrice,
	).Scan(&stats.MinDate)
	_ = s.sdb.Read.QueryRowContext(ctx,
		`SELECT fetched_at FROM coupang_price_history WHERE product_id = ? AND price = ? ORDER BY fetched_at DESC LIMIT 1`,
		productID, stats.MaxPrice,
	).Scan(&stats.MaxDate)

	return &stats, nil
}

func (s *SQLitePriceStore) UpdateSnapshot(ctx context.Context, snapshot CoupangSnapshot) error {
	if snapshot.LastSeenAt.IsZero() {
		snapshot.LastSeenAt = time.Now()
	}
	if snapshot.LastRefreshAttemptAt.IsZero() {
		snapshot.LastRefreshAttemptAt = snapshot.LastSeenAt
	}
	_, err := s.sdb.Write.ExecContext(ctx, `
		UPDATE coupang_products
		SET last_seen_price = ?,
		    last_seen_at = ?,
		    last_refresh_attempt_at = ?,
		    refresh_source = ?,
		    refresh_tier = ?,
		    refresh_in_flight = ?
		WHERE product_id = ?
	`, snapshot.Price, snapshot.LastSeenAt, snapshot.LastRefreshAttemptAt, snapshot.RefreshSource, normalizeTier(snapshot.Tier), snapshot.RefreshInFlight, snapshot.TrackID)
	return err
}

func (s *SQLitePriceStore) MarkRefreshState(ctx context.Context, productID string, attemptedAt time.Time, inFlight bool) error {
	if attemptedAt.IsZero() {
		attemptedAt = time.Now()
	}
	_, err := s.sdb.Write.ExecContext(ctx, `
		UPDATE coupang_products
		SET last_refresh_attempt_at = ?,
		    refresh_in_flight = ?
		WHERE product_id = ?
	`, attemptedAt, inFlight, productID)
	return err
}

func (s *SQLitePriceStore) SetProductTier(ctx context.Context, productID string, tier CoupangRefreshTier) error {
	_, err := s.sdb.Write.ExecContext(ctx,
		`UPDATE coupang_products SET refresh_tier = ? WHERE product_id = ?`,
		normalizeTier(tier),
		productID,
	)
	return err
}

func (s *SQLitePriceStore) MarkChartBackfillAt(ctx context.Context, productID string, at time.Time) error {
	_, err := s.sdb.Write.ExecContext(ctx,
		`UPDATE coupang_products SET fallcent_last_chart_backfill_at = ? WHERE product_id = ?`,
		at, productID,
	)
	return err
}

// ListWatchedProducts returns products queried within the given duration.
func (s *SQLitePriceStore) ListWatchedProducts(ctx context.Context, since time.Duration) ([]CoupangProductRecord, error) {
	cutoff := time.Now().Add(-since)
	query := `
		SELECT product_id, base_product_id, vendor_item_id, item_id, name, image_url, created_at, last_queried, query_count,
		       recent_query_count, query_window_started_at, last_seen_price, last_seen_at,
		       last_refresh_attempt_at, refresh_source, refresh_tier, refresh_in_flight,
		       fallcent_product_id, fallcent_search_keyword, fallcent_mapping_state, fallcent_verified_at,
		       fallcent_failure_count, fallcent_failure_reason, fallcent_lowest_price,
		       fallcent_last_chart_backfill_at
		FROM coupang_products
		WHERE last_queried >= ?
		ORDER BY last_queried DESC
	`

	rows, err := s.sdb.Read.QueryContext(ctx, query, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []CoupangProductRecord
	for rows.Next() {
		record, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		products = append(products, record)
	}
	return products, rows.Err()
}

func (s *SQLitePriceStore) CountTrackedProducts(ctx context.Context) (int, error) {
	var count int
	err := s.sdb.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM coupang_products`).Scan(&count)
	return count, err
}

// EvictStaleProducts removes products not queried within olderThan and their price history.
func (s *SQLitePriceStore) EvictStaleProducts(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan)

	tx, err := s.sdb.Write.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`DELETE FROM coupang_price_history WHERE product_id IN
		 (SELECT product_id FROM coupang_products WHERE last_queried < ?)`,
		cutoff,
	)
	if err != nil {
		return 0, err
	}

	result, err := tx.ExecContext(ctx,
		`DELETE FROM coupang_products WHERE last_queried < ?`,
		cutoff,
	)
	if err != nil {
		return 0, err
	}

	count, _ := result.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return int(count), nil
}

func (s *SQLitePriceStore) DeleteProducts(ctx context.Context, productIDs []string) (int, error) {
	if len(productIDs) == 0 {
		return 0, nil
	}

	placeholders := strings.TrimRight(strings.Repeat("?,", len(productIDs)), ",")
	args := make([]interface{}, 0, len(productIDs))
	for _, productID := range productIDs {
		args = append(args, productID)
	}

	tx, err := s.sdb.Write.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM coupang_price_history WHERE product_id IN (%s)`, placeholders),
		args...,
	); err != nil {
		return 0, err
	}

	result, err := tx.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM coupang_products WHERE product_id IN (%s)`, placeholders),
		args...,
	)
	if err != nil {
		return 0, err
	}

	count, _ := result.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return int(count), nil
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanProduct(scanner rowScanner) (CoupangProductRecord, error) {
	var record CoupangProductRecord
	var baseProductID sql.NullString
	var vendorItemID sql.NullString
	var itemID sql.NullString
	var name sql.NullString
	var imageURL sql.NullString
	var queryWindowStartedAt sql.NullTime
	var lastSeenAt sql.NullTime
	var lastRefreshAttemptAt sql.NullTime
	var refreshSource sql.NullString
	var refreshTier sql.NullString
	var fallcentProductID sql.NullString
	var fallcentSearchKeyword sql.NullString
	var fallcentMappingState sql.NullString
	var fallcentVerifiedAt sql.NullTime
	var fallcentFailureCount sql.NullInt64
	var fallcentFailureReason sql.NullString
	var fallcentLowestPrice sql.NullInt64
	var fallcentLastChartBackfillAt sql.NullTime
	err := scanner.Scan(
		&record.TrackID,
		&baseProductID,
		&vendorItemID,
		&itemID,
		&name,
		&imageURL,
		&record.CreatedAt,
		&record.LastQueried,
		&record.QueryCount,
		&record.RecentQueryCount,
		&queryWindowStartedAt,
		&record.Snapshot.Price,
		&lastSeenAt,
		&lastRefreshAttemptAt,
		&refreshSource,
		&refreshTier,
		&record.Snapshot.RefreshInFlight,
		&fallcentProductID,
		&fallcentSearchKeyword,
		&fallcentMappingState,
		&fallcentVerifiedAt,
		&fallcentFailureCount,
		&fallcentFailureReason,
		&fallcentLowestPrice,
		&fallcentLastChartBackfillAt,
	)
	if err != nil {
		return CoupangProductRecord{}, err
	}

	record.ProductID = baseProductID.String
	if record.ProductID == "" {
		record.ProductID = record.TrackID
	}
	record.VendorItemID = vendorItemID.String
	record.ItemID = itemID.String
	record.Name = name.String
	record.ImageURL = imageURL.String
	record.QueryWindowStartedAt = queryWindowStartedAt.Time
	record.Snapshot.TrackID = record.TrackID
	record.Snapshot.LastSeenAt = lastSeenAt.Time
	record.Snapshot.LastRefreshAttemptAt = lastRefreshAttemptAt.Time
	record.Snapshot.RefreshSource = refreshSource.String
	record.Snapshot.Tier = parseTier(refreshTier.String)
	record.SourceMapping = CoupangSourceMapping{
		TrackID:             record.TrackID,
		FallcentProductID:   fallcentProductID.String,
		SearchKeyword:       fallcentSearchKeyword.String,
		State:               parseSourceMappingState(fallcentMappingState.String),
		VerifiedAt:          fallcentVerifiedAt.Time,
		FailureCount:        int(fallcentFailureCount.Int64),
		LastFailureReason:   fallcentFailureReason.String,
		ComparativeMinPrice: int(fallcentLowestPrice.Int64),
		LastChartBackfillAt: fallcentLastChartBackfillAt.Time,
	}
	return record, nil
}

func trackIDForRecord(p CoupangProductRecord) string {
	if p.TrackID != "" {
		return p.TrackID
	}
	return p.ProductID
}

func insertSeedPricesTx(ctx context.Context, tx *sql.Tx, productID string, prices []int) error {
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO coupang_price_history (product_id, price, is_seed, fetched_at) VALUES (?, ?, TRUE, ?)`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now()
	for i, price := range prices {
		daysAgo := len(prices) - 1 - i
		ts := now.Add(-time.Duration(daysAgo) * 24 * time.Hour)
		if _, err := stmt.ExecContext(ctx, productID, price, ts); err != nil {
			return err
		}
	}
	return nil
}

func normalizeTier(tier CoupangRefreshTier) string {
	switch tier {
	case CoupangTierHot, CoupangTierWarm, CoupangTierCold:
		return string(tier)
	default:
		return string(CoupangTierWarm)
	}
}

func parseTier(value string) CoupangRefreshTier {
	switch CoupangRefreshTier(value) {
	case CoupangTierHot, CoupangTierWarm, CoupangTierCold:
		return CoupangRefreshTier(value)
	default:
		return CoupangTierWarm
	}
}

func normalizeSourceMappingState(state CoupangSourceMappingState) string {
	switch state {
	case CoupangSourceMappingVerified, CoupangSourceMappingNeedsRecheck, CoupangSourceMappingFailed:
		return string(state)
	default:
		return ""
	}
}

func parseSourceMappingState(value string) CoupangSourceMappingState {
	switch CoupangSourceMappingState(value) {
	case CoupangSourceMappingVerified, CoupangSourceMappingNeedsRecheck, CoupangSourceMappingFailed:
		return CoupangSourceMappingState(value)
	default:
		return CoupangSourceMappingUnknown
	}
}
