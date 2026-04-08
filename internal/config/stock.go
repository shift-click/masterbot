package config

import (
	"fmt"
	"time"
)

type StockConfig struct {
	PollInterval    time.Duration
	IdleTimeout     time.Duration
	OffHourInterval time.Duration
	MarketOpen      int
	MarketClose     int
}

type rawStockConfig struct {
	PollInterval    string `koanf:"poll_interval"`
	IdleTimeout     string `koanf:"idle_timeout"`
	OffHourInterval string `koanf:"off_hour_interval"`
	MarketOpen      int    `koanf:"market_open"`
	MarketClose     int    `koanf:"market_close"`
}

func defaultStockConfig() StockConfig {
	return StockConfig{
		PollInterval:    2 * time.Second,
		IdleTimeout:     10 * time.Minute,
		OffHourInterval: 60 * time.Second,
		MarketOpen:      9,
		MarketClose:     16,
	}
}

func defaultRawStockConfig() rawStockConfig {
	cfg := defaultStockConfig()
	return rawStockConfig{
		PollInterval:    cfg.PollInterval.String(),
		IdleTimeout:     cfg.IdleTimeout.String(),
		OffHourInterval: cfg.OffHourInterval.String(),
		MarketOpen:      cfg.MarketOpen,
		MarketClose:     cfg.MarketClose,
	}
}

func (r rawStockConfig) materialize() (StockConfig, error) {
	pollInterval, err := time.ParseDuration(r.PollInterval)
	if err != nil {
		return StockConfig{}, fmt.Errorf("parse stock.poll_interval: %w", err)
	}
	idleTimeout, err := time.ParseDuration(r.IdleTimeout)
	if err != nil {
		return StockConfig{}, fmt.Errorf("parse stock.idle_timeout: %w", err)
	}
	offHourInterval, err := time.ParseDuration(r.OffHourInterval)
	if err != nil {
		return StockConfig{}, fmt.Errorf("parse stock.off_hour_interval: %w", err)
	}

	return StockConfig{
		PollInterval:    pollInterval,
		IdleTimeout:     idleTimeout,
		OffHourInterval: offHourInterval,
		MarketOpen:      r.MarketOpen,
		MarketClose:     r.MarketClose,
	}, nil
}

func (c StockConfig) validate(problems *[]string) {
	if c.PollInterval <= 0 {
		*problems = append(*problems, "stock.poll_interval must be greater than zero")
	}
	if c.IdleTimeout <= 0 {
		*problems = append(*problems, "stock.idle_timeout must be greater than zero")
	}
	if c.OffHourInterval <= 0 {
		*problems = append(*problems, "stock.off_hour_interval must be greater than zero")
	}
}
