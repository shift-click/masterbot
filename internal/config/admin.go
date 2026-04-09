package config

import (
	"fmt"
	"strings"
	"time"
)

type AdminAudienceScope struct {
	Email    string
	Role     string
	Tenants  []string
	Rooms    []string
	Features []string
}

type AdminConfig struct {
	Enabled    bool
	ListenAddr string
	BasePath   string
	// SmokeRoomChatID is the raw chat id used by the admin command smoke
	// runner. When unset, the runner falls back to a synthetic identifier
	// (admin-smoke://synthetic) so smoke probes never share a room_id_hash
	// with operational traffic. Operators that want smoke probes to actually
	// pass ACL checks should register a dedicated test room and set this
	// field to its chat id explicitly.
	SmokeRoomChatID   string
	MetricsEnabled    bool
	MetricsDBPath     string
	PseudonymSecret   string
	FlushInterval     time.Duration
	RollupInterval    time.Duration
	RawRetention      time.Duration
	HourlyRetention   time.Duration
	DailyRetention    time.Duration
	ErrorRetention    time.Duration
	AuthEmailHeader   string
	AllowedEmails     []string
	AudienceScopes    []AdminAudienceScope
	TrustedProxyCIDRs []string
}

type rawAdminConfig struct {
	Enabled           bool   `koanf:"enabled"`
	ListenAddr        string `koanf:"listen_addr"`
	BasePath          string `koanf:"base_path"`
	SmokeRoomChatID   string `koanf:"smoke_room_chat_id"`
	MetricsEnabled    bool   `koanf:"metrics_enabled"`
	MetricsDBPath     string `koanf:"metrics_db_path"`
	PseudonymSecret   string `koanf:"pseudonym_secret"`
	FlushInterval     string `koanf:"flush_interval"`
	RollupInterval    string `koanf:"rollup_interval"`
	RawRetention      string `koanf:"raw_retention"`
	HourlyRetention   string `koanf:"hourly_retention"`
	DailyRetention    string `koanf:"daily_retention"`
	ErrorRetention    string `koanf:"error_retention"`
	AuthEmailHeader   string `koanf:"auth_email_header"`
	AllowedEmails     string `koanf:"allowed_emails"`
	AudienceScopes    string `koanf:"audience_scopes"`
	TrustedProxyCIDRs string `koanf:"trusted_proxy_cidrs"`
}

func defaultAdminConfig() AdminConfig {
	return AdminConfig{
		Enabled:           false,
		ListenAddr:        "127.0.0.1:9090",
		BasePath:          "/admin",
		SmokeRoomChatID:   "",
		MetricsEnabled:    false,
		MetricsDBPath:     "data/admin-metrics.db",
		PseudonymSecret:   "",
		FlushInterval:     2 * time.Second,
		RollupInterval:    5 * time.Minute,
		RawRetention:      90 * 24 * time.Hour,
		HourlyRetention:   90 * 24 * time.Hour,
		DailyRetention:    395 * 24 * time.Hour,
		ErrorRetention:    180 * 24 * time.Hour,
		AuthEmailHeader:   "X-Auth-Request-Email",
		AllowedEmails:     nil,
		AudienceScopes:    nil,
		TrustedProxyCIDRs: []string{"127.0.0.1/32", "::1/128"},
	}
}

func defaultRawAdminConfig() rawAdminConfig {
	cfg := defaultAdminConfig()
	return rawAdminConfig{
		Enabled:           cfg.Enabled,
		ListenAddr:        cfg.ListenAddr,
		BasePath:          cfg.BasePath,
		SmokeRoomChatID:   cfg.SmokeRoomChatID,
		MetricsEnabled:    cfg.MetricsEnabled,
		MetricsDBPath:     cfg.MetricsDBPath,
		PseudonymSecret:   cfg.PseudonymSecret,
		FlushInterval:     cfg.FlushInterval.String(),
		RollupInterval:    cfg.RollupInterval.String(),
		RawRetention:      cfg.RawRetention.String(),
		HourlyRetention:   cfg.HourlyRetention.String(),
		DailyRetention:    cfg.DailyRetention.String(),
		ErrorRetention:    cfg.ErrorRetention.String(),
		AuthEmailHeader:   cfg.AuthEmailHeader,
		AllowedEmails:     strings.Join(cfg.AllowedEmails, ","),
		AudienceScopes:    "",
		TrustedProxyCIDRs: strings.Join(cfg.TrustedProxyCIDRs, ","),
	}
}

