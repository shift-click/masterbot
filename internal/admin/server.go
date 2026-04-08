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
  <title>JucoBot Admin</title>
  <style>
    :root {
      --bg: #f4efe6;
      --panel: rgba(255, 252, 247, 0.92);
      --ink: #1f1f1b;
      --muted: #6d6a61;
      --accent: #0f766e;
      --accent-soft: #d6f0ed;
      --danger: #b42318;
      --warning: #b54708;
      --border: rgba(31, 31, 27, 0.12);
      --shadow: 0 24px 60px rgba(40, 27, 14, 0.08);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: "IBM Plex Sans", "Avenir Next", "Segoe UI", sans-serif;
      color: var(--ink);
      background:
        radial-gradient(circle at top left, rgba(15, 118, 110, 0.14), transparent 28%),
        radial-gradient(circle at top right, rgba(181, 71, 8, 0.12), transparent 24%),
        linear-gradient(180deg, #fbf8f2 0%, var(--bg) 100%);
      min-height: 100vh;
    }
    .shell {
      max-width: 1280px;
      margin: 0 auto;
      padding: 24px 18px 48px;
    }
    .hero {
      display: grid;
      gap: 18px;
      margin-bottom: 20px;
      padding: 22px;
      border: 1px solid var(--border);
      border-radius: 24px;
      background: linear-gradient(140deg, rgba(255,255,255,0.95), rgba(233,245,242,0.9));
      box-shadow: var(--shadow);
    }
    .hero h1 {
      margin: 0;
      font-family: Georgia, "Iowan Old Style", serif;
      font-size: clamp(2rem, 4vw, 3.4rem);
      font-weight: 600;
      letter-spacing: -0.03em;
    }
    .hero p { margin: 0; color: var(--muted); max-width: 760px; }
    .controls {
      display: flex;
      flex-wrap: wrap;
      gap: 12px;
      align-items: center;
      margin-bottom: 18px;
    }
    .tabs {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
    }
    button, select {
      border: 1px solid var(--border);
      background: rgba(255,255,255,0.85);
      color: var(--ink);
      border-radius: 999px;
      padding: 10px 14px;
      font: inherit;
      cursor: pointer;
    }
    button.active {
      background: var(--ink);
      color: white;
    }
    .grid {
      display: grid;
      gap: 14px;
      grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
      margin-bottom: 18px;
    }
    .card {
      border: 1px solid var(--border);
      border-radius: 20px;
      background: var(--panel);
      box-shadow: var(--shadow);
      padding: 18px;
      backdrop-filter: blur(12px);
    }
    .label {
      color: var(--muted);
      font-size: 0.82rem;
      text-transform: uppercase;
      letter-spacing: 0.08em;
    }
    .value {
      margin-top: 8px;
      font-size: 2rem;
      font-weight: 700;
      letter-spacing: -0.03em;
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
      padding: 12px 10px;
      border-bottom: 1px solid rgba(31, 31, 27, 0.08);
      text-align: left;
      font-size: 0.95rem;
    }
    th { color: var(--muted); font-weight: 600; }
    .pill {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      padding: 6px 10px;
      border-radius: 999px;
      background: var(--accent-soft);
      color: var(--accent);
      font-size: 0.85rem;
      font-weight: 600;
    }
    .pill.high { background: #fee4e2; color: var(--danger); }
    .pill.medium { background: #fff3db; color: var(--warning); }
    .muted { color: var(--muted); }
    .spark {
      display: inline-block;
      min-width: 120px;
      color: var(--muted);
      font-size: 0.85rem;
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
      font-size: 0.82rem;
      color: var(--muted);
    }
    @media (max-width: 640px) {
      .shell { padding: 16px 12px 28px; }
      .hero { padding: 18px; }
      .value { font-size: 1.55rem; }
    }
  </style>
</head>
<body>
  <div class="shell">
    <section class="hero">
      <div>
        <h1>JucoBot Operations Console</h1>
        <p>전체 상태 요약과 방별 분석을 한 화면 흐름으로 연결한 읽기 전용 운영 콘솔입니다. 모든 수치는 가명화된 메트릭 저장소 기준으로 집계됩니다.</p>
      </div>
    </section>

    <div class="controls">
      <div class="tabs">
        <button class="tab active" data-target="home">Home</button>
        <button class="tab" data-target="rooms">Rooms</button>
        <button class="tab" data-target="room-detail">Room Detail</button>
        <button class="tab" data-target="reliability">Reliability</button>
        <button class="tab" data-target="features">Feature Ops</button>
        <button class="tab" data-target="product">Product</button>
      </div>
      <label class="muted">Window
        <select id="window">
          <option value="24h">24h</option>
          <option value="168h" selected>7d</option>
          <option value="720h">30d</option>
        </select>
      </label>
    </div>

    <section id="home" class="section active">
      <div id="overview-cards" class="grid"></div>
      <div class="card">
        <div class="label">Anomalies</div>
        <div id="anomalies"></div>
      </div>
    </section>

    <section id="rooms" class="section">
      <div class="card table-wrap">
        <div class="label">Room Rankings</div>
        <table>
          <thead>
            <tr><th>방</th><th>요청 수</th><th>활성 사용자</th><th>에러율</th><th>7일 추세</th></tr>
          </thead>
          <tbody id="rooms-body"></tbody>
        </table>
      </div>
    </section>

    <section id="room-detail" class="section">
      <div class="grid" id="room-detail-cards"></div>
      <div class="card table-wrap">
        <div class="label">Command Mix</div>
        <table>
          <thead><tr><th>명령</th><th>요청 수</th><th>실패 수</th></tr></thead>
          <tbody id="room-commands"></tbody>
        </table>
      </div>
      <div class="card table-wrap">
        <div class="label">Recent Issues</div>
        <table>
          <thead><tr><th>시각</th><th>이벤트</th><th>명령</th><th>세부</th></tr></thead>
          <tbody id="room-issues"></tbody>
        </table>
      </div>
    </section>

    <section id="reliability" class="section">
      <div id="reliability-cards" class="grid"></div>
      <div class="card table-wrap">
        <div class="label">Error Breakdown</div>
        <table>
          <thead><tr><th>오류 분류</th><th>건수</th></tr></thead>
          <tbody id="reliability-errors"></tbody>
        </table>
      </div>
    </section>

    <section id="features" class="section">
      <div id="feature-cards" class="grid"></div>
      <div class="card table-wrap">
        <div class="label">Feature Usage</div>
        <table>
          <thead><tr><th>기능</th><th>요청 수</th><th>실패 수</th></tr></thead>
          <tbody id="feature-usage"></tbody>
        </table>
      </div>
      <div class="card table-wrap">
        <div class="label">Coupang Chart Skip Reasons</div>
        <table>
          <thead><tr><th>Reason</th><th>건수</th></tr></thead>
          <tbody id="feature-chart-reasons"></tbody>
        </table>
      </div>
    </section>

    <section id="product" class="section">
      <div id="product-funnel-cards" class="grid"></div>
      <div class="card table-wrap">
        <div class="label">Cohorts</div>
        <table>
          <thead><tr><th>Cohort Date</th><th>Activation Users</th></tr></thead>
          <tbody id="product-cohorts"></tbody>
        </table>
      </div>
      <div class="card table-wrap">
        <div class="label">Retention</div>
        <table>
          <thead><tr><th>Cohort</th><th>Bucket</th><th>Cohort Size</th><th>Retained</th><th>Rate</th></tr></thead>
          <tbody id="product-retention"></tbody>
        </table>
      </div>
    </section>
  </div>
  <script>
    const state = { room: "", me: null, tabs: ["home", "rooms", "room-detail", "reliability", "features", "product"] };
    const $ = (selector) => document.querySelector(selector);
    const fmtInt = (value) => Number(value || 0).toLocaleString("ko-KR");
    const fmtPct = (value) => ((value || 0) * 100).toFixed(1) + "%";
    const roomName = (room) => room.room_label || room.room_name_snapshot || room.room_id_hash || "-";
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
    const issueDetail = (item) => {
      const pre = el("pre");
      pre.textContent = item.detail || item.error_class || "-";
      return pre;
    };
    const endpoint = (path, params = {}) => {
      const url = new URL(path, window.location.origin);
      Object.entries(params).forEach(([key, value]) => {
        if (value !== undefined && value !== null && value !== "") url.searchParams.set(key, value);
      });
      return url.toString();
    };

    async function loadOverview() {
      const data = await fetchJSON(endpoint("./api/overview", { window: windowValue(), room: state.room }));
      replaceChildren("#overview-cards", [
        cardNode("오늘 요청 수", fmtInt(data.requests_today)),
        cardNode("에러율", fmtPct(data.error_rate)),
        cardNode("p95 응답시간", fmtInt(data.p95_latency_ms) + " ms"),
        cardNode("활성 방 수", fmtInt(data.active_rooms)),
        cardNode("활성 사용자 수", fmtInt(data.active_users)),
      ]);
      const anomalies = (data.anomalies || []).map(item => {
        const wrap = el("p");
        wrap.append(el("span", { className: "pill " + (item.severity || ""), text: item.severity || "info" }));
        wrap.append(document.createTextNode(" "));
        const strong = el("strong", { text: item.title || "-" });
        wrap.append(strong);
        wrap.append(el("br"));
        wrap.append(el("span", { className: "muted", text: item.detail || "-" }));
        return wrap;
      });
      replaceChildren("#anomalies", anomalies.length ? anomalies : [el("p", { className: "muted", text: "최근 구간에서 눈에 띄는 이상 징후가 없습니다." })]);
    }

    async function loadRooms() {
      const data = await fetchJSON(endpoint("./api/rooms", { window: windowValue(), limit: 50, room: state.room }));
      const rooms = data.rooms || [];
      const normalizedRows = rooms.map(room => {
        const tr = document.createElement("tr");
        const roomCell = el("td");
        const link = el("a", {
          className: "room-link",
          text: roomName(room),
          attrs: { href: "#", "data-room": room.room_id_hash || "" },
        });
        link.addEventListener("click", (event) => {
          event.preventDefault();
          state.room = link.getAttribute("data-room");
          switchTab("room-detail");
          loadRoomDetail();
        });
        roomCell.append(link);
        if (room.room_name_snapshot) {
          roomCell.append(el("div", { className: "muted", text: room.room_name_snapshot }));
        }
        tr.append(
          roomCell,
          cell(fmtInt(room.requests)),
          cell(fmtInt(room.active_users)),
          cell(fmtPct(room.error_rate)),
          cell(el("span", { className: "spark", text: spark(room.trend) })),
        );
        return tr;
      });
      replaceChildren("#rooms-body", normalizedRows.length ? normalizedRows : [emptyRow(5, "데이터가 없습니다.")]);
    }

    async function loadRoomDetail() {
      if (!state.room) {
        const card = el("div", { className: "card" });
        card.append(el("div", { className: "label", text: "Room Detail" }));
        card.append(el("p", { className: "muted", text: "Rooms 탭에서 방을 선택하세요." }));
        replaceChildren("#room-detail-cards", [card]);
        replaceChildren("#room-commands", []);
        replaceChildren("#room-issues", []);
        return;
      }
      const data = await fetchJSON(endpoint("./api/room", { window: windowValue(), room: state.room }));
      replaceChildren("#room-detail-cards", [
        cardNode("방", roomName(data)),
        cardNode("요청 수", fmtInt(data.requests)),
        cardNode("활성 사용자", fmtInt(data.active_users)),
        cardNode("에러율", fmtPct(data.error_rate)),
        cardNode("Explicit", fmtInt(data.explicit_count)),
        cardNode("Auto", fmtInt(data.auto_count)),
      ]);
      const commandRows = (data.commands || []).map(item => {
        const tr = document.createElement("tr");
        tr.append(cell(item.command_id || "-"), cell(fmtInt(item.requests)), cell(fmtInt(item.errors)));
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
      const data = await fetchJSON(endpoint("./api/reliability", { window: windowValue(), room: state.room }));
      replaceChildren("#reliability-cards", [
        cardNode("완료 명령 수", fmtInt(data.total_commands)),
        cardNode("실패 명령 수", fmtInt(data.failed_commands)),
        cardNode("에러율", fmtPct(data.error_rate)),
        cardNode("p95 응답시간", fmtInt(data.p95_latency_ms) + " ms"),
        cardNode("Rate Limit", fmtInt(data.rate_limited_count)),
        cardNode("ACL Deny", fmtInt(data.access_denied_count)),
        cardNode("Reply Failed", fmtInt(data.reply_failed_count)),
        cardNode("Ingress Overload", fmtInt(data.overload_count)),
      ]);
      const rows = (data.errors_by_class || []).map(item => {
        const tr = document.createElement("tr");
        tr.append(cell(item.error_class || "-"), cell(fmtInt(item.count)));
        return tr;
      });
      replaceChildren("#reliability-errors", rows.length ? rows : [emptyRow(2, "오류 데이터가 없습니다.")]);
    }

    async function loadFeatures() {
      const data = await fetchJSON(endpoint("./api/features", { window: windowValue(), room: state.room }));
      replaceChildren("#feature-cards", [
        cardNode("Tracked Products", fmtInt(data.coupang?.tracked_products)),
        cardNode("Stale Products", fmtInt(data.coupang?.stale_products)),
        cardNode("Stale Ratio", fmtPct(data.coupang?.stale_ratio)),
        cardNode("Refresh Success", fmtInt(data.coupang?.refresh_success_count)),
        cardNode("Refresh Failed", fmtInt(data.coupang?.refresh_failure_count)),
        cardNode("Partial Ratio", fmtPct(data.coupang?.partial_ratio)),
        cardNode("Join Timeout Ratio", fmtPct(data.coupang?.join_timeout_ratio)),
        cardNode("Chart Skipped", fmtInt(data.coupang?.chart_skipped_count)),
      ]);
      const usageRows = (data.feature_usage || []).map(item => {
        const tr = document.createElement("tr");
        tr.append(cell(item.command_id || "-"), cell(fmtInt(item.requests)), cell(fmtInt(item.errors)));
        return tr;
      });
      replaceChildren("#feature-usage", usageRows.length ? usageRows : [emptyRow(3, "기능 사용량 데이터가 없습니다.")]);
      const reasonRows = (data.coupang?.chart_skip_reasons || []).map(item => {
        const tr = document.createElement("tr");
        tr.append(cell(item.error_class || "-"), cell(fmtInt(item.count)));
        return tr;
      });
      replaceChildren("#feature-chart-reasons", reasonRows.length ? reasonRows : [emptyRow(2, "차트 스킵 reason 데이터가 없습니다.")]);
    }

    async function loadProduct() {
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
        emptyCard.append(el("div", { className: "label", text: "Funnel" }));
        emptyCard.append(el("p", { className: "muted", text: "데이터가 없습니다." }));
        funnelCards.push(emptyCard);
      }
      replaceChildren("#product-funnel-cards", funnelCards);
      const cohortRows = (cohortData.cohorts || []).map(item => {
        const tr = document.createElement("tr");
        tr.append(cell(item.cohort_date || "-"), cell(fmtInt(item.activation_users)));
        return tr;
      });
      replaceChildren("#product-cohorts", cohortRows.length ? cohortRows : [emptyRow(2, "코호트 데이터가 없습니다.")]);
      const retentionRows = (retentionData.retention || []).map(item => {
        const tr = document.createElement("tr");
        tr.append(
          cell(item.cohort_date || "-"),
          cell(item.bucket_date || "-"),
          cell(fmtInt(item.cohort_size)),
          cell(fmtInt(item.retained_users)),
          cell(fmtPct(item.retention_rate)),
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
        return ["home", "rooms", "room-detail", "reliability", "features", "product"];
      }
      if (role === "partner" || role === "customer") {
        return ["home", "rooms", "room-detail", "product"];
      }
      return ["home"];
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
        switchTab(state.tabs[0] || "home");
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
      const jobs = [loadOverview(), loadRooms(), loadRoomDetail(), loadProduct()];
      if (state.tabs.includes("reliability")) {
        jobs.push(loadReliability());
      }
      if (state.tabs.includes("features")) {
        jobs.push(loadFeatures());
      }
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
