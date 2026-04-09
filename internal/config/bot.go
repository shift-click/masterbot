package config

import (
	"fmt"
	"strings"
	"time"
)

type BotConfig struct {
	Name            string
	CommandPrefix   string
	LogLevel        string
	ShutdownTimeout time.Duration
}

type rawBotConfig struct {
	Name            string `koanf:"name"`
	CommandPrefix   string `koanf:"command_prefix"`
	LogLevel        string `koanf:"log_level"`
	ShutdownTimeout string `koanf:"shutdown_timeout"`
}

func defaultBotConfig() BotConfig {
	return BotConfig{
		Name:            "JucoBot v2",
		CommandPrefix:   "/",
		LogLevel:        "info",
		ShutdownTimeout: 10 * time.Second,
	}
}

func defaultRawBotConfig() rawBotConfig {
	cfg := defaultBotConfig()
	return rawBotConfig{
		Name:            cfg.Name,
		CommandPrefix:   cfg.CommandPrefix,
		LogLevel:        cfg.LogLevel,
		ShutdownTimeout: cfg.ShutdownTimeout.String(),
	}
}

func (r rawBotConfig) materialize() (BotConfig, error) {
	shutdownTimeout, err := time.ParseDuration(r.ShutdownTimeout)
	if err != nil {
		return BotConfig{}, fmt.Errorf("parse bot.shutdown_timeout: %w", err)
	}

	return BotConfig{
		Name:            r.Name,
		CommandPrefix:   r.CommandPrefix,
		LogLevel:        r.LogLevel,
		ShutdownTimeout: shutdownTimeout,
	}, nil
}

func (c BotConfig) validate(problems *[]string) {
	if strings.TrimSpace(c.CommandPrefix) == "" {
		*problems = append(*problems, "bot.command_prefix must not be empty")
	}
	if c.ShutdownTimeout <= 0 {
		*problems = append(*problems, "bot.shutdown_timeout must be greater than zero")
	}
}
