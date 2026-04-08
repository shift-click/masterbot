package admin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/metrics"
)

func TestAdminHelperParsersAndSets(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/x?command=coin&window=2h", nil)
	cmd, since, until := parseWindow(req, 24*time.Hour)
	if cmd != "coin" {
		t.Fatalf("command = %q", cmd)
	}
	if delta := until.Sub(since); delta < 2*time.Hour-2*time.Second || delta > 2*time.Hour+2*time.Second {
		t.Fatalf("window delta = %v", delta)
	}
	req = httptest.NewRequest(http.MethodGet, "/x?window=bad", nil)
	_, since, until = parseWindow(req, 3*time.Hour)
	if delta := until.Sub(since); delta < 3*time.Hour-2*time.Second || delta > 3*time.Hour+2*time.Second {
		t.Fatalf("fallback window delta = %v", delta)
	}

	if got := parseIntDefault("", 7); got != 7 {
		t.Fatalf("parseIntDefault empty = %d", got)
	}
	if got := parseIntDefault("12", 7); got != 12 {
		t.Fatalf("parseIntDefault parsed = %d", got)
	}
	if got := parseIntDefault("-1", 7); got != 7 {
		t.Fatalf("parseIntDefault invalid = %d", got)
	}

	scope := audienceFromScope(config.AdminAudienceScope{
		Role:     "Partner",
		Tenants:  []string{"tenant-a", "*"},
		Rooms:    []string{"room-1"},
		Features: []string{"coin"},
	})
	if scope.Role != "partner" || !scope.AllTenants || scope.AllRooms || scope.AllFeatures {
		t.Fatalf("audienceFromScope unexpected result: %+v", scope)
	}
	if set := toSet([]string{" a ", "", "*", "b"}); len(set) != 2 {
		t.Fatalf("toSet = %v", set)
	}
	if !containsWildcard([]string{"a", "*", "b"}) || containsWildcard([]string{"a", "b"}) {
		t.Fatal("containsWildcard mismatch")
	}
	if keys := keysFromSet(map[string]struct{}{"x": {}, "y": {}}); len(keys) != 2 {
		t.Fatalf("keysFromSet len = %d", len(keys))
	}
}

func TestAdminScopeResolutionAndValidation(t *testing.T) {
	t.Parallel()

	profile := audienceProfile{
		Role:            "partner",
		AllowedTenants:  map[string]struct{}{"tenant-a": {}},
		AllowedRooms:    map[string]struct{}{"room-1": {}},
		AllowedFeatures: map[string]struct{}{"coin": {}},
	}

	req := httptest.NewRequest(http.MethodGet, "/x?audience=partner&tenant=tenant-a&room=room-1&feature=coin", nil)
	scope := parseScopeFromRequest(req, "customer")
	if scope.Audience != "partner" || scope.Room != "room-1" {
		t.Fatalf("parseScopeFromRequest unexpected scope: %+v", scope)
	}

	if err := validateAudienceScope(profile, queryScope{Audience: "customer"}); err == nil {
		t.Fatal("expected audience scope mismatch")
	}
	if err := validateTenantFeatureScope(profile, queryScope{Tenant: "other"}); err == nil {
		t.Fatal("expected tenant denied")
	}
	if err := validateTenantFeatureScope(profile, queryScope{Feature: "stock"}); err == nil {
		t.Fatal("expected feature denied")
	}
	if _, err := resolveRoomScope(profile, queryScope{Room: "other"}, false); err == nil {
		t.Fatal("expected room denied")
	}

	// Single allowed room gets auto-assigned when omitted.
	resolved, err := resolveRoomScope(profile, queryScope{}, false)
	if err != nil || resolved.Room != "room-1" {
		t.Fatalf("resolveRoomScope single room = (%+v,%v)", resolved, err)
	}

	// Multiple rooms require explicit room when requireRoom=true.
	profile.AllowedRooms = map[string]struct{}{"room-1": {}, "room-2": {}}
	if _, err := resolveRoomScope(profile, queryScope{}, true); err == nil {
		t.Fatal("expected room required error for multiple rooms")
	}

	if room, ok := assignSingleAllowedRoom(map[string]struct{}{"only-room": {}}); !ok || room != "only-room" {
		t.Fatalf("assignSingleAllowedRoom = (%q,%v)", room, ok)
	}
	if _, ok := assignSingleAllowedRoom(map[string]struct{}{"a": {}, "b": {}}); ok {
		t.Fatal("assignSingleAllowedRoom should fail for multiple rooms")
	}
}

