package metrics

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestAsyncRecorderPseudonymizesAndStoresAlias(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "metrics.db"))
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	recorder := NewAsyncRecorder(
		store,
		"super-secret",
		map[string]string{"room-1": "운영방"},
		10*time.Millisecond,
		50*time.Millisecond,
		RetentionPolicy{
			Raw:    90 * 24 * time.Hour,
			Hourly: 90 * 24 * time.Hour,
			Daily:  395 * 24 * time.Hour,
			Error:  180 * 24 * time.Hour,
		},
		nil,
	)
	recorder.Start(ctx)

	success := true
	recorder.Record(ctx, Event{
		OccurredAt:    time.Now(),
		RequestID:     "r1",
		EventName:     EventCommandSucceeded,
		RawRoomID:     "room-1",
		RoomName:      "실제방이름",
		RawUserID:     "user-1",
		CommandID:     "코인",
		CommandSource: CommandSourceSlash,
		Success:       &success,
		Latency:       120 * time.Millisecond,
		Metadata:      map[string]any{"raw_message": "must-not-leak"},
	})
	cancel()
	recorder.Wait()

	row := store.sdb.Read.QueryRow(`
		SELECT room_id_hash, room_label, room_name_snapshot, user_id_hash, metadata_json
		FROM metrics_events
		LIMIT 1
	`)
	var roomIDHash, roomLabel, roomName, userIDHash, metadataJSON string
	if err := row.Scan(&roomIDHash, &roomLabel, &roomName, &userIDHash, &metadataJSON); err != nil {
		t.Fatalf("scan metrics row: %v", err)
	}
	if roomIDHash == "" || userIDHash == "" {
		t.Fatal("expected pseudonymized identifiers to be stored")
	}
	if roomLabel != "운영방" {
		t.Fatalf("room label = %q, want 운영방", roomLabel)
	}
	if roomName != "실제방이름" {
		t.Fatalf("room name snapshot = %q, want 실제방이름", roomName)
	}
	if roomIDHash == "room-1" || userIDHash == "user-1" {
		t.Fatal("expected raw identifiers not to be stored")
	}
	if metadataJSON == "" {
		t.Fatal("expected metadata json to be stored")
	}
}

func TestSQLiteStoreQueriesRoomsAndReliability(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "query.db"))
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().UTC()
	success := true
	failure := false
	if err := store.InsertEvents(context.Background(), []StoredEvent{
		{
			OccurredAt:       now.Add(-2 * time.Hour),
			RequestID:        "1",
			EventName:        string(EventMessageReceived),
			RoomIDHash:       "room-hash",
			RoomLabel:        "운영방",
			RoomNameSnapshot: "실제방",
			UserIDHash:       "user-a",
		},
		{
			OccurredAt:       now.Add(-2 * time.Hour),
			RequestID:        "1",
			EventName:        string(EventCommandSucceeded),
			RoomIDHash:       "room-hash",
			RoomLabel:        "운영방",
			RoomNameSnapshot: "실제방",
			UserIDHash:       "user-a",
			CommandID:        "코인",
			CommandSource:    string(CommandSourceSlash),
			Success:          &success,
			LatencyMS:        120,
		},
		{
			OccurredAt:       now.Add(-time.Hour),
			RequestID:        "2",
			EventName:        string(EventMessageReceived),
			RoomIDHash:       "room-hash",
			RoomLabel:        "운영방",
			RoomNameSnapshot: "실제방",
			UserIDHash:       "user-b",
		},
		{
			OccurredAt:       now.Add(-time.Hour),
			RequestID:        "2",
			EventName:        string(EventCommandFailed),
			RoomIDHash:       "room-hash",
			RoomLabel:        "운영방",
			RoomNameSnapshot: "실제방",
			UserIDHash:       "user-b",
			CommandID:        "주식",
			CommandSource:    string(CommandSourceExplicit),
			Success:          &failure,
			ErrorClass:       "handler_error",
			LatencyMS:        240,
		},
	}); err != nil {
		t.Fatalf("insert events: %v", err)
	}
	if err := store.RebuildRollups(context.Background()); err != nil {
		t.Fatalf("rebuild rollups: %v", err)
	}

	rooms, err := store.QueryRooms(context.Background(), now.Add(-7*24*time.Hour), now, "", "", 10)
	if err != nil {
		t.Fatalf("query rooms: %v", err)
	}
	if len(rooms) != 1 {
		t.Fatalf("rooms len = %d, want 1", len(rooms))
	}
	if rooms[0].Requests != 2 {
		t.Fatalf("requests = %d, want 2", rooms[0].Requests)
	}
	if rooms[0].ActiveUsers != 2 {
		t.Fatalf("active users = %d, want 2", rooms[0].ActiveUsers)
	}
	if rooms[0].ErrorCount != 1 {
		t.Fatalf("error count = %d, want 1", rooms[0].ErrorCount)
	}

	reliability, err := store.QueryReliability(context.Background(), now.Add(-7*24*time.Hour), now, "", "")
	if err != nil {
		t.Fatalf("query reliability: %v", err)
	}
	if reliability.TotalCommands != 2 {
		t.Fatalf("total commands = %d, want 2", reliability.TotalCommands)
	}
	if reliability.FailedCommands != 1 {
		t.Fatalf("failed commands = %d, want 1", reliability.FailedCommands)
	}
	if len(reliability.ErrorsByClass) != 1 || reliability.ErrorsByClass[0].ErrorClass != "handler_error" {
		t.Fatalf("errors by class = %+v", reliability.ErrorsByClass)
	}
}

