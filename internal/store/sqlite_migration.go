package store

import (
	"database/sql"
	"fmt"
	"time"
)

type migrationColumn struct {
	name string
	def  string
}

const (
	sqliteIntegerDefaultZero = "INTEGER DEFAULT 0"
	sqliteTextDefaultEmpty   = "TEXT DEFAULT ''"
)

var coupangProductMigrationColumns = []migrationColumn{
	{name: "base_product_id", def: "TEXT"},
	{name: "recent_query_count", def: sqliteIntegerDefaultZero},
	{name: "query_window_started_at", def: "DATETIME"},
	{name: "last_seen_price", def: sqliteIntegerDefaultZero},
	{name: "last_seen_at", def: "DATETIME"},
	{name: "last_refresh_attempt_at", def: "DATETIME"},
	{name: "refresh_source", def: sqliteTextDefaultEmpty},
	{name: "refresh_tier", def: "TEXT DEFAULT 'warm'"},
	{name: "refresh_in_flight", def: "BOOLEAN DEFAULT FALSE"},
	{name: "fallcent_product_id", def: sqliteTextDefaultEmpty},
	{name: "fallcent_search_keyword", def: sqliteTextDefaultEmpty},
	{name: "fallcent_mapping_state", def: sqliteTextDefaultEmpty},
	{name: "fallcent_verified_at", def: "DATETIME"},
	{name: "fallcent_failure_count", def: sqliteIntegerDefaultZero},
	{name: "fallcent_failure_reason", def: sqliteTextDefaultEmpty},
	{name: "fallcent_lowest_price", def: sqliteIntegerDefaultZero},
	{name: "fallcent_last_chart_backfill_at", def: "DATETIME"},
}

func (s *SQLitePriceStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS coupang_products (
		product_id               TEXT PRIMARY KEY,
		base_product_id          TEXT,
		vendor_item_id           TEXT,
		item_id                  TEXT,
		name                     TEXT,
		image_url                TEXT,
		created_at               DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_queried             DATETIME DEFAULT CURRENT_TIMESTAMP,
		query_count              INTEGER DEFAULT 0,
		recent_query_count       INTEGER DEFAULT 0,
		query_window_started_at  DATETIME,
		last_seen_price          INTEGER DEFAULT 0,
		last_seen_at             DATETIME,
		last_refresh_attempt_at  DATETIME,
		refresh_source           TEXT DEFAULT '',
		refresh_tier             TEXT DEFAULT 'warm',
		refresh_in_flight        BOOLEAN DEFAULT FALSE,
		fallcent_product_id      TEXT DEFAULT '',
		fallcent_search_keyword  TEXT DEFAULT '',
		fallcent_mapping_state   TEXT DEFAULT '',
		fallcent_verified_at     DATETIME,
		fallcent_failure_count   INTEGER DEFAULT 0,
		fallcent_failure_reason  TEXT DEFAULT '',
		fallcent_lowest_price    INTEGER DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS coupang_price_history (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		product_id TEXT NOT NULL,
		price      INTEGER NOT NULL,
		is_seed    BOOLEAN DEFAULT FALSE,
		fetched_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (product_id) REFERENCES coupang_products(product_id)
	);

	CREATE INDEX IF NOT EXISTS idx_price_product_time ON coupang_price_history(product_id, fetched_at);
	CREATE INDEX IF NOT EXISTS idx_products_queried ON coupang_products(last_queried);
	`
	if _, err := s.sdb.Write.Exec(schema); err != nil {
		return err
	}

	for _, column := range coupangProductMigrationColumns {
		if err := s.ensureColumn("coupang_products", column.name, column.def); err != nil {
			return err
		}
	}
	if _, err := s.sdb.Write.Exec(`CREATE INDEX IF NOT EXISTS idx_products_base_product_id ON coupang_products(base_product_id)`); err != nil {
		return err
	}
	if _, err := s.sdb.Write.Exec(`CREATE INDEX IF NOT EXISTS idx_products_refresh_tier ON coupang_products(refresh_tier, last_refresh_attempt_at)`); err != nil {
		return err
	}
	if _, err := s.sdb.Write.Exec(`
		UPDATE coupang_products
		SET base_product_id = COALESCE(NULLIF(base_product_id, ''), product_id)
	`); err != nil {
		return err
	}

	return s.backfillSnapshots()
}

func (s *SQLitePriceStore) ensureColumn(table, name, definition string) error {
	rows, err := s.sdb.Write.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var columnName, columnType string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if columnName == name {
			return nil
		}
	}

	_, err = s.sdb.Write.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, name, definition))
	return err
}

func (s *SQLitePriceStore) backfillSnapshots() error {
	if _, err := s.sdb.Write.Exec(`
		UPDATE coupang_products
		SET query_window_started_at = COALESCE(query_window_started_at, last_queried, created_at, CURRENT_TIMESTAMP),
		    recent_query_count = CASE
		        WHEN recent_query_count > 0 THEN recent_query_count
		        WHEN query_count > 0 THEN 1
		        ELSE 0
		    END,
		    refresh_tier = CASE
		        WHEN refresh_tier IN ('hot', 'warm', 'cold') THEN refresh_tier
		        ELSE 'warm'
		    END,
		    refresh_source = COALESCE(refresh_source, ''),
		    fallcent_product_id = COALESCE(fallcent_product_id, ''),
		    fallcent_search_keyword = COALESCE(fallcent_search_keyword, ''),
		    fallcent_mapping_state = COALESCE(fallcent_mapping_state, ''),
		    fallcent_failure_reason = COALESCE(fallcent_failure_reason, ''),
		    fallcent_failure_count = COALESCE(fallcent_failure_count, 0),
		    fallcent_lowest_price = COALESCE(fallcent_lowest_price, 0)
	`); err != nil {
		return err
	}

	rows, err := s.sdb.Write.Query(`
		SELECT product_id
		FROM coupang_products
		WHERE COALESCE(last_seen_price, 0) = 0 OR last_seen_at IS NULL
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var productIDs []string
	for rows.Next() {
		var productID string
		if err := rows.Scan(&productID); err != nil {
			return err
		}
		productIDs = append(productIDs, productID)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, productID := range productIDs {
		var price int
		var fetchedAt time.Time
		err := s.sdb.Write.QueryRow(`
			SELECT price, fetched_at
			FROM coupang_price_history
			WHERE product_id = ?
			ORDER BY fetched_at DESC, id DESC
			LIMIT 1
		`, productID).Scan(&price, &fetchedAt)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		if _, err := s.sdb.Write.Exec(`
			UPDATE coupang_products
			SET last_seen_price = ?,
			    last_seen_at = COALESCE(last_seen_at, ?),
			    last_refresh_attempt_at = COALESCE(last_refresh_attempt_at, ?),
			    refresh_source = CASE WHEN refresh_source = '' THEN 'history_backfill' ELSE refresh_source END,
			    refresh_tier = CASE
			        WHEN refresh_tier IN ('hot', 'warm', 'cold') THEN refresh_tier
			        ELSE 'warm'
			    END
			WHERE product_id = ?
		`, price, fetchedAt, fetchedAt, productID); err != nil {
			return err
		}
	}

	return nil
}
