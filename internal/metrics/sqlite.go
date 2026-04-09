package metrics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shift-click/masterbot/internal/sqliteutil"
)

type SQLiteStore struct {
	sdb *sqliteutil.SQLiteDB
}

const (
	defaultTextEmptyColumn = "TEXT DEFAULT ''"
	filterAudienceClause   = " AND audience = ?"
	filterTenantClause     = " AND tenant_id_hash = ?"
	filterRoomScopeClause  = " AND room_scope_hash = ?"
	filterFeatureKeyClause = " AND feature_key = ?"
)

func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	sdb, err := sqliteutil.OpenSQLiteDB(dbPath)
	if err != nil {
		return nil, err
	}
	store := &SQLiteStore{sdb: sdb}
	if err := store.migrate(); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("migrate metrics schema: %w", err)
	}
	if err := sdb.OptimizeFull(); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("initial optimize: %w", err)
	}
	return store, nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.sdb == nil {
		return nil
	}
	_ = s.sdb.Optimize()
	return s.sdb.Close()
}

func (s *SQLiteStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS metrics_events (
		id                  INTEGER PRIMARY KEY AUTOINCREMENT,
		occurred_at         DATETIME NOT NULL,
		request_id          TEXT DEFAULT '',
		event_name          TEXT NOT NULL,
		room_id_hash        TEXT DEFAULT '',
		tenant_id_hash      TEXT DEFAULT '',
		room_scope_hash     TEXT DEFAULT '',
		room_label          TEXT DEFAULT '',
		room_name_snapshot  TEXT DEFAULT '',
		user_id_hash        TEXT DEFAULT '',
		command_id          TEXT DEFAULT '',
		command_source      TEXT DEFAULT '',
		audience            TEXT DEFAULT '',
		feature_key         TEXT DEFAULT '',
		attribution         TEXT DEFAULT '',
		success             INTEGER,
		error_class         TEXT DEFAULT '',
		latency_ms          INTEGER DEFAULT 0,
		denied              INTEGER DEFAULT 0,
		rate_limited        INTEGER DEFAULT 0,
		reply_type          TEXT DEFAULT '',
		metadata_json       TEXT DEFAULT '{}'
	);
	CREATE INDEX IF NOT EXISTS idx_metrics_events_time ON metrics_events(occurred_at);
	CREATE INDEX IF NOT EXISTS idx_metrics_events_room_time ON metrics_events(room_id_hash, occurred_at);
	CREATE INDEX IF NOT EXISTS idx_metrics_events_command_time ON metrics_events(command_id, occurred_at);
	CREATE INDEX IF NOT EXISTS idx_metrics_events_name_time ON metrics_events(event_name, occurred_at);

	CREATE TABLE IF NOT EXISTS metrics_hourly_rollups (
		bucket_at           DATETIME NOT NULL,
		room_id_hash        TEXT NOT NULL,
		room_label          TEXT DEFAULT '',
		room_name_snapshot  TEXT DEFAULT '',
		command_id          TEXT DEFAULT '',
		command_source      TEXT DEFAULT '',
		request_count       INTEGER DEFAULT 0,
		success_count       INTEGER DEFAULT 0,
		error_count         INTEGER DEFAULT 0,
		denied_count        INTEGER DEFAULT 0,
		rate_limited_count  INTEGER DEFAULT 0,
		reply_failed_count  INTEGER DEFAULT 0,
		unique_users        INTEGER DEFAULT 0,
		avg_latency_ms      REAL DEFAULT 0,
		PRIMARY KEY (bucket_at, room_id_hash, command_id, command_source)
	);

	CREATE TABLE IF NOT EXISTS metrics_daily_rollups (
		bucket_date         DATE NOT NULL,
		room_id_hash        TEXT NOT NULL,
		room_label          TEXT DEFAULT '',
		room_name_snapshot  TEXT DEFAULT '',
		command_id          TEXT DEFAULT '',
		command_source      TEXT DEFAULT '',
		request_count       INTEGER DEFAULT 0,
		success_count       INTEGER DEFAULT 0,
		error_count         INTEGER DEFAULT 0,
		denied_count        INTEGER DEFAULT 0,
		rate_limited_count  INTEGER DEFAULT 0,
		reply_failed_count  INTEGER DEFAULT 0,
		unique_users        INTEGER DEFAULT 0,
		avg_latency_ms      REAL DEFAULT 0,
		PRIMARY KEY (bucket_date, room_id_hash, command_id, command_source)
	);

	CREATE TABLE IF NOT EXISTS metrics_error_summaries (
		bucket_date         DATE NOT NULL,
		room_id_hash        TEXT NOT NULL,
		room_label          TEXT DEFAULT '',
		command_id          TEXT DEFAULT '',
		error_class         TEXT NOT NULL,
		error_count         INTEGER DEFAULT 0,
		PRIMARY KEY (bucket_date, room_id_hash, command_id, error_class)
	);

	CREATE TABLE IF NOT EXISTS metrics_product_funnel_rollups (
		bucket_date         DATE NOT NULL,
		audience            TEXT NOT NULL,
		tenant_id_hash      TEXT NOT NULL,
		room_scope_hash     TEXT NOT NULL,
		feature_key         TEXT NOT NULL,
		stage               TEXT NOT NULL,
		event_count         INTEGER DEFAULT 0,
		PRIMARY KEY (bucket_date, audience, tenant_id_hash, room_scope_hash, feature_key, stage)
	);

	CREATE TABLE IF NOT EXISTS metrics_product_retention_rollups (
		cohort_date         DATE NOT NULL,
		bucket_date         DATE NOT NULL,
		audience            TEXT NOT NULL,
		tenant_id_hash      TEXT NOT NULL,
		room_scope_hash     TEXT NOT NULL,
		feature_key         TEXT NOT NULL,
		cohort_size         INTEGER DEFAULT 0,
		retained_users      INTEGER DEFAULT 0,
		PRIMARY KEY (cohort_date, bucket_date, audience, tenant_id_hash, room_scope_hash, feature_key)
	);
	`
	if _, err := s.sdb.Write.Exec(schema); err != nil {
		return err
	}
	// Backward-compatible additive migrations for existing deployments.
	for _, column := range []struct {
		name       string
		definition string
	}{
		{name: "tenant_id_hash", definition: defaultTextEmptyColumn},
		{name: "room_scope_hash", definition: defaultTextEmptyColumn},
		{name: "audience", definition: defaultTextEmptyColumn},
		{name: "feature_key", definition: defaultTextEmptyColumn},
		{name: "attribution", definition: defaultTextEmptyColumn},
	} {
		if err := s.addColumnIfMissing("metrics_events", column.name, column.definition); err != nil {
			return err
		}
	}
	// Create indexes that depend on migrated columns (must run after addColumnIfMissing).
	postMigrationIndexes := `
	CREATE INDEX IF NOT EXISTS idx_metrics_events_tenant_time ON metrics_events(tenant_id_hash, occurred_at);
	CREATE INDEX IF NOT EXISTS idx_metrics_events_scope_time ON metrics_events(room_scope_hash, occurred_at);
	CREATE INDEX IF NOT EXISTS idx_metrics_events_audience_time ON metrics_events(audience, occurred_at);
	CREATE INDEX IF NOT EXISTS idx_metrics_events_feature_time ON metrics_events(feature_key, occurred_at);
	`
	if _, err := s.sdb.Write.Exec(postMigrationIndexes); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteStore) InsertEvents(ctx context.Context, events []StoredEvent) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.sdb.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO metrics_events (
			occurred_at, request_id, event_name, room_id_hash, tenant_id_hash, room_scope_hash,
			room_label, room_name_snapshot, user_id_hash, command_id, command_source, audience,
			feature_key, attribution, success, error_class, latency_ms,
			denied, rate_limited, reply_type, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, event := range events {
		var success any
		if event.Success != nil {
			if *event.Success {
				success = 1
			} else {
				success = 0
			}
		}
		if _, err := stmt.ExecContext(
			ctx,
			timestampString(event.OccurredAt),
			event.RequestID,
			event.EventName,
			event.RoomIDHash,
			event.TenantIDHash,
			event.RoomScopeHash,
			event.RoomLabel,
			event.RoomNameSnapshot,
			event.UserIDHash,
			event.CommandID,
			event.CommandSource,
			event.Audience,
			event.FeatureKey,
			event.Attribution,
			success,
			event.ErrorClass,
			event.LatencyMS,
			boolInt(event.Denied),
			boolInt(event.RateLimited),
			event.ReplyType,
			event.MetadataJSON,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// InsertEventsIfAbsent inserts events when the same request_id + event_name pair
// does not already exist. It is intended for idempotent historical backfills.
func (s *SQLiteStore) InsertEventsIfAbsent(ctx context.Context, events []StoredEvent) (int64, error) {
	if len(events) == 0 {
		return 0, nil
	}
	tx, err := s.sdb.Write.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO metrics_events (
			occurred_at, request_id, event_name, room_id_hash, tenant_id_hash, room_scope_hash,
			room_label, room_name_snapshot, user_id_hash, command_id, command_source, audience,
			feature_key, attribution, success, error_class, latency_ms,
			denied, rate_limited, reply_type, metadata_json
		)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		WHERE NOT EXISTS (
			SELECT 1
			FROM metrics_events
			WHERE request_id = ? AND event_name = ?
		)
	`)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	defer stmt.Close()

	var inserted int64
	for _, event := range events {
		var success any
		if event.Success != nil {
			if *event.Success {
				success = 1
			} else {
				success = 0
			}
		}
		res, err := stmt.ExecContext(
			ctx,
			timestampString(event.OccurredAt),
			event.RequestID,
			event.EventName,
			event.RoomIDHash,
			event.TenantIDHash,
			event.RoomScopeHash,
			event.RoomLabel,
			event.RoomNameSnapshot,
			event.UserIDHash,
			event.CommandID,
			event.CommandSource,
			event.Audience,
			event.FeatureKey,
			event.Attribution,
			success,
			event.ErrorClass,
			event.LatencyMS,
			boolInt(event.Denied),
			boolInt(event.RateLimited),
			event.ReplyType,
			event.MetadataJSON,
			event.RequestID,
			event.EventName,
		)
		if err != nil {
			_ = tx.Rollback()
			return 0, err
		}
		affected, err := res.RowsAffected()
		if err == nil {
			inserted += affected
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}

func (s *SQLiteStore) Cleanup(ctx context.Context, retention RetentionPolicy, now time.Time) error {
	tx, err := s.sdb.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	cutoffs := []struct {
		query string
		when  string
	}{
		{query: `DELETE FROM metrics_events WHERE occurred_at < ?`, when: timestampString(now.Add(-retention.Raw))},
		{query: `DELETE FROM metrics_hourly_rollups WHERE bucket_at < ?`, when: hourBucketString(now.Add(-retention.Hourly))},
		{query: `DELETE FROM metrics_daily_rollups WHERE bucket_date < ?`, when: dateString(now.Add(-retention.Daily))},
		{query: `DELETE FROM metrics_error_summaries WHERE bucket_date < ?`, when: dateString(now.Add(-retention.Error))},
		{query: `DELETE FROM metrics_product_funnel_rollups WHERE bucket_date < ?`, when: dateString(now.Add(-retention.Daily))},
		{query: `DELETE FROM metrics_product_retention_rollups WHERE bucket_date < ?`, when: dateString(now.Add(-retention.Daily))},
	}
	for _, cutoff := range cutoffs {
		if _, err := tx.ExecContext(ctx, cutoff.query, cutoff.when); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) RebuildRollups(ctx context.Context) error {
	tx, err := s.sdb.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, query := range []string{
		`DELETE FROM metrics_hourly_rollups`,
		`DELETE FROM metrics_daily_rollups`,
		`DELETE FROM metrics_error_summaries`,
		`DELETE FROM metrics_product_funnel_rollups`,
		`DELETE FROM metrics_product_retention_rollups`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO metrics_hourly_rollups (
			bucket_at, room_id_hash, room_label, room_name_snapshot, command_id, command_source,
			request_count, success_count, error_count, denied_count, rate_limited_count,
			reply_failed_count, unique_users, avg_latency_ms
		)
		SELECT
			substr(occurred_at, 1, 13) || ':00:00Z',
			COALESCE(room_id_hash, ''),
			MAX(COALESCE(room_label, '')),
			MAX(COALESCE(room_name_snapshot, '')),
			COALESCE(command_id, ''),
			COALESCE(command_source, ''),
			SUM(CASE WHEN event_name = 'message_received' THEN 1 ELSE 0 END),
			SUM(CASE WHEN event_name = 'command_succeeded' THEN 1 ELSE 0 END),
			SUM(CASE WHEN event_name = 'command_failed' THEN 1 ELSE 0 END),
			SUM(CASE WHEN event_name = 'access_denied' THEN 1 ELSE 0 END),
			SUM(CASE WHEN event_name = 'rate_limited' THEN 1 ELSE 0 END),
			SUM(CASE WHEN event_name = 'reply_failed' THEN 1 ELSE 0 END),
			COUNT(DISTINCT CASE WHEN user_id_hash <> '' THEN user_id_hash END),
			AVG(CASE WHEN latency_ms > 0 THEN latency_ms END)
		FROM metrics_events
		GROUP BY 1, 2, 5, 6
	`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO metrics_daily_rollups (
			bucket_date, room_id_hash, room_label, room_name_snapshot, command_id, command_source,
			request_count, success_count, error_count, denied_count, rate_limited_count,
			reply_failed_count, unique_users, avg_latency_ms
		)
		SELECT
			substr(occurred_at, 1, 10),
			COALESCE(room_id_hash, ''),
			MAX(COALESCE(room_label, '')),
			MAX(COALESCE(room_name_snapshot, '')),
			COALESCE(command_id, ''),
			COALESCE(command_source, ''),
			SUM(CASE WHEN event_name = 'message_received' THEN 1 ELSE 0 END),
			SUM(CASE WHEN event_name = 'command_succeeded' THEN 1 ELSE 0 END),
			SUM(CASE WHEN event_name = 'command_failed' THEN 1 ELSE 0 END),
			SUM(CASE WHEN event_name = 'access_denied' THEN 1 ELSE 0 END),
			SUM(CASE WHEN event_name = 'rate_limited' THEN 1 ELSE 0 END),
			SUM(CASE WHEN event_name = 'reply_failed' THEN 1 ELSE 0 END),
			COUNT(DISTINCT CASE WHEN user_id_hash <> '' THEN user_id_hash END),
			AVG(CASE WHEN latency_ms > 0 THEN latency_ms END)
		FROM metrics_events
		GROUP BY 1, 2, 5, 6
	`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO metrics_error_summaries (
			bucket_date, room_id_hash, room_label, command_id, error_class, error_count
		)
		SELECT
			substr(occurred_at, 1, 10),
			COALESCE(room_id_hash, ''),
			MAX(COALESCE(room_label, '')),
			COALESCE(command_id, ''),
			error_class,
			COUNT(*)
		FROM metrics_events
		WHERE error_class <> ''
		GROUP BY 1, 2, 4, 5
	`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO metrics_product_funnel_rollups (
			bucket_date, audience, tenant_id_hash, room_scope_hash, feature_key, stage, event_count
		)
		SELECT
			substr(occurred_at, 1, 10),
			COALESCE(audience, ''),
			COALESCE(tenant_id_hash, ''),
			COALESCE(room_scope_hash, ''),
			COALESCE(feature_key, ''),
			event_name,
			COUNT(*)
		FROM metrics_events
		WHERE event_name IN ('acquisition', 'activation', 'engagement', 'conversion', 'retention_return', 'churn_signal')
		GROUP BY 1, 2, 3, 4, 5, 6
	`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO metrics_product_retention_rollups (
			cohort_date, bucket_date, audience, tenant_id_hash, room_scope_hash, feature_key, cohort_size, retained_users
		)
		WITH first_activation AS (
			SELECT
				user_id_hash,
				MIN(substr(occurred_at, 1, 10)) AS cohort_date,
				COALESCE(audience, '') AS audience,
				COALESCE(tenant_id_hash, '') AS tenant_id_hash,
				COALESCE(room_scope_hash, '') AS room_scope_hash,
				COALESCE(feature_key, '') AS feature_key
			FROM metrics_events
			WHERE event_name = 'activation' AND user_id_hash <> ''
			GROUP BY user_id_hash, audience, tenant_id_hash, room_scope_hash, feature_key
		),
		cohort_sizes AS (
			SELECT
				cohort_date,
				audience,
				tenant_id_hash,
				room_scope_hash,
				feature_key,
				COUNT(DISTINCT user_id_hash) AS cohort_size
			FROM first_activation
			GROUP BY cohort_date, audience, tenant_id_hash, room_scope_hash, feature_key
		),
		retention_counts AS (
			SELECT
				fa.cohort_date,
				substr(e.occurred_at, 1, 10) AS bucket_date,
				fa.audience,
				fa.tenant_id_hash,
				fa.room_scope_hash,
				fa.feature_key,
				COUNT(DISTINCT fa.user_id_hash) AS retained_users
			FROM first_activation fa
			JOIN metrics_events e
			  ON e.user_id_hash = fa.user_id_hash
			 AND e.event_name = 'retention_return'
			 AND substr(e.occurred_at, 1, 10) >= fa.cohort_date
			GROUP BY fa.cohort_date, bucket_date, fa.audience, fa.tenant_id_hash, fa.room_scope_hash, fa.feature_key
		)
		SELECT
			rc.cohort_date,
			rc.bucket_date,
			rc.audience,
			rc.tenant_id_hash,
			rc.room_scope_hash,
			rc.feature_key,
			cs.cohort_size,
			rc.retained_users
		FROM retention_counts rc
		JOIN cohort_sizes cs
		  ON cs.cohort_date = rc.cohort_date
		 AND cs.audience = rc.audience
		 AND cs.tenant_id_hash = rc.tenant_id_hash
		 AND cs.room_scope_hash = rc.room_scope_hash
		 AND cs.feature_key = rc.feature_key
	`); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func ratio(numerator, denominator int64) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func timestampString(ts time.Time) string {
	return ts.UTC().Format(time.RFC3339)
}

func dateString(ts time.Time) string {
	return ts.UTC().Format("2006-01-02")
}

func hourBucketString(ts time.Time) string {
	return ts.UTC().Format("2006-01-02T15:00:00Z")
}

func (s *SQLiteStore) addColumnIfMissing(tableName, columnName, definition string) error {
	rows, err := s.sdb.Write.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, tableName))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid       int
			name      string
			typ       string
			notNull   int
			defaultV  any
			primaryID int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultV, &primaryID); err != nil {
			return err
		}
		if strings.EqualFold(name, columnName) {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.sdb.Write.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, tableName, columnName, definition))
	return err
}