func TestSQLiteStoreQueriesProductAnalytics(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "product.db"))
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().UTC()
	if err := store.InsertEvents(context.Background(), []StoredEvent{
		{
			OccurredAt:    now.Add(-48 * time.Hour),
			EventName:     string(EventActivation),
			UserIDHash:    "user-a",
			Audience:      "customer",
			TenantIDHash:  "tenant-a",
			RoomScopeHash: "room-a",
			FeatureKey:    "코인",
		},
		{
			OccurredAt:    now.Add(-24 * time.Hour),
			EventName:     string(EventEngagement),
			UserIDHash:    "user-a",
			Audience:      "customer",
			TenantIDHash:  "tenant-a",
			RoomScopeHash: "room-a",
			FeatureKey:    "코인",
		},
		{
			OccurredAt:    now.Add(-12 * time.Hour),
			EventName:     string(EventConversion),
			UserIDHash:    "user-a",
			Audience:      "customer",
			TenantIDHash:  "tenant-a",
			RoomScopeHash: "room-a",
			FeatureKey:    "코인",
		},
		{
			OccurredAt:    now.Add(-6 * time.Hour),
			EventName:     string(EventRetentionReturn),
			UserIDHash:    "user-a",
			Audience:      "customer",
			TenantIDHash:  "tenant-a",
			RoomScopeHash: "room-a",
			FeatureKey:    "코인",
		},
	}); err != nil {
		t.Fatalf("insert product events: %v", err)
	}
	if err := store.RebuildRollups(context.Background()); err != nil {
		t.Fatalf("rebuild rollups: %v", err)
	}

	funnel, err := store.QueryProductFunnel(context.Background(), now.Add(-7*24*time.Hour), now, "customer", "tenant-a", "room-a", "코인")
	if err != nil {
		t.Fatalf("query funnel: %v", err)
	}
	if len(funnel) == 0 {
		t.Fatal("expected funnel data")
	}

	cohorts, err := store.QueryProductCohorts(context.Background(), now.Add(-7*24*time.Hour), now, "customer", "tenant-a", "room-a", "코인")
	if err != nil {
		t.Fatalf("query cohorts: %v", err)
	}
	if len(cohorts) == 0 {
		t.Fatal("expected cohort data")
	}

	retention, err := store.QueryProductRetention(context.Background(), now.Add(-7*24*time.Hour), now, "customer", "tenant-a", "room-a", "코인")
	if err != nil {
		t.Fatalf("query retention: %v", err)
	}
	if len(retention) == 0 {
		t.Fatal("expected retention data")
	}
}

