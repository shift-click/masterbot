package config

import (
	"strings"
)

type StoreConfig struct {
	Driver string
	Table  string
}

type rawStoreConfig struct {
	Driver string `koanf:"driver"`
	Table  string `koanf:"table"`
}

func defaultStoreConfig() StoreConfig {
	return StoreConfig{
		Driver: "memory",
		Table:  "bot_state",
	}
}

func defaultRawStoreConfig() rawStoreConfig {
	cfg := defaultStoreConfig()
	return rawStoreConfig{
		Driver: cfg.Driver,
		Table:  cfg.Table,
	}
}

func (r rawStoreConfig) materialize() (StoreConfig, error) {
	return StoreConfig{
		Driver: strings.TrimSpace(r.Driver),
		Table:  strings.TrimSpace(r.Table),
	}, nil
}

func (c StoreConfig) validate(problems *[]string) {
	switch strings.TrimSpace(strings.ToLower(c.Driver)) {
	case "memory":
	default:
		*problems = append(*problems, "store.driver must be one of: memory")
	}
}
