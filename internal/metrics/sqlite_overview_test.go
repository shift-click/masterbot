package metrics

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSQLiteStoreOverviewAndDetailQueries(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "overview.db"))
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().UTC()
	success := true
	failure := false

	var events []StoredEvent
	for i := 0; i < 12; i++ {
		occurredAt := now.Add(-36 * time.Hour).Add(time.Duration(i) * time.Minute)
		events = append(events,
			StoredEvent{
				OccurredAt:       occurredAt,
				RequestID:        "prev-msg-" + itoa(i),
				EventName:        string(EventMessageReceived),
				RoomIDHash:       "room-1",
				RoomLabel:        "운영방",
				RoomNameSnapshot: "운영방 실명",
				UserIDHash:       "user-a",
			},
			StoredEvent{
				OccurredAt:       occurredAt,
				RequestID:        "prev-cmd-" + itoa(i),
				EventName:        string(EventCommandSucceeded),
				RoomIDHash:       "room-1",
				RoomLabel:        "운영방",
				RoomNameSnapshot: "운영방 실명",
				UserIDHash:       "user-a",
				CommandID:        "코인",
				CommandSource:    string(CommandSourceSlash),
				Success:          &success,
				LatencyMS:        320,
			},
		)
	}

	for i := 0; i < 20; i++ {
		occurredAt := now.Add(-90 * time.Minute).Add(time.Duration(i) * time.Minute)
		cmdName := "코인"
		if i%3 == 0 {
			cmdName = "주식"
		}
		cmdEvent := string(EventCommandSucceeded)
		successRef := &success
		errClass := ""
		if i < 6 {
			cmdEvent = string(EventCommandFailed)
			successRef = &failure
			errClass = "handler_error"
		}
		events = append(events,
			StoredEvent{
				OccurredAt:       occurredAt,
				RequestID:        "curr-msg-" + itoa(i),
				EventName:        string(EventMessageReceived),
				RoomIDHash:       "room-1",
				RoomLabel:        "운영방",
				RoomNameSnapshot: "운영방 실명",
				UserIDHash:       "user-b",
			},
			StoredEvent{
				OccurredAt:       occurredAt,
				RequestID:        "curr-cmd-" + itoa(i),
				EventName:        cmdEvent,
				RoomIDHash:       "room-1",
				RoomLabel:        "운영방",
				RoomNameSnapshot: "운영방 실명",
				UserIDHash:       "user-b",
				CommandID:        cmdName,
				CommandSource:    string(CommandSourceExplicit),
				Success:          successRef,
				ErrorClass:       errClass,
				LatencyMS:        2100,
			},
		)
	}

	for i := 0; i < 12; i++ {
		events = append(events, StoredEvent{
			OccurredAt:       now.Add(-70 * time.Minute).Add(time.Duration(i) * time.Minute),
			RequestID:        "deny-" + itoa(i),
			EventName:        string(EventAccessDenied),
			RoomIDHash:       "room-1",
			RoomLabel:        "운영방",
			RoomNameSnapshot: "운영방 실명",
			UserIDHash:       "user-c",
			CommandID:        "코인",
		})
	}

	events = append(events,
		StoredEvent{
			OccurredAt:    now.Add(-2 * time.Hour),
			RequestID:     "cp-ok",
			EventName:     string(EventCoupangRefreshSucceeded),
			CommandID:     "쿠팡",
			RoomIDHash:    "room-1",
			RoomLabel:     "운영방",
			RoomScopeHash: "room-1",
		},
		StoredEvent{
			OccurredAt:    now.Add(-90 * time.Minute),
			RequestID:     "cp-fail",
			EventName:     string(EventCoupangRefreshFailed),
			CommandID:     "쿠팡",
			RoomIDHash:    "room-1",
			RoomLabel:     "운영방",
			RoomScopeHash: "room-1",
		},
		StoredEvent{
			OccurredAt:       now.Add(-45 * time.Minute),
			RequestID:        "overload-1",
			EventName:        string(EventTransportOverload),
			RoomIDHash:       "room-1",
			RoomLabel:        "운영방",
			RoomNameSnapshot: "운영방 실명",
			UserIDHash:       "user-d",
			ErrorClass:       "room_buffer_full",
			MetadataJSON:     `{"drop_reason":"room_buffer_full","queue_len":64,"error":"<script>alert(1)</script>"}`,
		},
	)

	if err := store.InsertEvents(context.Background(), events); err != nil {
		t.Fatalf("insert events: %v", err)
	}
	if err := store.RebuildRollups(context.Background()); err != nil {
		t.Fatalf("rebuild rollups: %v", err)
	}

	overview, err := store.QueryOverview(context.Background(), now)
	if err != nil {
		t.Fatalf("query overview: %v", err)
	}
	if overview.RequestsToday == 0 {
		t.Fatalf("expected requests today > 0: %+v", overview)
	}
	if len(overview.Anomalies) == 0 {
		t.Fatalf("expected anomalies, got %+v", overview)
	}

	detail, err := store.QueryRoomDetail(context.Background(), "room-1", now.Add(-72*time.Hour), now, "")
	if err != nil {
		t.Fatalf("query room detail: %v", err)
	}
	if detail.RoomIDHash != "room-1" {
		t.Fatalf("unexpected room id: %+v", detail)
	}
	if len(detail.Commands) == 0 {
		t.Fatalf("expected command usage in room detail: %+v", detail)
	}
	if len(detail.Hourly) == 0 {
		t.Fatalf("expected hourly trend in room detail: %+v", detail)
	}
	if len(detail.RecentIssues) == 0 {
		t.Fatalf("expected recent issues in room detail: %+v", detail)
	}
	if detail.RecentIssues[0].Detail == "" {
		t.Fatalf("expected summarized issue detail: %+v", detail.RecentIssues[0])
	}
	if strings.Contains(detail.RecentIssues[0].Detail, "<script>") {
		t.Fatalf("issue detail should be sanitized: %+v", detail.RecentIssues[0])
	}

	usage, err := store.QueryFeatureUsage(context.Background(), now.Add(-72*time.Hour), now, "room-1")
	if err != nil {
		t.Fatalf("query feature usage: %v", err)
	}
	if len(usage) == 0 {
		t.Fatal("expected feature usage rows")
	}

	coupangStats, err := store.QueryCoupangRefreshStats(context.Background(), now.Add(-72*time.Hour), now, "room-1")
	if err != nil {
		t.Fatalf("query coupang refresh stats: %v", err)
	}
	if coupangStats.RefreshSuccessCount != 1 || coupangStats.RefreshFailureCount != 1 {
		t.Fatalf("unexpected coupang stats: %+v", coupangStats)
	}

	reliability, err := store.QueryReliability(context.Background(), now.Add(-72*time.Hour), now, "room-1", "")
	if err != nil {
		t.Fatalf("query reliability: %v", err)
	}
	if reliability.OverloadCount != 1 {
		t.Fatalf("overload_count = %d, want 1", reliability.OverloadCount)
	}
}

func TestOverviewAnomalyHelpersAndNoopRecorder(t *testing.T) {
	t.Parallel()

	anomalies := detectOverviewAnomalies(overviewAnomalyInputs{
		currentCmds:   20,
		currentFails:  5,
		prevCmds:      20,
		prevFails:     0,
		currentP95:    2200,
		prevP95:       900,
		currentDenied: 12,
		prevDenied:    1,
	})
	if len(anomalies) != 3 {
		t.Fatalf("anomalies len = %d, want 3", len(anomalies))
	}
	if !isErrorRateSpike(20, 0.25, 0.01) {
		t.Fatal("expected error-rate spike")
	}
	if !isLatencyRegression(2200, 1200) {
		t.Fatal("expected latency regression")
	}
	if !isAccessDeniedSpike(12, 2) {
		t.Fatal("expected access-denied spike")
	}

	var r NoopRecorder
	r.Record(context.Background(), Event{})
}

func itoa(v int) string {
	return fmt.Sprintf("%d", v)
}
