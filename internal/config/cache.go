package config

import (
	"fmt"
	"strings"
	"time"
)

type CacheConfig struct {
	Driver          string
	DefaultTTL      time.Duration
	StaleTTL        time.Duration
	CleanupInterval time.Duration
	RedisAddr       string
	RedisPassword   string
	RedisDB         int
}

type rawCacheConfig struct {
	Driver          string `koanf:"driver"`
	DefaultTTL      string `koanf:"default_ttl"`
	StaleTTL        string `koanf:"stale_ttl"`
	CleanupInterval string `koanf:"cleanup_interval"`
	RedisAddr       string `koanf:"redis_addr"`
	RedisPassword   string `koanf:"redis_password"`
	RedisDB         int    `koanf:"redis_db"`
}

func defaultCacheConfig() CacheConfig {
	return CacheConfig{
		Driver:          "memory",
		DefaultTTL:      5 * time.Minute,
		StaleTTL:        1 * time.Hour,
		CleanupInterval: 1 * time.Minute,
	}
}

func defaultRawCacheConfig() rawCacheConfig {
	cfg := defaultCacheConfig()
	return rawCacheConfig{
		Driver:          cfg.Driver,
		DefaultTTL:      cfg.DefaultTTL.String(),
		StaleTTL:        cfg.StaleTTL.String(),
		CleanupInterval: cfg.CleanupInterval.String(),
		RedisAddr:       cfg.RedisAddr,
		RedisPassword:   cfg.RedisPassword,
		RedisDB:         cfg.RedisDB,
	}
}

func (r rawCacheConfig) materialize() (CacheConfig, error) {
	defaultTTL, err := time.ParseDuration(r.DefaultTTL)
	if err != nil {
		return CacheConfig{}, fmt.Errorf("parse cache.default_ttl: %w", err)
	}
	staleTTL, err := time.ParseDuration(r.StaleTTL)
	if err != nil {
		return CacheConfig{}, fmt.Errorf("parse cache.stale_ttl: %w", err)
	}
	cleanupInterval, err := time.ParseDuration(r.CleanupInterval)
	if err != nil {
		return CacheConfig{}, fmt.Errorf("parse cache.cleanup_interval: %w", err)
	}

	return CacheConfig{
		Driver:          r.Driver,
		DefaultTTL:      defaultTTL,
		StaleTTL:        staleTTL,
		CleanupInterval: cleanupInterval,
		RedisAddr:       r.RedisAddr,
		RedisPassword:   r.RedisPassword,
		RedisDB:         r.RedisDB,
	}, nil
}

func (c CacheConfig) validate(problems *[]string) {
	if c.DefaultTTL <= 0 {
		*problems = append(*problems, "cache.default_ttl must be greater than zero")
	}
	if c.StaleTTL < c.DefaultTTL {
		*problems = append(*problems, "cache.stale_ttl must be greater than or equal to cache.default_ttl")
	}

	switch strings.TrimSpace(strings.ToLower(c.Driver)) {
	case "memory", "redis":
	default:
		*problems = append(*problems, "cache.driver must be one of: memory, redis")
	}

	if strings.EqualFold(strings.TrimSpace(c.Driver), "redis") && strings.TrimSpace(c.RedisAddr) == "" {
		*problems = append(*problems, "cache.redis_addr is required when cache.driver=redis")
	}
}