func TestSQLiteStoreInsertEventsIfAbsentDeduplicatesRequestIDAndEventName(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "dedupe.db"))
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().UTC()
	inserted, err := store.InsertEventsIfAbsent(context.Background(), []StoredEvent{
		{
			OccurredAt: now,
			RequestID:  "req-1",
			EventName:  string(EventMessageReceived),
			RoomIDHash: "room-1",
		},
		{
			OccurredAt: now.Add(time.Second),
			RequestID:  "req-1",
			EventName:  string(EventMessageReceived),
			RoomIDHash: "room-1",
		},
		{
			OccurredAt: now.Add(2 * time.Second),
			RequestID:  "req-1",
			EventName:  string(EventCommandSucceeded),
			RoomIDHash: "room-1",
		},
	})
	if err != nil {
		t.Fatalf("insert if absent: %v", err)
	}
	if inserted != 2 {
		t.Fatalf("inserted = %d, want 2", inserted)
	}

	var count int
	if err := store.sdb.Read.QueryRow(`SELECT COUNT(*) FROM metrics_events`).Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 2 {
		t.Fatalf("event count = %d, want 2", count)
	}
}

func TestSQLiteStoreQueryCoupangRefreshStatsIncludesCompositeAndJoin(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "coupang-stats.db"))
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().UTC()
	if err := store.InsertEvents(context.Background(), []StoredEvent{
		{
			OccurredAt: now.Add(-5 * time.Minute),
			EventName:  string(EventCoupangRefreshSucceeded),
			CommandID:  "쿠팡",
			RoomIDHash: "room-1",
		},
		{
			OccurredAt:   now.Add(-4 * time.Minute),
			EventName:    string(EventCoupangLookupCoalesced),
			CommandID:    "쿠팡",
			RoomIDHash:   "room-1",
			MetadataJSON: `{"coalesced_hit":true,"join_timeout":true}`,
		},
		{
			OccurredAt:   now.Add(-3 * time.Minute),
			EventName:    string(EventCoupangLookupCoalesced),
			CommandID:    "쿠팡",
			RoomIDHash:   "room-1",
			MetadataJSON: `{"coalesced_hit":true,"join_timeout":false}`,
		},
		{
			OccurredAt:   now.Add(-2 * time.Minute),
			EventName:    string(EventReplyCompositeOutcome),
			FeatureKey:   "coupang",
			RoomIDHash:   "room-1",
			MetadataJSON: `{"composite_delivery_outcome":"partial_success","chart_final_mode":"text_only_skipped","chart_decision_reason":"insufficient_points"}`,
		},
		{
			OccurredAt:   now.Add(-time.Minute),
			EventName:    string(EventReplyCompositeOutcome),
			FeatureKey:   "coupang",
			RoomIDHash:   "room-1",
			MetadataJSON: `{"composite_delivery_outcome":"full_success","chart_final_mode":"with_chart","chart_decision_reason":"eligible"}`,
		},
		{
			OccurredAt:   now.Add(-90 * time.Second),
			EventName:    string(EventCoupangRegistrationPath),
			CommandID:    "쿠팡",
			RoomIDHash:   "room-1",
			MetadataJSON: `{"registration_stage":"direct_resolved","response_mode":"text_first","seed_deferred":true}`,
		},
		{
			OccurredAt:   now.Add(-30 * time.Second),
			EventName:    string(EventCoupangRegistrationPath),
			CommandID:    "쿠팡",
			RoomIDHash:   "room-1",
			MetadataJSON: `{"registration_stage":"deferred","response_mode":"registration_deferred","budget_exhausted":true,"rescue_deferred":true,"registration_deferred":true}`,
		},
	}); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	stats, err := store.QueryCoupangRefreshStats(context.Background(), now.Add(-time.Hour), now, "room-1")
	if err != nil {
		t.Fatalf("query coupang stats: %v", err)
	}
	if stats.JoinTotalCount != 2 || stats.JoinTimeoutCount != 1 {
		t.Fatalf("unexpected join stats: %+v", stats)
	}
	if stats.JoinTimeoutRatio != 0.5 {
		t.Fatalf("join timeout ratio = %f, want 0.5", stats.JoinTimeoutRatio)
	}
	if stats.CompositeTotalCount != 2 || stats.PartialCount != 1 {
		t.Fatalf("unexpected composite stats: %+v", stats)
	}
	if stats.PartialRatio != 0.5 {
		t.Fatalf("partial ratio = %f, want 0.5", stats.PartialRatio)
	}
	if stats.ChartSkippedCount != 1 {
		t.Fatalf("chart skipped count = %d, want 1", stats.ChartSkippedCount)
	}
	if len(stats.ChartSkipReasons) != 1 || stats.ChartSkipReasons[0].ErrorClass != "insufficient_points" || stats.ChartSkipReasons[0].Count != 1 {
		t.Fatalf("unexpected chart skip reasons: %+v", stats.ChartSkipReasons)
	}
	if stats.RegistrationCount != 2 || stats.DeferredCount != 1 || stats.BudgetExceededCount != 1 {
		t.Fatalf("unexpected registration stats: %+v", stats)
	}
	if stats.SeedDeferredCount != 1 || stats.RescueDeferredCount != 1 {
		t.Fatalf("unexpected deferred counters: %+v", stats)
	}
}

