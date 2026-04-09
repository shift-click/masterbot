package config

import (
	"fmt"
	"strings"
	"time"
)

type LottoConfig struct {
	DBPath       string
	SyncCooldown time.Duration
}

type rawLottoConfig struct {
	DBPath       string `koanf:"db_path"`
	SyncCooldown string `koanf:"sync_cooldown"`
}

func defaultLottoConfig() LottoConfig {
	return LottoConfig{
		DBPath:       "data/lotto.db",
		SyncCooldown: 10 * time.Minute,
	}
}

func defaultRawLottoConfig() rawLottoConfig {
	cfg := defaultLottoConfig()
	return rawLottoConfig{
		DBPath:       cfg.DBPath,
		SyncCooldown: cfg.SyncCooldown.String(),
	}
}

func (r rawLottoConfig) materialize() (LottoConfig, error) {
	syncCooldown, err := time.ParseDuration(r.SyncCooldown)
	if err != nil {
		return LottoConfig{}, fmt.Errorf("parse lotto.sync_cooldown: %w", err)
	}
	return LottoConfig{
		DBPath:       strings.TrimSpace(r.DBPath),
		SyncCooldown: syncCooldown,
	}, nil
}

func (c LottoConfig) validate(problems *[]string) {
	if strings.TrimSpace(c.DBPath) == "" {
		*problems = append(*problems, "lotto.db_path must not be empty")
	}
	if c.SyncCooldown <= 0 {
		*problems = append(*problems, "lotto.sync_cooldown must be greater than zero")
	}
}
