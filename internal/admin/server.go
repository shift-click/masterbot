package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/metrics"
)

type FeatureStatsLoader func(context.Context, time.Time, time.Time, string) (metrics.CoupangFeatureStats, error)

type audienceProfile struct {
	Role            string
	AllowedTenants  map[string]struct{}
	AllowedRooms    map[string]struct{}
	AllowedFeatures map[string]struct{}
	AllTenants      bool
	AllRooms        bool
	AllFeatures     bool
}

type Server struct {
	cfg              config.AdminConfig
	store            *metrics.SQLiteStore
	featureLoader    FeatureStatsLoader
	smokeRunner      CommandSmokeRunner
	smokeProbes      []CommandSmokeProbe
	logger           *slog.Logger
	allowedEmails    map[string]struct{}
	audiencesByEmail map[string]audienceProfile
	trustedCIDRs     []*net.IPNet
	httpServer       *http.Server
}

type contextKey string

const adminEmailKey contextKey = "admin_email"
const adminAudienceKey contextKey = "admin_audience"

type queryScope struct {
	Audience string
	Tenant   string
	Room     string
	Feature  string
}

type statusError struct {
	status int
	msg    string
}

func (e statusError) Error() string { return e.msg }

func NewServer(cfg config.AdminConfig, store *metrics.SQLiteStore, featureLoader FeatureStatsLoader, smokeRunner CommandSmokeRunner, smokeProbes []CommandSmokeProbe, logger *slog.Logger) (*Server, error) {
	if store == nil {
		return nil, errors.New("metrics store is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	trustedCIDRs, err := parseTrustedCIDRs(cfg.TrustedProxyCIDRs)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]struct{}, len(cfg.AllowedEmails))
	for _, email := range cfg.AllowedEmails {
		email = strings.ToLower(strings.TrimSpace(email))
		if email == "" {
			continue
		}
		allowed[email] = struct{}{}
	}
	audiences := make(map[string]audienceProfile, len(cfg.AudienceScopes))
	for _, scope := range cfg.AudienceScopes {
		email := strings.ToLower(strings.TrimSpace(scope.Email))
		if email == "" {
			continue
		}
		allowed[email] = struct{}{}
		audiences[email] = audienceFromScope(scope)
	}
	for email := range allowed {
		if _, exists := audiences[email]; !exists {
			audiences[email] = audienceProfile{
				Role:        "operator",
				AllTenants:  true,
				AllRooms:    true,
				AllFeatures: true,
			}
		}
	}
	return &Server{
		cfg:              cfg,
		store:            store,
		featureLoader:    featureLoader,
		smokeRunner:      smokeRunner,
		smokeProbes:      append([]CommandSmokeProbe(nil), smokeProbes...),
		logger:           logger.With("component", "admin_server"),
		allowedEmails:    allowed,
		audiencesByEmail: audiences,
		trustedCIDRs:     trustedCIDRs,
	}, nil
}

func (s *Server) Start(ctx context.Context) error {
	if s == nil {
		return nil
	}
	handler := s.routes()
	s.httpServer = &http.Server{
		Addr:    s.cfg.ListenAddr,
		Handler: handler,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(shutdownCtx)
	}()
	err := s.httpServer.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	base := s.cfg.BasePath
	mux.HandleFunc(base, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, base+"/", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc(base+"/", s.handleDashboard)
	mux.HandleFunc(base+"/api/me", s.handleMe)
	mux.HandleFunc(base+"/api/overview", s.handleOverview)
	mux.HandleFunc(base+"/api/rooms", s.handleRooms)
	mux.HandleFunc(base+"/api/room", s.handleRoomDetail)
	mux.HandleFunc(base+"/api/reliability", s.handleReliability)
	mux.HandleFunc(base+"/api/features", s.handleFeatures)
	mux.HandleFunc(base+"/api/smoke/commands", s.handleSmokeCommands)
	mux.HandleFunc(base+"/api/product/funnel", s.handleProductFunnel)
	mux.HandleFunc(base+"/api/product/cohorts", s.handleProductCohorts)
	mux.HandleFunc(base+"/api/product/retention", s.handleProductRetention)
	return s.authMiddleware(mux)
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "read-only admin surface", http.StatusMethodNotAllowed)
			return
		}
		if !s.isTrustedProxy(r.RemoteAddr) {
			s.logger.Warn("admin access denied: untrusted proxy", "remote_addr", r.RemoteAddr, "path", r.URL.Path)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		email := strings.ToLower(strings.TrimSpace(r.Header.Get(s.cfg.AuthEmailHeader)))
		if email == "" {
			s.logger.Warn("admin access denied: missing auth email", "remote_addr", r.RemoteAddr, "path", r.URL.Path)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if _, ok := s.allowedEmails[email]; !ok {
			s.logger.Warn("admin access denied: email not allowlisted", "email", email, "path", r.URL.Path)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		profile, ok := s.audiencesByEmail[email]
		if !ok {
			s.logger.Warn("admin access denied: audience profile missing", "email", email, "path", r.URL.Path)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		s.logger.Info("admin request accepted", "email", email, "audience", profile.Role, "path", r.URL.Path)
		ctx := context.WithValue(r.Context(), adminEmailKey, email)
		ctx = context.WithValue(ctx, adminAudienceKey, profile)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTML))
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	profile, err := s.audienceFromRequest(r)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"role":             profile.Role,
		"allowed_tenants":  keysFromSet(profile.AllowedTenants),
		"allowed_rooms":    keysFromSet(profile.AllowedRooms),
		"allowed_features": keysFromSet(profile.AllowedFeatures),
		"all_tenants":      profile.AllTenants,
		"all_rooms":        profile.AllRooms,
		"all_features":     profile.AllFeatures,
	})
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	_, _, until := parseWindow(r, 24*time.Hour)
	overview, err := s.store.QueryOverview(r.Context(), until)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, overview)
}