func TestAdminAudienceFromRequestAndWriteHelpers(t *testing.T) {
	t.Parallel()

	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if _, err := s.audienceFromRequest(req); err == nil {
		t.Fatal("expected missing audience context error")
	}

	req = req.WithContext(context.WithValue(req.Context(), adminAudienceKey, audienceProfile{Role: ""}))
	if _, err := s.audienceFromRequest(req); err == nil {
		t.Fatal("expected empty audience role error")
	}

	req = req.WithContext(context.WithValue(req.Context(), adminAudienceKey, audienceProfile{Role: "operator"}))
	if profile, err := s.audienceFromRequest(req); err != nil || profile.Role != "operator" {
		t.Fatalf("audienceFromRequest = (%+v,%v)", profile, err)
	}

	rec := httptest.NewRecorder()
	writeJSON(rec, map[string]any{"ok": true})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "\"ok\"") {
		t.Fatalf("writeJSON response = code %d body %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	writeError(rec, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("writeError nil code = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	writeError(rec, statusError{status: http.StatusForbidden, msg: "deny"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("writeError statusError code = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	writeError(rec, errors.New("boom"))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("writeError generic response = code %d body %s", rec.Code, rec.Body.String())
	}
}

func TestAdminTrustedCIDRAndAuthMiddlewarePaths(t *testing.T) {
	t.Parallel()

	cidrs, err := parseTrustedCIDRs([]string{"127.0.0.1", "::1/128"})
	if err != nil || len(cidrs) != 2 {
		t.Fatalf("parseTrustedCIDRs valid = (%v,%v)", cidrs, err)
	}
	if _, err := parseTrustedCIDRs(nil); err == nil {
		t.Fatal("expected parseTrustedCIDRs error for empty")
	}
	if _, err := parseTrustedCIDRs([]string{"bad-ip"}); err == nil {
		t.Fatal("expected parseTrustedCIDRs error for invalid ip")
	}

	s := &Server{
		cfg: config.AdminConfig{
			AuthEmailHeader: "X-Auth-Request-Email",
		},
		logger: slog.Default(),
		allowedEmails: map[string]struct{}{
			"admin@example.com": {},
		},
		audiencesByEmail: map[string]audienceProfile{
			"admin@example.com": {Role: "operator", AllRooms: true, AllTenants: true, AllFeatures: true},
		},
		trustedCIDRs: cidrs,
	}

	handler := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))

	makeReq := func(method, remote, email string) *http.Request {
		req := httptest.NewRequest(method, "/admin/api/me", nil)
		req.RemoteAddr = remote
		if email != "" {
			req.Header.Set("X-Auth-Request-Email", email)
		}
		return req
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, makeReq(http.MethodPost, "127.0.0.1:1234", "admin@example.com"))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method guard status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, makeReq(http.MethodGet, "10.0.0.1:1234", "admin@example.com"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("untrusted proxy status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, makeReq(http.MethodGet, "127.0.0.1:1234", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing email status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, makeReq(http.MethodGet, "127.0.0.1:1234", "other@example.com"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("allowlist deny status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, makeReq(http.MethodGet, "127.0.0.1:1234", "admin@example.com"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "\"ok\":true") {
		t.Fatalf("authorized status/body = %d %s", rec.Code, rec.Body.String())
	}

	if !s.isTrustedProxy("127.0.0.1:9000") || s.isTrustedProxy("bad-addr") {
		t.Fatal("isTrustedProxy behavior mismatch")
	}
}

func TestResolveScopeOnServerProfiles(t *testing.T) {
	t.Parallel()

	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(context.WithValue(req.Context(), adminAudienceKey, audienceProfile{
		Role:            "partner",
		AllowedTenants:  map[string]struct{}{"tenant-a": {}},
		AllowedRooms:    map[string]struct{}{"room-1": {}},
		AllowedFeatures: map[string]struct{}{"coin": {}},
	}))
	req = httptest.NewRequest(http.MethodGet, "/x?audience=partner&tenant=tenant-a&room=room-1&feature=coin", nil).WithContext(req.Context())
	if scope, err := s.resolveScope(req, true); err != nil || scope.Room != "room-1" {
		t.Fatalf("resolveScope valid = (%+v,%v)", scope, err)
	}

	req = httptest.NewRequest(http.MethodGet, "/x?audience=partner&tenant=other", nil).WithContext(req.Context())
	if _, err := s.resolveScope(req, false); err == nil {
		t.Fatal("expected resolveScope error for denied tenant")
	}

	// Operator bypasses fine-grained checks.
	req = httptest.NewRequest(http.MethodGet, "/x?tenant=other&room=other", nil)
	req = req.WithContext(context.WithValue(req.Context(), adminAudienceKey, audienceProfile{Role: "operator"}))
	if _, err := s.resolveScope(req, false); err != nil {
		t.Fatalf("operator resolveScope should pass: %v", err)
	}
}

func TestAdminServerHandlersAndStart(t *testing.T) {
	t.Parallel()

	server := newTestAdminServer(t)
	server.featureLoader = func(context.Context, time.Time, time.Time, string) (metrics.CoupangFeatureStats, error) {
		return metrics.CoupangFeatureStats{
			TrackedProducts: 2,
			StaleProducts:   1,
			StaleRatio:      0.5,
		}, nil
	}
	handler := server.routes()

	call := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Auth-Request-Email", "admin@example.com")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := call("/admin"); rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("/admin redirect status = %d", rec.Code)
	}
	if rec := call("/admin/"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "JucoBot Operations Console") {
		t.Fatalf("/admin/ status/body = %d %s", rec.Code, rec.Body.String())
	}
	for _, path := range []string{
		"/admin/api/rooms",
		"/admin/api/room?room=room-1",
		"/admin/api/reliability",
		"/admin/api/features",
		"/admin/api/product/funnel",
		"/admin/api/product/cohorts",
		"/admin/api/product/retention",
	} {
		if rec := call(path); rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", path, rec.Code, rec.Body.String())
		}
	}

	// Start/Shutdown path
	server.cfg.ListenAddr = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.Start(ctx)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server.Start() returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server.Start() did not stop after context cancellation")
	}
}