func TestSQLiteStoreQueryCoupangRefreshStatsAbsorbsMalformedMetadata(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "coupang-stats-malformed.db"))
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().UTC()
	if err := store.InsertEvents(context.Background(), []StoredEvent{
		{
			OccurredAt:   now.Add(-4 * time.Minute),
			EventName:    string(EventCoupangLookupCoalesced),
			CommandID:    "쿠팡",
			RoomIDHash:   "room-1",
			MetadataJSON: `{"coalesced_hit":true}`,
		},
		{
			OccurredAt:   now.Add(-3 * time.Minute),
			EventName:    string(EventCoupangLookupCoalesced),
			CommandID:    "쿠팡",
			RoomIDHash:   "room-1",
			MetadataJSON: `{"coalesced_hit":`,
		},
		{
			OccurredAt:   now.Add(-2 * time.Minute),
			EventName:    string(EventReplyCompositeOutcome),
			FeatureKey:   "coupang",
			RoomIDHash:   "room-1",
			MetadataJSON: `{"chart_final_mode":"text_only_skipped"}`,
		},
		{
			OccurredAt:   now.Add(-time.Minute),
			EventName:    string(EventReplyCompositeOutcome),
			FeatureKey:   "coupang",
			RoomIDHash:   "room-1",
			MetadataJSON: `not-json`,
		},
	}); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	stats, err := store.QueryCoupangRefreshStats(context.Background(), now.Add(-time.Hour), now, "room-1")
	if err != nil {
		t.Fatalf("query coupang stats: %v", err)
	}
	if stats.JoinTotalCount != 1 || stats.JoinTimeoutCount != 0 {
		t.Fatalf("unexpected join stats: %+v", stats)
	}
	if stats.CompositeTotalCount != 2 || stats.PartialCount != 0 {
		t.Fatalf("unexpected composite stats: %+v", stats)
	}
	if stats.ChartSkippedCount != 1 {
		t.Fatalf("chart skipped count = %d, want 1", stats.ChartSkippedCount)
	}
	if len(stats.ChartSkipReasons) != 1 || stats.ChartSkipReasons[0].ErrorClass != "unknown" || stats.ChartSkipReasons[0].Count != 1 {
		t.Fatalf("unexpected chart skip reasons: %+v", stats.ChartSkipReasons)
	}
}