func (s *Server) handleRooms(w http.ResponseWriter, r *http.Request) {
	commandID, since, until := parseWindow(r, 7*24*time.Hour)
	scope, err := s.resolveScope(r, false)
	if err != nil {
		writeError(w, err)
		return
	}
	limit := parseIntDefault(r.URL.Query().Get("limit"), 50)
	rooms, err := s.store.QueryRooms(r.Context(), since, until, scope.Room, commandID, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"rooms": rooms})
}

func (s *Server) handleRoomDetail(w http.ResponseWriter, r *http.Request) {
	scope, err := s.resolveScope(r, true)
	if err != nil {
		writeError(w, err)
		return
	}
	commandID, since, until := parseWindow(r, 7*24*time.Hour)
	detail, err := s.store.QueryRoomDetail(r.Context(), scope.Room, since, until, commandID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, detail)
}

func (s *Server) handleReliability(w http.ResponseWriter, r *http.Request) {
	commandID, since, until := parseWindow(r, 7*24*time.Hour)
	scope, err := s.resolveScope(r, false)
	if err != nil {
		writeError(w, err)
		return
	}
	reliability, err := s.store.QueryReliability(r.Context(), since, until, scope.Room, commandID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, reliability)
}

func (s *Server) handleFeatures(w http.ResponseWriter, r *http.Request) {
	_, since, until := parseWindow(r, 7*24*time.Hour)
	scope, err := s.resolveScope(r, false)
	if err != nil {
		writeError(w, err)
		return
	}
	usage, err := s.store.QueryFeatureUsage(r.Context(), since, until, scope.Room)
	if err != nil {
		writeError(w, err)
		return
	}
	coupang, err := s.store.QueryCoupangRefreshStats(r.Context(), since, until, scope.Room)
	if err != nil {
		writeError(w, err)
		return
	}
	if s.featureLoader != nil {
		extra, err := s.featureLoader(r.Context(), since, until, scope.Room)
		if err != nil {
			writeError(w, err)
			return
		}
		coupang.TrackedProducts = extra.TrackedProducts
		coupang.StaleProducts = extra.StaleProducts
		coupang.StaleRatio = extra.StaleRatio
	}
	writeJSON(w, metrics.FeatureOps{
		FeatureUsage: usage,
		Coupang:      coupang,
	})
}

func (s *Server) handleSmokeCommands(w http.ResponseWriter, r *http.Request) {
	if s.smokeRunner == nil || len(s.smokeProbes) == 0 {
		writeJSON(w, map[string]any{
			"ok":      true,
			"skipped": true,
			"reason":  "command smoke probes not configured",
		})
		return
	}
	results, err := s.smokeRunner.Run(r.Context(), s.smokeProbes)
	if err != nil {
		writeError(w, err)
		return
	}
	ok := true
	for _, result := range results {
		if !result.OK {
			ok = false
			break
		}
	}
	writeJSON(w, map[string]any{
		"ok":      ok,
		"skipped": false,
		"results": results,
	})
}

func (s *Server) handleProductFunnel(w http.ResponseWriter, r *http.Request) {
	_, since, until := parseWindow(r, 7*24*time.Hour)
	scope, err := s.resolveScope(r, false)
	if err != nil {
		writeError(w, err)
		return
	}
	funnel, err := s.store.QueryProductFunnel(r.Context(), since, until, scope.Audience, scope.Tenant, scope.Room, scope.Feature)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"funnel": funnel})
}

func (s *Server) handleProductCohorts(w http.ResponseWriter, r *http.Request) {
	_, since, until := parseWindow(r, 30*24*time.Hour)
	scope, err := s.resolveScope(r, false)
	if err != nil {
		writeError(w, err)
		return
	}
	cohorts, err := s.store.QueryProductCohorts(r.Context(), since, until, scope.Audience, scope.Tenant, scope.Room, scope.Feature)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"cohorts": cohorts})
}

func (s *Server) handleProductRetention(w http.ResponseWriter, r *http.Request) {
	_, since, until := parseWindow(r, 30*24*time.Hour)
	scope, err := s.resolveScope(r, false)
	if err != nil {
		writeError(w, err)
		return
	}
	retention, err := s.store.QueryProductRetention(r.Context(), since, until, scope.Audience, scope.Tenant, scope.Room, scope.Feature)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"retention": retention})
}

func parseWindow(r *http.Request, defaultWindow time.Duration) (commandID string, since time.Time, until time.Time) {
	commandID = strings.TrimSpace(r.URL.Query().Get("command"))
	window := defaultWindow
	if raw := strings.TrimSpace(r.URL.Query().Get("window")); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			window = parsed
		}
	}
	until = time.Now()
	since = until.Add(-window)
	return commandID, since, until
}

