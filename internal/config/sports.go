package config

import (
	"fmt"
	"time"
)

// SportsConfig holds configuration for sports/esports data providers.
type SportsConfig struct {
	LivePollInterval   time.Duration // polling interval during live matches (default 30s)
	MatchDayInterval   time.Duration // polling interval on match days, pre-game (default 1h)
	IdleDayInterval    time.Duration // polling interval on days without matches (default 6h)
	PreMatchLeadTime   time.Duration // how early to switch to live polling before match start (default 30m)
	OddsCacheTTL       time.Duration // TTL for odds cache (default 4h)
	EventFetchDelay    time.Duration // delay after score change before fetching events (default 90s)
	APIFootballKey     string        // RapidAPI key for API-Football
	APIFootballBudget  int           // daily request budget for API-Football (default 100)
	OddsAPIKey         string        // API key for The Odds API
}

type rawSportsConfig struct {
	LivePollInterval  string `koanf:"live_poll_interval"`
	MatchDayInterval  string `koanf:"match_day_interval"`
	IdleDayInterval   string `koanf:"idle_day_interval"`
	PreMatchLeadTime  string `koanf:"pre_match_lead_time"`
	OddsCacheTTL      string `koanf:"odds_cache_ttl"`
	EventFetchDelay   string `koanf:"event_fetch_delay"`
	APIFootballKey    string `koanf:"api_football_key"`
	APIFootballBudget int    `koanf:"api_football_budget"`
	OddsAPIKey        string `koanf:"odds_api_key"`
}

func defaultSportsConfig() SportsConfig {
	return SportsConfig{
		LivePollInterval:  30 * time.Second,
		MatchDayInterval:  1 * time.Hour,
		IdleDayInterval:   6 * time.Hour,
		PreMatchLeadTime:  30 * time.Minute,
		OddsCacheTTL:      4 * time.Hour,
		EventFetchDelay:   90 * time.Second,
		APIFootballBudget: 100,
	}
}

func defaultRawSportsConfig() rawSportsConfig {
	cfg := defaultSportsConfig()
	return rawSportsConfig{
		LivePollInterval:  cfg.LivePollInterval.String(),
		MatchDayInterval:  cfg.MatchDayInterval.String(),
		IdleDayInterval:   cfg.IdleDayInterval.String(),
		PreMatchLeadTime:  cfg.PreMatchLeadTime.String(),
		OddsCacheTTL:      cfg.OddsCacheTTL.String(),
		EventFetchDelay:   cfg.EventFetchDelay.String(),
		APIFootballBudget: cfg.APIFootballBudget,
	}
}

func (r rawSportsConfig) materialize() (SportsConfig, error) {
	livePoll, err := time.ParseDuration(r.LivePollInterval)
	if err != nil {
		return SportsConfig{}, fmt.Errorf("parse sports.live_poll_interval: %w", err)
	}
	matchDay, err := time.ParseDuration(r.MatchDayInterval)
	if err != nil {
		return SportsConfig{}, fmt.Errorf("parse sports.match_day_interval: %w", err)
	}
	idleDay, err := time.ParseDuration(r.IdleDayInterval)
	if err != nil {
		return SportsConfig{}, fmt.Errorf("parse sports.idle_day_interval: %w", err)
	}
	preLead, err := time.ParseDuration(r.PreMatchLeadTime)
	if err != nil {
		return SportsConfig{}, fmt.Errorf("parse sports.pre_match_lead_time: %w", err)
	}
	oddsTTL, err := time.ParseDuration(r.OddsCacheTTL)
	if err != nil {
		return SportsConfig{}, fmt.Errorf("parse sports.odds_cache_ttl: %w", err)
	}
	eventDelay, err := time.ParseDuration(r.EventFetchDelay)
	if err != nil {
		return SportsConfig{}, fmt.Errorf("parse sports.event_fetch_delay: %w", err)
	}

	return SportsConfig{
		LivePollInterval:  livePoll,
		MatchDayInterval:  matchDay,
		IdleDayInterval:   idleDay,
		PreMatchLeadTime:  preLead,
		OddsCacheTTL:      oddsTTL,
		EventFetchDelay:   eventDelay,
		APIFootballKey:    r.APIFootballKey,
		APIFootballBudget: r.APIFootballBudget,
		OddsAPIKey:        r.OddsAPIKey,
	}, nil
}

func (c SportsConfig) validate(problems *[]string) {
	if c.LivePollInterval <= 0 {
		*problems = append(*problems, "sports.live_poll_interval must be greater than zero")
	}
	if c.MatchDayInterval <= 0 {
		*problems = append(*problems, "sports.match_day_interval must be greater than zero")
	}
	if c.IdleDayInterval <= 0 {
		*problems = append(*problems, "sports.idle_day_interval must be greater than zero")
	}
	if c.APIFootballBudget <= 0 {
		*problems = append(*problems, "sports.api_football_budget must be greater than zero")
	}
}
