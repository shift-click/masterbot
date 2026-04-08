package config

import (
	"fmt"
	"time"
)

type CoupangConfig struct {
	DBPath                    string
	CollectInterval           time.Duration
	IdleTimeout               time.Duration
	MaxProducts               int
	HotInterval               time.Duration
	WarmInterval              time.Duration
	ColdInterval              time.Duration
	Freshness                 time.Duration
	StaleThreshold            time.Duration
	MinRefreshInterval        time.Duration
	RefreshBudgetPerHour      int
	RegistrationBudgetPerHour int
	ResolutionBudgetPerHour   int
	TierWindow                time.Duration
	HotThreshold              int
	WarmThreshold             int
	CandidateFanout           int
	MappingRecheckBackoff     time.Duration
	AllowAuxiliaryFallback    bool
	RegistrationLatencyBudget time.Duration
	ReadRefreshTimeout        time.Duration
	LookupCoalescingEnabled   bool
	RegistrationJoinWait      time.Duration
	ReadRefreshJoinWait       time.Duration
	ChartBackfillInterval     time.Duration
	ScraperProxyURL           string
}

type rawCoupangConfig struct {
	DBPath                    string `koanf:"db_path"`
	CollectInterval           string `koanf:"collect_interval"`
	IdleTimeout               string `koanf:"idle_timeout"`
	MaxProducts               int    `koanf:"max_products"`
	HotInterval               string `koanf:"hot_interval"`
	WarmInterval              string `koanf:"warm_interval"`
	ColdInterval              string `koanf:"cold_interval"`
	Freshness                 string `koanf:"freshness"`
	StaleThreshold            string `koanf:"stale_threshold"`
	MinRefreshInterval        string `koanf:"min_refresh_interval"`
	RefreshBudgetPerHour      int    `koanf:"refresh_budget_per_hour"`
	RegistrationBudgetPerHour int    `koanf:"registration_budget_per_hour"`
	ResolutionBudgetPerHour   int    `koanf:"resolution_budget_per_hour"`
	TierWindow                string `koanf:"tier_window"`
	HotThreshold              int    `koanf:"hot_threshold"`
	WarmThreshold             int    `koanf:"warm_threshold"`
	CandidateFanout           int    `koanf:"candidate_fanout"`
	MappingRecheckBackoff     string `koanf:"mapping_recheck_backoff"`
	AllowAuxiliaryFallback    bool   `koanf:"allow_auxiliary_fallback"`
	RegistrationLatencyBudget string `koanf:"registration_latency_budget"`
	ReadRefreshTimeout        string `koanf:"read_refresh_timeout"`
	LookupCoalescingEnabled   bool   `koanf:"lookup_coalescing_enabled"`
	RegistrationJoinWait      string `koanf:"registration_join_wait"`
	ReadRefreshJoinWait       string `koanf:"read_refresh_join_wait"`
	ChartBackfillInterval     string `koanf:"chart_backfill_interval"`
	ScraperProxyURL           string `koanf:"scraper_proxy_url"`
}

func defaultCoupangConfig() CoupangConfig {
	return CoupangConfig{
		DBPath:                    "data/coupang.db",
		CollectInterval:           15 * time.Minute,
		IdleTimeout:               30 * 24 * time.Hour,
		MaxProducts:               10000,
		HotInterval:               1 * time.Hour,
		WarmInterval:              6 * time.Hour,
		ColdInterval:              24 * time.Hour,
		Freshness:                 1 * time.Hour,
		StaleThreshold:            24 * time.Hour,
		MinRefreshInterval:        30 * time.Minute,
		RefreshBudgetPerHour:      120,
		RegistrationBudgetPerHour: 30,
		ResolutionBudgetPerHour:   60,
		TierWindow:                24 * time.Hour,
		HotThreshold:              3,
		WarmThreshold:             1,
		CandidateFanout:           3,
		MappingRecheckBackoff:     6 * time.Hour,
		AllowAuxiliaryFallback:    true,
		RegistrationLatencyBudget: 2 * time.Second,
		ReadRefreshTimeout:        2 * time.Second,
		LookupCoalescingEnabled:   true,
		RegistrationJoinWait:      2 * time.Second,
		ReadRefreshJoinWait:       2 * time.Second,
		ChartBackfillInterval:     72 * time.Hour,
	}
}

