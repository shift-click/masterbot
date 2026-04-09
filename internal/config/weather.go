package config

import (
	"fmt"
	"time"
)

// WeatherConfig holds configuration for the weather feature.
type WeatherConfig struct {
	CacheTTL          time.Duration // TTL for forecast/air quality cache (default 15m)
	YesterdayCacheTTL time.Duration // TTL for yesterday temperature cache (default 1h)
}

type rawWeatherConfig struct {
	CacheTTL          string `koanf:"cache_ttl"`
	YesterdayCacheTTL string `koanf:"yesterday_cache_ttl"`
}

func defaultWeatherConfig() WeatherConfig {
	return WeatherConfig{
		CacheTTL:          15 * time.Minute,
		YesterdayCacheTTL: 1 * time.Hour,
	}
}

func defaultRawWeatherConfig() rawWeatherConfig {
	cfg := defaultWeatherConfig()
	return rawWeatherConfig{
		CacheTTL:          cfg.CacheTTL.String(),
		YesterdayCacheTTL: cfg.YesterdayCacheTTL.String(),
	}
}

func (r rawWeatherConfig) materialize() (WeatherConfig, error) {
	cacheTTL, err := time.ParseDuration(r.CacheTTL)
	if err != nil {
		return WeatherConfig{}, fmt.Errorf("parse weather.cache_ttl: %w", err)
	}
	yesterdayTTL, err := time.ParseDuration(r.YesterdayCacheTTL)
	if err != nil {
		return WeatherConfig{}, fmt.Errorf("parse weather.yesterday_cache_ttl: %w", err)
	}
	return WeatherConfig{
		CacheTTL:          cacheTTL,
		YesterdayCacheTTL: yesterdayTTL,
	}, nil
}

func (c WeatherConfig) validate(problems *[]string) {
	if c.CacheTTL <= 0 {
		*problems = append(*problems, "weather.cache_ttl must be greater than zero")
	}
	if c.YesterdayCacheTTL <= 0 {
		*problems = append(*problems, "weather.yesterday_cache_ttl must be greater than zero")
	}
}