func parseIntDefault(value string, fallback int) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func audienceFromScope(scope config.AdminAudienceScope) audienceProfile {
	return audienceProfile{
		Role:            strings.ToLower(strings.TrimSpace(scope.Role)),
		AllowedTenants:  toSet(scope.Tenants),
		AllowedRooms:    toSet(scope.Rooms),
		AllowedFeatures: toSet(scope.Features),
		AllTenants:      containsWildcard(scope.Tenants),
		AllRooms:        containsWildcard(scope.Rooms),
		AllFeatures:     containsWildcard(scope.Features),
	}
}

func toSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || value == "*" {
			continue
		}
		out[value] = struct{}{}
	}
	return out
}

func containsWildcard(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "*" {
			return true
		}
	}
	return false
}

func keysFromSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	return out
}

func (s *Server) audienceFromRequest(r *http.Request) (audienceProfile, error) {
	raw := r.Context().Value(adminAudienceKey)
	profile, ok := raw.(audienceProfile)
	if !ok {
		return audienceProfile{}, statusError{status: http.StatusForbidden, msg: "audience context missing"}
	}
	if strings.TrimSpace(profile.Role) == "" {
		return audienceProfile{}, statusError{status: http.StatusForbidden, msg: "audience role missing"}
	}
	return profile, nil
}

func (s *Server) resolveScope(r *http.Request, requireRoom bool) (queryScope, error) {
	profile, err := s.audienceFromRequest(r)
	if err != nil {
		return queryScope{}, err
	}
	scope := parseScopeFromRequest(r, profile.Role)
	if profile.Role == "operator" {
		return scope, nil
	}
	if err := validateAudienceScope(profile, scope); err != nil {
		return queryScope{}, statusError{status: http.StatusForbidden, msg: "audience role mismatch"}
	}
	if err := validateTenantFeatureScope(profile, scope); err != nil {
		return queryScope{}, err
	}
	return resolveRoomScope(profile, scope, requireRoom)
}

func parseScopeFromRequest(r *http.Request, defaultAudience string) queryScope {
	scope := queryScope{
		Audience: strings.TrimSpace(r.URL.Query().Get("audience")),
		Tenant:   strings.TrimSpace(r.URL.Query().Get("tenant")),
		Room:     strings.TrimSpace(r.URL.Query().Get("room")),
		Feature:  strings.TrimSpace(r.URL.Query().Get("feature")),
	}
	if scope.Audience == "" {
		scope.Audience = strings.TrimSpace(r.Header.Get("X-Analytics-Audience"))
	}
	if scope.Audience == "" {
		scope.Audience = defaultAudience
	}
	return scope
}

func validateAudienceScope(profile audienceProfile, scope queryScope) error {
	if scope.Audience == profile.Role {
		return nil
	}
	return statusError{status: http.StatusForbidden, msg: "audience role mismatch"}
}

func validateTenantFeatureScope(profile audienceProfile, scope queryScope) error {
	if !profile.AllTenants && scope.Tenant != "" {
		if _, ok := profile.AllowedTenants[scope.Tenant]; !ok {
			return statusError{status: http.StatusForbidden, msg: "tenant scope denied"}
		}
	}
	if !profile.AllFeatures && scope.Feature != "" {
		if _, ok := profile.AllowedFeatures[scope.Feature]; !ok {
			return statusError{status: http.StatusForbidden, msg: "feature scope denied"}
		}
	}
	return nil
}

func resolveRoomScope(profile audienceProfile, scope queryScope, requireRoom bool) (queryScope, error) {
	if profile.AllRooms {
		return scope, nil
	}
	if scope.Room == "" {
		room, assigned := assignSingleAllowedRoom(profile.AllowedRooms)
		if assigned {
			scope.Room = room
		} else if requireRoom || len(profile.AllowedRooms) > 1 {
			return queryScope{}, statusError{status: http.StatusForbidden, msg: "room scope is required"}
		}
	}
	if scope.Room != "" {
		if _, ok := profile.AllowedRooms[scope.Room]; !ok {
			return queryScope{}, statusError{status: http.StatusForbidden, msg: "room scope denied"}
		}
	}
	return scope, nil
}

func assignSingleAllowedRoom(rooms map[string]struct{}) (string, bool) {
	if len(rooms) != 1 {
		return "", false
	}
	for room := range rooms {
		return room, true
	}
	return "", false
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	if err == nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var statusErr statusError
	if errors.As(err, &statusErr) {
		http.Error(w, statusErr.Error(), statusErr.status)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func parseTrustedCIDRs(values []string) ([]*net.IPNet, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("trusted proxy cidrs are required")
	}
	var cidrs []*net.IPNet
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if !strings.Contains(value, "/") {
			ip := net.ParseIP(value)
			if ip == nil {
				return nil, fmt.Errorf("invalid trusted proxy IP: %s", value)
			}
			maskBits := 32
			if ip.To4() == nil {
				maskBits = 128
			}
			value = fmt.Sprintf("%s/%d", ip.String(), maskBits)
		}
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, fmt.Errorf("parse trusted proxy cidr %q: %w", value, err)
		}
		cidrs = append(cidrs, network)
	}
	if len(cidrs) == 0 {
		return nil, fmt.Errorf("trusted proxy cidrs are required")
	}
	return cidrs, nil
}