func defaultRawCoupangConfig() rawCoupangConfig {
	cfg := defaultCoupangConfig()
	return rawCoupangConfig{
		DBPath:                    cfg.DBPath,
		CollectInterval:           cfg.CollectInterval.String(),
		IdleTimeout:               cfg.IdleTimeout.String(),
		MaxProducts:               cfg.MaxProducts,
		HotInterval:               cfg.HotInterval.String(),
		WarmInterval:              cfg.WarmInterval.String(),
		ColdInterval:              cfg.ColdInterval.String(),
		Freshness:                 cfg.Freshness.String(),
		StaleThreshold:            cfg.StaleThreshold.String(),
		MinRefreshInterval:        cfg.MinRefreshInterval.String(),
		RefreshBudgetPerHour:      cfg.RefreshBudgetPerHour,
		RegistrationBudgetPerHour: cfg.RegistrationBudgetPerHour,
		ResolutionBudgetPerHour:   cfg.ResolutionBudgetPerHour,
		TierWindow:                cfg.TierWindow.String(),
		HotThreshold:              cfg.HotThreshold,
		WarmThreshold:             cfg.WarmThreshold,
		CandidateFanout:           cfg.CandidateFanout,
		MappingRecheckBackoff:     cfg.MappingRecheckBackoff.String(),
		AllowAuxiliaryFallback:    cfg.AllowAuxiliaryFallback,
		RegistrationLatencyBudget: cfg.RegistrationLatencyBudget.String(),
		ReadRefreshTimeout:        cfg.ReadRefreshTimeout.String(),
		LookupCoalescingEnabled:   cfg.LookupCoalescingEnabled,
		RegistrationJoinWait:      cfg.RegistrationJoinWait.String(),
		ReadRefreshJoinWait:       cfg.ReadRefreshJoinWait.String(),
		ChartBackfillInterval:     cfg.ChartBackfillInterval.String(),
	}
}

func (r rawCoupangConfig) materialize() (CoupangConfig, error) {
	collectInterval, err := time.ParseDuration(r.CollectInterval)
	if err != nil {
		return CoupangConfig{}, fmt.Errorf("parse coupang.collect_interval: %w", err)
	}
	idleTimeout, err := time.ParseDuration(r.IdleTimeout)
	if err != nil {
		return CoupangConfig{}, fmt.Errorf("parse coupang.idle_timeout: %w", err)
	}
	hotInterval, err := time.ParseDuration(r.HotInterval)
	if err != nil {
		return CoupangConfig{}, fmt.Errorf("parse coupang.hot_interval: %w", err)
	}
	warmInterval, err := time.ParseDuration(r.WarmInterval)
	if err != nil {
		return CoupangConfig{}, fmt.Errorf("parse coupang.warm_interval: %w", err)
	}
	coldInterval, err := time.ParseDuration(r.ColdInterval)
	if err != nil {
		return CoupangConfig{}, fmt.Errorf("parse coupang.cold_interval: %w", err)
	}
	freshness, err := time.ParseDuration(r.Freshness)
	if err != nil {
		return CoupangConfig{}, fmt.Errorf("parse coupang.freshness: %w", err)
	}
	staleThreshold, err := time.ParseDuration(r.StaleThreshold)
	if err != nil {
		return CoupangConfig{}, fmt.Errorf("parse coupang.stale_threshold: %w", err)
	}
	minRefreshInterval, err := time.ParseDuration(r.MinRefreshInterval)
	if err != nil {
		return CoupangConfig{}, fmt.Errorf("parse coupang.min_refresh_interval: %w", err)
	}
	tierWindow, err := time.ParseDuration(r.TierWindow)
	if err != nil {
		return CoupangConfig{}, fmt.Errorf("parse coupang.tier_window: %w", err)
	}
	mappingRecheckBackoff, err := time.ParseDuration(r.MappingRecheckBackoff)
	if err != nil {
		return CoupangConfig{}, fmt.Errorf("parse coupang.mapping_recheck_backoff: %w", err)
	}
	registrationLatencyBudget, err := time.ParseDuration(r.RegistrationLatencyBudget)
	if err != nil {
		return CoupangConfig{}, fmt.Errorf("parse coupang.registration_latency_budget: %w", err)
	}
	readRefreshTimeout, err := time.ParseDuration(r.ReadRefreshTimeout)
	if err != nil {
		return CoupangConfig{}, fmt.Errorf("parse coupang.read_refresh_timeout: %w", err)
	}
	registrationJoinWait, err := time.ParseDuration(r.RegistrationJoinWait)
	if err != nil {
		return CoupangConfig{}, fmt.Errorf("parse coupang.registration_join_wait: %w", err)
	}
	readRefreshJoinWait, err := time.ParseDuration(r.ReadRefreshJoinWait)
	if err != nil {
		return CoupangConfig{}, fmt.Errorf("parse coupang.read_refresh_join_wait: %w", err)
	}
	chartBackfillInterval, err := time.ParseDuration(r.ChartBackfillInterval)
	if err != nil {
		return CoupangConfig{}, fmt.Errorf("parse coupang.chart_backfill_interval: %w", err)
	}

	return CoupangConfig{
		DBPath:                    r.DBPath,
		CollectInterval:           collectInterval,
		IdleTimeout:               idleTimeout,
		MaxProducts:               r.MaxProducts,
		HotInterval:               hotInterval,
		WarmInterval:              warmInterval,
		ColdInterval:              coldInterval,
		Freshness:                 freshness,
		StaleThreshold:            staleThreshold,
		MinRefreshInterval:        minRefreshInterval,
		RefreshBudgetPerHour:      r.RefreshBudgetPerHour,
		RegistrationBudgetPerHour: r.RegistrationBudgetPerHour,
		ResolutionBudgetPerHour:   r.ResolutionBudgetPerHour,
		TierWindow:                tierWindow,
		HotThreshold:              r.HotThreshold,
		WarmThreshold:             r.WarmThreshold,
		CandidateFanout:           r.CandidateFanout,
		MappingRecheckBackoff:     mappingRecheckBackoff,
		AllowAuxiliaryFallback:    r.AllowAuxiliaryFallback,
		RegistrationLatencyBudget: registrationLatencyBudget,
		ReadRefreshTimeout:        readRefreshTimeout,
		LookupCoalescingEnabled:   r.LookupCoalescingEnabled,
		RegistrationJoinWait:      registrationJoinWait,
		ReadRefreshJoinWait:       readRefreshJoinWait,
		ChartBackfillInterval:     chartBackfillInterval,
		ScraperProxyURL:           r.ScraperProxyURL,
	}, nil
}