func (r rawAdminConfig) materialize() (AdminConfig, error) {
	flushInterval, err := time.ParseDuration(r.FlushInterval)
	if err != nil {
		return AdminConfig{}, fmt.Errorf("parse admin.flush_interval: %w", err)
	}
	rollupInterval, err := time.ParseDuration(r.RollupInterval)
	if err != nil {
		return AdminConfig{}, fmt.Errorf("parse admin.rollup_interval: %w", err)
	}
	rawRetention, err := time.ParseDuration(r.RawRetention)
	if err != nil {
		return AdminConfig{}, fmt.Errorf("parse admin.raw_retention: %w", err)
	}
	hourlyRetention, err := time.ParseDuration(r.HourlyRetention)
	if err != nil {
		return AdminConfig{}, fmt.Errorf("parse admin.hourly_retention: %w", err)
	}
	dailyRetention, err := time.ParseDuration(r.DailyRetention)
	if err != nil {
		return AdminConfig{}, fmt.Errorf("parse admin.daily_retention: %w", err)
	}
	errorRetention, err := time.ParseDuration(r.ErrorRetention)
	if err != nil {
		return AdminConfig{}, fmt.Errorf("parse admin.error_retention: %w", err)
	}
	audienceScopes, err := parseAudienceScopes(r.AudienceScopes)
	if err != nil {
		return AdminConfig{}, fmt.Errorf("parse admin.audience_scopes: %w", err)
	}

	return AdminConfig{
		Enabled:           r.Enabled,
		ListenAddr:        strings.TrimSpace(r.ListenAddr),
		BasePath:          normalizeBasePath(r.BasePath),
		SmokeRoomChatID:   strings.TrimSpace(r.SmokeRoomChatID),
		MetricsEnabled:    r.MetricsEnabled,
		MetricsDBPath:     strings.TrimSpace(r.MetricsDBPath),
		PseudonymSecret:   strings.TrimSpace(r.PseudonymSecret),
		FlushInterval:     flushInterval,
		RollupInterval:    rollupInterval,
		RawRetention:      rawRetention,
		HourlyRetention:   hourlyRetention,
		DailyRetention:    dailyRetention,
		ErrorRetention:    errorRetention,
		AuthEmailHeader:   strings.TrimSpace(r.AuthEmailHeader),
		AllowedEmails:     splitCommaList(r.AllowedEmails),
		AudienceScopes:    audienceScopes,
		TrustedProxyCIDRs: splitCommaList(r.TrustedProxyCIDRs),
	}, nil
}

func (c AdminConfig) validate(problems *[]string) {
	c.validateAdminMetricsSettings(problems)
	c.validateAdminRuntimeSettings(problems)
	c.validateAdminAudienceScopes(problems)
}

func (c AdminConfig) validateAdminMetricsSettings(problems *[]string) {
	if !c.MetricsEnabled {
		return
	}
	if strings.TrimSpace(c.MetricsDBPath) == "" {
		*problems = append(*problems, "admin.metrics_db_path is required when admin.metrics_enabled=true")
	}
	if strings.TrimSpace(c.PseudonymSecret) == "" {
		*problems = append(*problems, "admin.pseudonym_secret is required when admin.metrics_enabled=true")
	}
	if c.FlushInterval <= 0 {
		*problems = append(*problems, "admin.flush_interval must be greater than zero")
	}
	if c.RollupInterval <= 0 {
		*problems = append(*problems, "admin.rollup_interval must be greater than zero")
	}
	if c.RawRetention <= 0 || c.HourlyRetention <= 0 || c.DailyRetention <= 0 || c.ErrorRetention <= 0 {
		*problems = append(*problems, "admin retention durations must be greater than zero")
	}
}

func (c AdminConfig) validateAdminRuntimeSettings(problems *[]string) {
	if !c.Enabled {
		return
	}
	if !c.MetricsEnabled {
		*problems = append(*problems, "admin.metrics_enabled must be true when admin.enabled=true")
	}
	if strings.TrimSpace(c.ListenAddr) == "" {
		*problems = append(*problems, "admin.listen_addr must not be empty when admin.enabled=true")
	}
	if strings.TrimSpace(c.BasePath) == "" {
		*problems = append(*problems, "admin.base_path must not be empty when admin.enabled=true")
	}
	if strings.TrimSpace(c.AuthEmailHeader) == "" {
		*problems = append(*problems, "admin.auth_email_header must not be empty when admin.enabled=true")
	}
	if len(c.AllowedEmails) == 0 && len(c.AudienceScopes) == 0 {
		*problems = append(*problems, "admin.allowed_emails or admin.audience_scopes must not be empty when admin.enabled=true")
	}
	if len(c.TrustedProxyCIDRs) == 0 {
		*problems = append(*problems, "admin.trusted_proxy_cidrs must not be empty when admin.enabled=true")
	}
}

func (c AdminConfig) validateAdminAudienceScopes(problems *[]string) {
	seen := make(map[string]struct{}, len(c.AudienceScopes))
	for _, scope := range c.AudienceScopes {
		email := strings.ToLower(strings.TrimSpace(scope.Email))
		if email == "" {
			*problems = append(*problems, "admin.audience_scopes email must not be empty")
			continue
		}
		if _, exists := seen[email]; exists {
			*problems = append(*problems, fmt.Sprintf("admin.audience_scopes duplicate email: %s", email))
		}
		seen[email] = struct{}{}
		role := strings.ToLower(strings.TrimSpace(scope.Role))
		if role != "operator" && role != "partner" && role != "customer" {
			*problems = append(*problems, fmt.Sprintf("admin.audience_scopes invalid role for %s: %s", email, scope.Role))
		}
	}
}

func splitCommaList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func normalizeBasePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/admin"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.TrimRight(path, "/")
}

func parseAudienceScopes(value string) ([]AdminAudienceScope, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	entries := splitCommaList(value)
	out := make([]AdminAudienceScope, 0, len(entries))
	for _, entry := range entries {
		parts := strings.Split(entry, "|")
		if len(parts) != 5 {
			return nil, fmt.Errorf("invalid audience scope %q (expected email|role|tenants|rooms|features)", entry)
		}
		email := strings.ToLower(strings.TrimSpace(parts[0]))
		role := strings.ToLower(strings.TrimSpace(parts[1]))
		if email == "" || role == "" {
			return nil, fmt.Errorf("invalid audience scope %q: email and role are required", entry)
		}
		out = append(out, AdminAudienceScope{
			Email:    email,
			Role:     role,
			Tenants:  splitPipeList(parts[2]),
			Rooms:    splitPipeList(parts[3]),
			Features: splitPipeList(parts[4]),
		})
	}
	return out, nil
}

func splitPipeList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}
