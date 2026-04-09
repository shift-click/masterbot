package config

import (
	"fmt"
	"time"
)

type HTTPTestConfig struct {
	Enabled      bool
	Addr         string
	ReplyTimeout time.Duration
}

type rawHTTPTestConfig struct {
	Enabled      bool   `koanf:"enabled"`
	Addr         string `koanf:"addr"`
	ReplyTimeout string `koanf:"reply_timeout"`
}

func defaultHTTPTestConfig() HTTPTestConfig {
	return HTTPTestConfig{
		Enabled:      false,
		Addr:         ":18080",
		ReplyTimeout: 10 * time.Second,
	}
}

func defaultRawHTTPTestConfig() rawHTTPTestConfig {
	cfg := defaultHTTPTestConfig()
	return rawHTTPTestConfig{
		Enabled:      cfg.Enabled,
		Addr:         cfg.Addr,
		ReplyTimeout: cfg.ReplyTimeout.String(),
	}
}

func (r rawHTTPTestConfig) materialize() (HTTPTestConfig, error) {
	replyTimeout, err := time.ParseDuration(r.ReplyTimeout)
	if err != nil {
		return HTTPTestConfig{}, fmt.Errorf("parse http_test.reply_timeout: %w", err)
	}

	return HTTPTestConfig{
		Enabled:      r.Enabled,
		Addr:         r.Addr,
		ReplyTimeout: replyTimeout,
	}, nil
}

func (c HTTPTestConfig) validate(problems *[]string) {
	if !c.Enabled {
		return
	}
	if c.Addr == "" {
		*problems = append(*problems, "http_test.addr must not be empty")
	}
	if c.ReplyTimeout <= 0 {
		*problems = append(*problems, "http_test.reply_timeout must be greater than zero")
	}
}