func (c CoupangConfig) validate(problems *[]string) {
	validateCoupangPositiveValues(c, problems)
	validateCoupangConsistency(c, problems)
}

func validateCoupangPositiveValues(c CoupangConfig, problems *[]string) {
	validatePositiveDuration(problems, "coupang.collect_interval", c.CollectInterval)
	validatePositiveDuration(problems, "coupang.idle_timeout", c.IdleTimeout)
	validatePositiveInt(problems, "coupang.max_products", c.MaxProducts)
	if hasNonPositiveDuration(c.HotInterval, c.WarmInterval, c.ColdInterval) {
		*problems = append(*problems, "coupang tier intervals must be greater than zero")
	}
	validatePositiveDuration(problems, "coupang.freshness", c.Freshness)
	validatePositiveDuration(problems, "coupang.min_refresh_interval", c.MinRefreshInterval)
	validatePositiveInt(problems, "coupang.refresh_budget_per_hour", c.RefreshBudgetPerHour)
	validatePositiveInt(problems, "coupang.registration_budget_per_hour", c.RegistrationBudgetPerHour)
	validatePositiveInt(problems, "coupang.resolution_budget_per_hour", c.ResolutionBudgetPerHour)
	validatePositiveDuration(problems, "coupang.tier_window", c.TierWindow)
	validatePositiveInt(problems, "coupang.warm_threshold", c.WarmThreshold)
	validatePositiveInt(problems, "coupang.candidate_fanout", c.CandidateFanout)
	validatePositiveDuration(problems, "coupang.mapping_recheck_backoff", c.MappingRecheckBackoff)
	validatePositiveDuration(problems, "coupang.registration_latency_budget", c.RegistrationLatencyBudget)
	validatePositiveDuration(problems, "coupang.read_refresh_timeout", c.ReadRefreshTimeout)
	validatePositiveDuration(problems, "coupang.registration_join_wait", c.RegistrationJoinWait)
	validatePositiveDuration(problems, "coupang.read_refresh_join_wait", c.ReadRefreshJoinWait)
}

func hasNonPositiveDuration(values ...time.Duration) bool {
	for _, value := range values {
		if value <= 0 {
			return true
		}
	}
	return false
}

func validatePositiveDuration(problems *[]string, name string, value time.Duration) {
	if value <= 0 {
		*problems = append(*problems, fmt.Sprintf("%s must be greater than zero", name))
	}
}

func validatePositiveInt(problems *[]string, name string, value int) {
	if value <= 0 {
		*problems = append(*problems, fmt.Sprintf("%s must be greater than zero", name))
	}
}

func validateCoupangConsistency(c CoupangConfig, problems *[]string) {
	if c.StaleThreshold < c.Freshness {
		*problems = append(*problems, "coupang.stale_threshold must be greater than or equal to coupang.freshness")
	}
	if c.HotThreshold < c.WarmThreshold {
		*problems = append(*problems, "coupang.hot_threshold must be greater than or equal to coupang.warm_threshold")
	}
}