func (s *Server) isTrustedProxy(remoteAddr string) bool {
	host := remoteAddr
	if parsedHost, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = parsedHost
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, network := range s.trustedCIDRs {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

const dashboardHTML = `<!doctype html>
<html lang="ko">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>JucoBot 운영 콘솔</title>
  <style>
    :root {
      --bg: #f4efe6;
      --panel: rgba(255, 252, 247, 0.94);
      --ink: #1f1f1b;
      --muted: #6d6a61;
      --accent: #0f766e;
      --accent-soft: #d6f0ed;
      --danger: #b42318;
      --warning: #b54708;
      --border: rgba(31, 31, 27, 0.12);
      --shadow: 0 18px 48px rgba(40, 27, 14, 0.08);
      --font-sans: "IBM Plex Sans", "Avenir Next", "Segoe UI", sans-serif;
      --font-mono: ui-monospace, "SF Mono", "Menlo", "Consolas", monospace;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: var(--font-sans);
      color: var(--ink);
      background:
        radial-gradient(circle at top left, rgba(15, 118, 110, 0.10), transparent 32%),
        linear-gradient(180deg, #fbf8f2 0%, var(--bg) 100%);
      min-height: 100vh;
    }
    .shell {
      max-width: 1320px;
      margin: 0 auto;
      padding: 22px 18px 48px;
    }
    .topbar {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 12px;
      margin-bottom: 18px;
    }
    .brand {
      display: flex;
      align-items: baseline;
      gap: 10px;
      margin-right: auto;
    }
    .brand h1 {
      margin: 0;
      font-size: clamp(1.4rem, 2.5vw, 1.8rem);
      font-weight: 700;
      letter-spacing: -0.02em;
    }
    .brand .crumb {
      color: var(--muted);
      font-size: 0.85rem;
    }
    .tabs {
      display: flex;
      flex-wrap: wrap;
      gap: 6px;
    }
    button, select {
      border: 1px solid var(--border);
      background: rgba(255,255,255,0.9);
      color: var(--ink);
      border-radius: 8px;
      padding: 8px 14px;
      font: inherit;
      cursor: pointer;
    }
    button.tab {
      font-weight: 600;
      font-size: 0.92rem;
    }
    button.tab.active {
      background: var(--ink);
      color: white;
      border-color: var(--ink);
    }
    .window-picker {
      color: var(--muted);
      font-size: 0.85rem;
      display: inline-flex;
      align-items: center;
      gap: 6px;
    }
    .grid {
      display: grid;
      gap: 14px;
      grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
    }
    .grid.kpi {
      margin-bottom: 18px;
    }
    .card {
      border: 1px solid var(--border);
      border-radius: 16px;
      background: var(--panel);
      box-shadow: var(--shadow);
      padding: 18px;
      backdrop-filter: blur(8px);
    }
    .label {
      color: var(--muted);
      font-size: 0.78rem;
      text-transform: uppercase;
      letter-spacing: 0.08em;
      font-weight: 600;
    }
    .value {
      margin-top: 6px;
      font-size: 1.85rem;
      font-weight: 700;
      letter-spacing: -0.02em;
      font-family: var(--font-mono);
      font-variant-numeric: tabular-nums;
    }
    .section {
      display: none;
      gap: 16px;
    }
    .section.active {
      display: grid;
    }
    .table-wrap {
      overflow: auto;
    }
    table {
      width: 100%;
      border-collapse: collapse;
    }
    th, td {
      padding: 11px 10px;
      border-bottom: 1px solid rgba(31, 31, 27, 0.08);
      text-align: left;
      font-size: 0.93rem;
      vertical-align: middle;
    }
    th {
      color: var(--muted);
      font-weight: 600;
      font-size: 0.78rem;
      text-transform: uppercase;
      letter-spacing: 0.06em;
    }
    td.num-cell, th.num-cell {
      text-align: right;
      font-family: var(--font-mono);
      font-variant-numeric: tabular-nums;
    }
    .pill {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      padding: 5px 10px;
      border-radius: 999px;
      background: var(--accent-soft);
      color: var(--accent);
      font-size: 0.78rem;
      font-weight: 600;
    }
    .pill.high { background: #fee4e2; color: var(--danger); }
    .pill.medium { background: #fff3db; color: var(--warning); }
    .muted { color: var(--muted); }
    .spark {
      display: inline-block;
      min-width: 120px;
      color: var(--muted);
      font-size: 0.82rem;
      font-family: var(--font-mono);
      font-variant-numeric: tabular-nums;
    }
    .room-link {
      color: var(--accent);
      text-decoration: none;
      cursor: pointer;
      font-weight: 600;
    }
    .room-link:hover { text-decoration: underline; }
    pre {
      white-space: pre-wrap;
      word-break: break-word;
      font-size: 0.8rem;
      color: var(--muted);
      font-family: var(--font-mono);
      margin: 0;
    }
    .anomaly {
      display: flex;
      gap: 10px;
      padding: 10px 0;
      border-bottom: 1px solid rgba(31, 31, 27, 0.06);
    }
    .anomaly:last-child { border-bottom: none; }
    .anomaly .body {
      display: flex;
      flex-direction: column;
      gap: 2px;
    }
    .anomaly strong { font-size: 0.95rem; }
    .anomaly .detail { color: var(--muted); font-size: 0.85rem; }
    .empty-msg {
      color: var(--muted);
      font-size: 0.9rem;
      margin: 0;
    }
    .rooms-master-detail {
      display: grid;
      grid-template-columns: minmax(0, 1.1fr) minmax(0, 1fr);
      gap: 14px;
    }
    @media (max-width: 900px) {
      .rooms-master-detail { grid-template-columns: 1fr; }
    }
    .detail-stack {
      display: flex;
      flex-direction: column;
      gap: 14px;
    }
    .detail-empty {
      color: var(--muted);
      font-size: 0.9rem;
      padding: 18px 4px;
    }
    .room-id-chip {
      display: inline-flex;
      align-items: center;
      padding: 3px 8px;
      border-radius: 6px;
      background: var(--accent-soft);
      color: var(--accent);
      font-family: var(--font-mono);
      font-size: 0.78rem;
      font-weight: 600;
    }
    @media (max-width: 640px) {
      .shell { padding: 16px 12px 28px; }
      .value { font-size: 1.45rem; }
    }
  </style>
</head>
<body>
  <div class="shell">
    <header class="topbar">
      <div class="brand">
        <h1>JucoBot 운영 콘솔</h1>
        <span class="crumb">읽기 전용 · 가명화 메트릭 기준</span>
      </div>
      <div class="tabs" role="tablist">
        <button class="tab active" data-target="overview">개요</button>
        <button class="tab" data-target="rooms">방</button>
        <button class="tab" data-target="reliability">신뢰성</button>
        <button class="tab" data-target="insights">분석</button>
      </div>
      <label class="window-picker">기간
        <select id="window">
          <option value="24h">24시간</option>
          <option value="168h" selected>7일</option>
          <option value="720h">30일</option>
        </select>
      </label>
    </header>

    <section id="overview" class="section active">
      <div id="overview-cards" class="grid kpi"></div>
      <div class="card">
        <div class="label">이상 징후</div>
        <div id="anomalies"></div>
      </div>
      <div class="card table-wrap">
        <div class="label">활성 방 상위 5</div>
        <table>
          <thead>
            <tr>
              <th>방</th>
              <th class="num-cell">요청</th>
              <th class="num-cell">사용자</th>
              <th class="num-cell">에러율</th>
              <th>추세</th>
            </tr>
          </thead>
          <tbody id="overview-top-rooms"></tbody>
        </table>
      </div>
    </section>

    <section id="rooms" class="section">
      <div class="rooms-master-detail">
        <div class="card table-wrap">
          <div class="label">방 순위</div>
          <table>
            <thead>
              <tr>
                <th>방</th>
                <th class="num-cell">요청</th>
                <th class="num-cell">사용자</th>
                <th class="num-cell">에러율</th>
                <th>추세</th>
              </tr>
            </thead>
            <tbody id="rooms-body"></tbody>
          </table>
        </div>
        <div class="detail-stack">
          <div class="card">
            <div class="label">방 상세</div>
            <div id="room-detail-summary"></div>
          </div>
          <div class="grid" id="room-detail-cards"></div>
          <div class="card table-wrap">
            <div class="label">명령 분포</div>
            <table>
              <thead>
                <tr>
                  <th>명령</th>
                  <th class="num-cell">요청</th>
                  <th class="num-cell">실패</th>
                </tr>
              </thead>
              <tbody id="room-commands"></tbody>
            </table>
          </div>
          <div class="card table-wrap">
            <div class="label">최근 이슈</div>
            <table>
              <thead>
                <tr>
                  <th>시각</th>
                  <th>이벤트</th>
                  <th>명령</th>
                  <th>세부</th>
                </tr>
              </thead>
              <tbody id="room-issues"></tbody>
            </table>
          </div>
        </div>
      </div>
    </section>

    <section id="reliability" class="section">
      <div id="reliability-cards" class="grid kpi"></div>
      <div class="card table-wrap">
        <div class="label">오류 분해</div>
        <table>
          <thead>
            <tr>
              <th>오류 분류</th>
              <th class="num-cell">건수</th>
            </tr>
          </thead>
          <tbody id="reliability-errors"></tbody>
        </table>
      </div>
      <div id="feature-cards" class="grid kpi"></div>
      <div class="card table-wrap">
        <div class="label">기능 사용량</div>
        <table>
          <thead>
            <tr>
              <th>기능</th>
              <th class="num-cell">요청</th>
              <th class="num-cell">실패</th>
            </tr>
          </thead>
          <tbody id="feature-usage"></tbody>
        </table>
      </div>
      <div class="card table-wrap">
        <div class="label">쿠팡 차트 스킵 사유</div>
        <table>
          <thead>
            <tr>
              <th>사유</th>
              <th class="num-cell">건수</th>
            </tr>
          </thead>
          <tbody id="feature-chart-reasons"></tbody>
        </table>
      </div>
    </section>

    <section id="insights" class="section">
      <div id="product-funnel-cards" class="grid kpi"></div>
      <div class="card table-wrap">
        <div class="label">코호트</div>
        <table>
          <thead>
            <tr>
              <th>코호트 일자</th>
              <th class="num-cell">활성화 사용자</th>
            </tr>
          </thead>
          <tbody id="product-cohorts"></tbody>
        </table>
      </div>
      <div class="card table-wrap">
        <div class="label">리텐션</div>
        <table>
          <thead>
            <tr>
              <th>코호트</th>
              <th>버킷</th>
              <th class="num-cell">코호트 크기</th>
              <th class="num-cell">잔존</th>
              <th class="num-cell">잔존율</th>
            </tr>
          </thead>
          <tbody id="product-retention"></tbody>
        </table>
      </div>
    </section>
  </div>
  <script>
    const state = { room: "", me: null, tabs: ["overview", "rooms", "reliability", "insights"] };
    const $ = (selector) => document.querySelector(selector);
    const fmtInt = (value) => Number(value || 0).toLocaleString("ko-KR");
    const fmtPct = (value) => ((value || 0) * 100).toFixed(1) + "%";
    const shortHash = (hash) => hash ? "방 #" + hash.slice(0, 8) : "-";
    const roomName = (room) => room.room_label || room.room_name_snapshot || shortHash(room.room_id_hash);
    const spark = (trend = []) => trend.map(point => point.count).join(" · ") || "데이터 없음";
    const windowValue = () => $("#window").value;
    const el = (tag, options = {}) => {
      const node = document.createElement(tag);
      if (options.className) node.className = options.className;
      if (options.text !== undefined && options.text !== null) node.textContent = String(options.text);
      if (options.attrs) {
        Object.entries(options.attrs).forEach(([key, value]) => {
          if (value !== undefined && value !== null) {
            node.setAttribute(key, String(value));
          }
        });
      }
      return node;
    };
    const cardNode = (label, value) => {
      const card = el("div", { className: "card" });
      card.append(el("div", { className: "label", text: label }));
      card.append(el("div", { className: "value", text: value }));
      return card;
    };
    const replaceChildren = (selector, children) => {
      const target = $(selector);
      target.replaceChildren(...children);
    };
    const emptyRow = (colspan, message) => {
      const row = document.createElement("tr");
      row.append(el("td", { className: "muted", text: message, attrs: { colspan } }));
      return row;
    };
    const cell = (value, className = "") => {
      const td = el("td", { className });
      if (value instanceof Node) {
        td.append(value);
      } else {
        td.textContent = String(value ?? "");
      }
      return td;
    };
    const numCell = (value) => cell(value, "num-cell");
    const issueDetail = (item) => {
      const pre = el("pre");
      pre.textContent = item.detail || item.error_class || "-";
      return pre;
    };
    const endpoint = (path, params = {}) => {
      const base = new URL("./", window.location.href);
      const url = new URL(path, base);
      Object.entries(params).forEach(([key, value]) => {
        if (value !== undefined && value !== null && value !== "") url.searchParams.set(key, value);
      });
      return url.toString();
    };
    const renderRoomRow = (room, onClick) => {
      const tr = document.createElement("tr");
      const roomCell = el("td");
      const primary = roomName(room);
      const link = el("a", {
        className: "room-link",
        text: primary,
        attrs: { href: "#", "data-room": room.room_id_hash || "" },
      });
      link.addEventListener("click", (event) => {
        event.preventDefault();
        const hash = link.getAttribute("data-room");
        state.room = hash;
        if (typeof onClick === "function") onClick(hash);
      });
      roomCell.append(link);
      const secondary = room.room_name_snapshot;
      if (secondary && secondary !== primary && secondary !== room.room_label) {
        roomCell.append(el("div", { className: "muted", text: secondary }));
      }
      tr.append(
        roomCell,
        numCell(fmtInt(room.requests)),
        numCell(fmtInt(room.active_users)),
        numCell(fmtPct(room.error_rate)),
        cell(el("span", { className: "spark", text: spark(room.trend) })),
      );
      return tr;
    };

    async function loadOverview() {
      const [overview, rooms] = await Promise.all([
        fetchJSON(endpoint("./api/overview", { window: windowValue(), room: state.room })),
        fetchJSON(endpoint("./api/rooms", { window: windowValue(), limit: 5, room: state.room })),
      ]);
      replaceChildren("#overview-cards", [
        cardNode("오늘 요청 수", fmtInt(overview.requests_today)),
        cardNode("에러율", fmtPct(overview.error_rate)),
        cardNode("p95 응답시간", fmtInt(overview.p95_latency_ms) + " ms"),
        cardNode("활성 방 수", fmtInt(overview.active_rooms)),
        cardNode("활성 사용자 수", fmtInt(overview.active_users)),
      ]);
      const anomalies = (overview.anomalies || []).map(item => {
        const wrap = el("div", { className: "anomaly" });
        wrap.append(el("span", { className: "pill " + (item.severity || ""), text: item.severity || "info" }));
        const body = el("div", { className: "body" });
        body.append(el("strong", { text: item.title || "-" }));
        body.append(el("span", { className: "detail", text: item.detail || "-" }));
        wrap.append(body);
        return wrap;
      });
      replaceChildren("#anomalies", anomalies.length ? anomalies : [el("p", { className: "empty-msg", text: "최근 구간에서 눈에 띄는 이상 징후가 없습니다." })]);
      const topRooms = (rooms.rooms || []).map(room => renderRoomRow(room, (hash) => {
        switchTab("rooms");
        loadRoomDetailInline(hash);
      }));
      replaceChildren("#overview-top-rooms", topRooms.length ? topRooms : [emptyRow(5, "데이터가 없습니다.")]);
    }

    async function loadRooms() {
      const data = await fetchJSON(endpoint("./api/rooms", { window: windowValue(), limit: 50, room: state.room }));
      const rooms = data.rooms || [];
      const rows = rooms.map(room => renderRoomRow(room, (hash) => loadRoomDetailInline(hash)));
      replaceChildren("#rooms-body", rows.length ? rows : [emptyRow(5, "데이터가 없습니다.")]);
      if (state.room) {
        await loadRoomDetailInline(state.room);
      } else {
        renderEmptyRoomDetail();
      }
    }

    function renderEmptyRoomDetail() {
      replaceChildren("#room-detail-summary", [el("p", { className: "detail-empty", text: "좌측 방 순위에서 방을 선택하세요." })]);
      replaceChildren("#room-detail-cards", []);
      replaceChildren("#room-commands", []);
      replaceChildren("#room-issues", []);
    }

    async function loadRoomDetailInline(hash) {
      if (!hash) {
        renderEmptyRoomDetail();
        return;
      }
      const data = await fetchJSON(endpoint("./api/room", { window: windowValue(), room: hash }));
      const summary = el("div");
      const heading = el("div", { className: "value", text: roomName(data) });
      summary.append(heading);
      if (data.room_id_hash) {
        summary.append(el("div", { className: "room-id-chip", text: shortHash(data.room_id_hash), attrs: { "data-room": data.room_id_hash } }));
      }
      replaceChildren("#room-detail-summary", [summary]);
      replaceChildren("#room-detail-cards", [
        cardNode("요청 수", fmtInt(data.requests)),
        cardNode("활성 사용자", fmtInt(data.active_users)),
        cardNode("에러율", fmtPct(data.error_rate)),
        cardNode("Explicit", fmtInt(data.explicit_count)),
        cardNode("Auto", fmtInt(data.auto_count)),
      ]);
      const commandRows = (data.commands || []).map(item => {
        const tr = document.createElement("tr");
        tr.append(cell(item.command_id || "-"), numCell(fmtInt(item.requests)), numCell(fmtInt(item.errors)));
        return tr;
      });
      replaceChildren("#room-commands", commandRows.length ? commandRows : [emptyRow(3, "데이터가 없습니다.")]);
      const issueRows = (data.recent_issues || []).map(item => {
        const tr = document.createElement("tr");
        tr.append(
          cell(item.occurred_at || "-"),
          cell(item.event_name || "-"),
          cell(item.command_id || "-"),
          cell(issueDetail(item)),
        );
        return tr;
      });
      replaceChildren("#room-issues", issueRows.length ? issueRows : [emptyRow(4, "최근 이슈가 없습니다.")]);
    }

    async function loadReliability() {
      const [reliability, features] = await Promise.all([
        fetchJSON(endpoint("./api/reliability", { window: windowValue(), room: state.room })),
        fetchJSON(endpoint("./api/features", { window: windowValue(), room: state.room })),
      ]);
      replaceChildren("#reliability-cards", [
        cardNode("완료 명령 수", fmtInt(reliability.total_commands)),
        cardNode("실패 명령 수", fmtInt(reliability.failed_commands)),
        cardNode("에러율", fmtPct(reliability.error_rate)),
        cardNode("p95 응답시간", fmtInt(reliability.p95_latency_ms) + " ms"),
        cardNode("Rate Limit", fmtInt(reliability.rate_limited_count)),
        cardNode("ACL Deny", fmtInt(reliability.access_denied_count)),
        cardNode("응답 실패", fmtInt(reliability.reply_failed_count)),
        cardNode("Ingress 과부하", fmtInt(reliability.overload_count)),
      ]);
      const errorRows = (reliability.errors_by_class || []).map(item => {
        const tr = document.createElement("tr");
        tr.append(cell(item.error_class || "-"), numCell(fmtInt(item.count)));
        return tr;
      });
      replaceChildren("#reliability-errors", errorRows.length ? errorRows : [emptyRow(2, "오류 데이터가 없습니다.")]);
      const coupang = features.coupang || {};
      replaceChildren("#feature-cards", [
        cardNode("쿠팡 추적 상품", fmtInt(coupang.tracked_products)),
        cardNode("쿠팡 stale 상품", fmtInt(coupang.stale_products)),
        cardNode("쿠팡 stale 비율", fmtPct(coupang.stale_ratio)),
        cardNode("쿠팡 refresh 성공", fmtInt(coupang.refresh_success_count)),
        cardNode("쿠팡 refresh 실패", fmtInt(coupang.refresh_failure_count)),
        cardNode("부분 응답 비율", fmtPct(coupang.partial_ratio)),
        cardNode("Join Timeout 비율", fmtPct(coupang.join_timeout_ratio)),
        cardNode("차트 스킵", fmtInt(coupang.chart_skipped_count)),
      ]);
      const usageRows = (features.feature_usage || []).map(item => {
        const tr = document.createElement("tr");
        tr.append(cell(item.command_id || "-"), numCell(fmtInt(item.requests)), numCell(fmtInt(item.errors)));
        return tr;
      });
      replaceChildren("#feature-usage", usageRows.length ? usageRows : [emptyRow(3, "기능 사용량 데이터가 없습니다.")]);
      const reasonRows = (coupang.chart_skip_reasons || []).map(item => {
        const tr = document.createElement("tr");
        tr.append(cell(item.error_class || "-"), numCell(fmtInt(item.count)));
        return tr;
      });
      replaceChildren("#feature-chart-reasons", reasonRows.length ? reasonRows : [emptyRow(2, "차트 스킵 사유 데이터가 없습니다.")]);
    }

    async function loadInsights() {
      const params = { window: windowValue(), room: state.room };
      const [funnelData, cohortData, retentionData] = await Promise.all([
        fetchJSON(endpoint("./api/product/funnel", params)),
        fetchJSON(endpoint("./api/product/cohorts", params)),
        fetchJSON(endpoint("./api/product/retention", params)),
      ]);
      const funnel = funnelData.funnel || [];
      const funnelCards = funnel.map(item => cardNode(item.stage || "-", fmtInt(item.count) + " / 전환 " + fmtPct(item.conversion_rate || 0)));
      if (funnelCards.length === 0) {
        const emptyCard = el("div", { className: "card" });
        emptyCard.append(el("div", { className: "label", text: "퍼널" }));
        emptyCard.append(el("p", { className: "empty-msg", text: "데이터가 없습니다." }));
        funnelCards.push(emptyCard);
      }
      replaceChildren("#product-funnel-cards", funnelCards);
      const cohortRows = (cohortData.cohorts || []).map(item => {
        const tr = document.createElement("tr");
        tr.append(cell(item.cohort_date || "-"), numCell(fmtInt(item.activation_users)));
        return tr;
      });
      replaceChildren("#product-cohorts", cohortRows.length ? cohortRows : [emptyRow(2, "코호트 데이터가 없습니다.")]);
      const retentionRows = (retentionData.retention || []).map(item => {
        const tr = document.createElement("tr");
        tr.append(
          cell(item.cohort_date || "-"),
          cell(item.bucket_date || "-"),
          numCell(fmtInt(item.cohort_size)),
          numCell(fmtInt(item.retained_users)),
          numCell(fmtPct(item.retention_rate)),
        );
        return tr;
      });
      replaceChildren("#product-retention", retentionRows.length ? retentionRows : [emptyRow(5, "리텐션 데이터가 없습니다.")]);
    }

    async function loadMe() {
      const data = await fetchJSON(endpoint("./api/me"));
      state.me = data;
      if (!state.room && Array.isArray(data.allowed_rooms) && data.allowed_rooms.length > 0) {
        state.room = data.allowed_rooms[0];
      }
      state.tabs = tabsForRole(data.role);
      applyVisibleTabs();
    }

    function tabsForRole(role) {
      if (role === "operator") {
        return ["overview", "rooms", "reliability", "insights"];
      }
      if (role === "partner" || role === "customer") {
        return ["overview", "rooms", "insights"];
      }
      return ["overview"];
    }

    function applyVisibleTabs() {
      const allowed = new Set(state.tabs);
      document.querySelectorAll(".tab").forEach(button => {
        const visible = allowed.has(button.dataset.target);
        button.style.display = visible ? "inline-flex" : "none";
      });
      document.querySelectorAll(".section").forEach(section => {
        section.style.display = allowed.has(section.id) ? "" : "none";
      });
      const active = document.querySelector(".tab.active");
      if (!active || !allowed.has(active.dataset.target)) {
        switchTab(state.tabs[0] || "overview");
      }
    }

    async function fetchJSON(url) {
      const response = await fetch(url, { headers: { "Accept": "application/json" } });
      if (!response.ok) throw new Error(await response.text());
      return response.json();
    }

    function switchTab(target) {
      if (!state.tabs.includes(target)) return;
      document.querySelectorAll(".tab").forEach(button => button.classList.toggle("active", button.dataset.target === target));
      document.querySelectorAll(".section").forEach(section => section.classList.toggle("active", section.id === target));
    }

    async function refreshAll() {
      if (!state.me) {
        await loadMe();
      }
      const jobs = [];
      if (state.tabs.includes("overview")) jobs.push(loadOverview());
      if (state.tabs.includes("rooms")) jobs.push(loadRooms());
      if (state.tabs.includes("reliability")) jobs.push(loadReliability());
      if (state.tabs.includes("insights")) jobs.push(loadInsights());
      await Promise.all(jobs);
    }

    document.querySelectorAll(".tab").forEach(button => {
      button.addEventListener("click", () => switchTab(button.dataset.target));
    });
    $("#window").addEventListener("change", refreshAll);
    refreshAll().catch(error => {
      const shell = el("div", { className: "shell" });
      const card = el("div", { className: "card" });
      card.append(el("strong", { text: "대시보드 로드 실패" }));
      card.append(issueDetail({ detail: error.message }));
      shell.append(card);
      document.body.append(shell);
    });
  </script>
</body>
</html>`
