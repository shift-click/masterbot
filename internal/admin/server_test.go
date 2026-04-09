package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/metrics"
)

type stubSmokeRunner struct {
	results []CommandSmokeResult
	err     error
}

func (r stubSmokeRunner) Run(context.Context, []CommandSmokeProbe) ([]CommandSmokeResult, error) {
	return append([]CommandSmokeResult(nil), r.results...), r.err
}

func TestAdminServerRejectsMissingEmailHeader(t *testing.T) {
	t.Parallel()

	server := newTestAdminServer(t)
	req := httptest.NewRequest(http.MethodGet, "/admin/api/overview", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAdminServerAllowsAllowlistedEmail(t *testing.T) {
	t.Parallel()

	server := newTestAdminServer(t)
	req := httptest.NewRequest(http.MethodGet, "/admin/api/overview", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Auth-Request-Email", "admin@example.com")
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload metrics.Overview
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
}

func TestAdminServerRejectsUntrustedProxy(t *testing.T) {
	t.Parallel()

	server := newTestAdminServer(t)
	req := httptest.NewRequest(http.MethodGet, "/admin/api/overview", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Auth-Request-Email", "admin@example.com")
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestAdminServerAudienceScopeDeniesOutOfScopeRoom(t *testing.T) {
	t.Parallel()

	server := newTestAdminServerWithAudience(t, []config.AdminAudienceScope{
		{
			Email:    "partner@example.com",
			Role:     "partner",
			Tenants:  []string{"tenant-a"},
			Rooms:    []string{"room-1"},
			Features: []string{"코인"},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/admin/api/room?room=room-2", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Auth-Request-Email", "partner@example.com")
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestAdminServerAudienceMeEndpoint(t *testing.T) {
	t.Parallel()

	server := newTestAdminServerWithAudience(t, []config.AdminAudienceScope{
		{
			Email:    "customer@example.com",
			Role:     "customer",
			Tenants:  []string{"tenant-a"},
			Rooms:    []string{"room-1"},
			Features: []string{"코인"},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/admin/api/me", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Auth-Request-Email", "customer@example.com")
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload["role"] != "customer" {
		t.Fatalf("role = %v, want customer", payload["role"])
	}
}

func TestAdminServerSmokeCommandsConfigured(t *testing.T) {
	t.Parallel()

	server := newTestAdminServer(t)
	server.smokeRunner = stubSmokeRunner{
		results: []CommandSmokeResult{
			{ID: "help", Message: "도움", OK: true, ReplyCount: 1, Replies: []string{"사용 가능한 명령어"}},
			{ID: "calc", Message: "100*2", OK: true, ReplyCount: 1, Replies: []string{"200"}},
		},
	}
	server.smokeProbes = []CommandSmokeProbe{
		{ID: "help", Message: "도움"},
		{ID: "calc", Message: "100*2"},
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/api/smoke/commands", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Auth-Request-Email", "admin@example.com")
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		OK      bool                 `json:"ok"`
		Skipped bool                 `json:"skipped"`
		Results []CommandSmokeResult `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if !payload.OK || payload.Skipped {
		t.Fatalf("unexpected smoke payload: %+v", payload)
	}
	if len(payload.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(payload.Results))
	}
}

func TestAdminServerSmokeCommandsSkippedWithoutConfiguration(t *testing.T) {
	t.Parallel()

	server := newTestAdminServer(t)
	req := httptest.NewRequest(http.MethodGet, "/admin/api/smoke/commands", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Auth-Request-Email", "admin@example.com")
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		OK      bool   `json:"ok"`
		Skipped bool   `json:"skipped"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if !payload.OK || !payload.Skipped {
		t.Fatalf("unexpected skipped payload: %+v", payload)
	}
	if payload.Reason == "" {
		t.Fatal("expected skipped reason")
	}
}

func TestAdminServerRoomDetailSanitizesIssueDetail(t *testing.T) {
	t.Parallel()

	server := newTestAdminServer(t)
	success := false
	if err := server.store.InsertEvents(context.Background(), []metrics.StoredEvent{
		{
			OccurredAt:       time.Now().UTC().Add(-5 * time.Minute),
			RequestID:        "issue-1",
			EventName:        string(metrics.EventCommandFailed),
			RoomIDHash:       "room-1",
			RoomLabel:        "운영방",
			RoomNameSnapshot: "실제방",
			UserIDHash:       "user-1",
			CommandID:        "코인",
			Success:          &success,
			ErrorClass:       "handler_error",
			MetadataJSON:     `{"error":"<script>alert(1)</script>\nboom"}`,
		},
	}); err != nil {
		t.Fatalf("insert issue event: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/api/room?room=room-1", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Auth-Request-Email", "admin@example.com")
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload metrics.RoomDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode room detail: %v", err)
	}
	if len(payload.RecentIssues) == 0 {
		t.Fatal("expected recent issues")
	}
	if strings.Contains(payload.RecentIssues[0].Detail, "<script>") {
		t.Fatalf("detail should be sanitized: %q", payload.RecentIssues[0].Detail)
	}
}

func TestDashboardHTMLDoesNotUseDynamicInnerHTML(t *testing.T) {
	t.Parallel()

	if strings.Contains(dashboardHTML, ".innerHTML =") {
		t.Fatal("dashboard should render dynamic data without innerHTML")
	}
}

func TestDashboardHTMLHasShortHashFallback(t *testing.T) {
	t.Parallel()

	if !strings.Contains(dashboardHTML, `const shortHash = (hash) => hash ? "방 #" + hash.slice(0, 8)`) {
		t.Fatal("dashboard should expose shortHash helper for empty-label rooms")
	}
	if !strings.Contains(dashboardHTML, "room.room_label || room.room_name_snapshot || shortHash(room.room_id_hash)") {
		t.Fatal("dashboard roomName fallback chain should end in shortHash")
	}
}

func TestDashboardHTMLAvoidsDuplicateLabelRendering(t *testing.T) {
	t.Parallel()

	// renderRoomRow must guard the secondary muted line so it never repeats
	// the primary label or the room_label value verbatim.
	if !strings.Contains(dashboardHTML, "secondary && secondary !== primary && secondary !== room.room_label") {
		t.Fatal("dashboard secondary line guard missing — duplicate labels would render")
	}
}

func TestDashboardHTMLHasFourTabIA(t *testing.T) {
	t.Parallel()

	for _, marker := range []string{
		`data-target="overview"`,
		`data-target="rooms"`,
		`data-target="reliability"`,
		`data-target="insights"`,
	} {
		if !strings.Contains(dashboardHTML, marker) {
			t.Fatalf("dashboard missing new four-tab marker: %s", marker)
		}
	}
	for _, removed := range []string{
		`data-target="home"`,
		`data-target="room-detail"`,
		`data-target="features"`,
		`data-target="product"`,
	} {
		if strings.Contains(dashboardHTML, removed) {
			t.Fatalf("dashboard still contains legacy tab marker: %s", removed)
		}
	}
}

func TestDashboardHTMLUsesKoreanTabLabels(t *testing.T) {
	t.Parallel()

	for _, label := range []string{
		`>개요</button>`,
		`>방</button>`,
		`>신뢰성</button>`,
		`>분석</button>`,
		"이상 징후",
		"방 순위",
		"오류 분해",
		"기능 사용량",
		"코호트",
		"리텐션",
	} {
		if !strings.Contains(dashboardHTML, label) {
			t.Fatalf("dashboard missing Korean label: %q", label)
		}
	}
	if strings.Contains(dashboardHTML, "전체 상태 요약과 방별 분석을") {
		t.Fatal("dashboard hero self-description prose should be removed")
	}
}

func TestDashboardHTMLDeclaresMonospaceCSS(t *testing.T) {
	t.Parallel()

	if !strings.Contains(dashboardHTML, "--font-mono:") {
		t.Fatal("dashboard CSS missing --font-mono variable")
	}
	if !strings.Contains(dashboardHTML, "tabular-nums") {
		t.Fatal("dashboard CSS missing tabular-nums numeric alignment")
	}
	if !strings.Contains(dashboardHTML, ".num-cell") {
		t.Fatal("dashboard CSS missing .num-cell rule")
	}
}

func TestDashboardHTMLComposesOverviewFromMultipleAPIs(t *testing.T) {
	t.Parallel()

	if !strings.Contains(dashboardHTML, "loadOverview") {
		t.Fatal("dashboard missing loadOverview function")
	}
	if !strings.Contains(dashboardHTML, `endpoint("./api/overview"`) {
		t.Fatal("dashboard loadOverview must still call /api/overview")
	}
	if !strings.Contains(dashboardHTML, "loadInsights") {
		t.Fatal("dashboard missing loadInsights function (renamed product)")
	}
	if !strings.Contains(dashboardHTML, "loadRoomDetailInline") {
		t.Fatal("dashboard missing inline room detail loader")
	}
}

func TestDashboardHTMLRoleTabsUseFourTabIDs(t *testing.T) {
	t.Parallel()

	if !strings.Contains(dashboardHTML, `["overview", "rooms", "reliability", "insights"]`) {
		t.Fatal("operator role tabs must use the four-tab IDs")
	}
	if strings.Contains(dashboardHTML, `["home", "rooms", "room-detail", "reliability", "features", "product"]`) {
		t.Fatal("legacy six-tab ID array must not remain in tabsForRole")
	}
}

func newTestAdminServer(t *testing.T) *Server {
	t.Helper()

	store, err := metrics.NewSQLiteStore(filepath.Join(t.TempDir(), "admin-metrics.db"))
	if err != nil {
		t.Fatalf("new metrics store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().UTC()
	success := true
	if err := store.InsertEvents(context.Background(), []metrics.StoredEvent{
		{
			OccurredAt:       now.Add(-time.Hour),
			RequestID:        "1",
			EventName:        string(metrics.EventMessageReceived),
			RoomIDHash:       "room-1",
			RoomLabel:        "운영방",
			RoomNameSnapshot: "실제방",
			UserIDHash:       "user-1",
		},
		{
			OccurredAt:       now.Add(-time.Hour),
			RequestID:        "1",
			EventName:        string(metrics.EventCommandSucceeded),
			RoomIDHash:       "room-1",
			RoomLabel:        "운영방",
			RoomNameSnapshot: "실제방",
			UserIDHash:       "user-1",
			CommandID:        "코인",
			CommandSource:    string(metrics.CommandSourceSlash),
			Success:          &success,
			LatencyMS:        120,
		},
	}); err != nil {
		t.Fatalf("insert sample events: %v", err)
	}
	if err := store.RebuildRollups(context.Background()); err != nil {
		t.Fatalf("rebuild rollups: %v", err)
	}

	cfg := config.Default().Admin
	cfg.Enabled = true
	cfg.MetricsEnabled = true
	cfg.PseudonymSecret = "secret"
	cfg.AllowedEmails = []string{"admin@example.com"}
	cfg.TrustedProxyCIDRs = []string{"127.0.0.1/32"}

	server, err := NewServer(cfg, store, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("new admin server: %v", err)
	}
	return server
}

func newTestAdminServerWithAudience(t *testing.T, scopes []config.AdminAudienceScope) *Server {
	t.Helper()
	server := newTestAdminServer(t)
	cfg := config.Default().Admin
	cfg.Enabled = true
	cfg.MetricsEnabled = true
	cfg.PseudonymSecret = "secret"
	cfg.AllowedEmails = []string{"admin@example.com"}
	cfg.AudienceScopes = scopes
	cfg.TrustedProxyCIDRs = []string{"127.0.0.1/32"}

	newServer, err := NewServer(cfg, server.store, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("new server with audience: %v", err)
	}
	return newServer
}
