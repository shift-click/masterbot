package config

import (
	"fmt"
	"strings"
	"time"
)

// IrisInstanceConfig holds connection settings for a single Iris instance.
type IrisInstanceConfig struct {
	ID                string
	WSURL             string
	HTTPURL           string
	RequestTimeout    time.Duration
	ReconnectMin      time.Duration
	ReconnectMax      time.Duration
	RoomWorkerEnabled bool
	RoomWorkerCount   int
}

// IrisConfig is the top-level Iris configuration.
// When Instances is non-empty, per-instance settings are used.
// When Instances is empty but WSURL/HTTPURL are set, a single "default" instance is inferred.
type IrisConfig struct {
	Enabled           bool
	WSURL             string // legacy single-instance field
	HTTPURL           string // legacy single-instance field
	RequestTimeout    time.Duration
	ReconnectMin      time.Duration
	ReconnectMax      time.Duration
	RoomWorkerEnabled bool
	RoomWorkerCount   int
	Instances         []IrisInstanceConfig
}

type rawIrisInstanceConfig struct {
	ID                string `koanf:"id"`
	WSURL             string `koanf:"ws_url"`
	HTTPURL           string `koanf:"http_url"`
	RequestTimeout    string `koanf:"request_timeout"`
	ReconnectMin      string `koanf:"reconnect_min"`
	ReconnectMax      string `koanf:"reconnect_max"`
	RoomWorkerEnabled *bool  `koanf:"room_worker_enabled"`
	RoomWorkerCount   *int   `koanf:"room_worker_count"`
}

type rawIrisConfig struct {
	Enabled           bool                    `koanf:"enabled"`
	WSURL             string                  `koanf:"ws_url"`
	HTTPURL           string                  `koanf:"http_url"`
	RequestTimeout    string                  `koanf:"request_timeout"`
	ReconnectMin      string                  `koanf:"reconnect_min"`
	ReconnectMax      string                  `koanf:"reconnect_max"`
	RoomWorkerEnabled bool                    `koanf:"room_worker_enabled"`
	RoomWorkerCount   int                     `koanf:"room_worker_count"`
	Instances         []rawIrisInstanceConfig `koanf:"instances"`
}

func defaultIrisConfig() IrisConfig {
	return IrisConfig{
		Enabled:           true,
		WSURL:             "ws://127.0.0.1:3000/ws",
		HTTPURL:           "http://127.0.0.1:3000",
		RequestTimeout:    5 * time.Second,
		ReconnectMin:      1 * time.Second,
		ReconnectMax:      30 * time.Second,
		RoomWorkerEnabled: true,
		RoomWorkerCount:   32,
	}
}

func defaultRawIrisConfig() rawIrisConfig {
	cfg := defaultIrisConfig()
	return rawIrisConfig{
		Enabled:           cfg.Enabled,
		WSURL:             cfg.WSURL,
		HTTPURL:           cfg.HTTPURL,
		RequestTimeout:    cfg.RequestTimeout.String(),
		ReconnectMin:      cfg.ReconnectMin.String(),
		ReconnectMax:      cfg.ReconnectMax.String(),
		RoomWorkerEnabled: cfg.RoomWorkerEnabled,
		RoomWorkerCount:   cfg.RoomWorkerCount,
	}
}

func (r rawIrisConfig) materialize() (IrisConfig, error) {
	requestTimeout, err := time.ParseDuration(r.RequestTimeout)
	if err != nil {
		return IrisConfig{}, fmt.Errorf("parse iris.request_timeout: %w", err)
	}
	reconnectMin, err := time.ParseDuration(r.ReconnectMin)
	if err != nil {
		return IrisConfig{}, fmt.Errorf("parse iris.reconnect_min: %w", err)
	}
	reconnectMax, err := time.ParseDuration(r.ReconnectMax)
	if err != nil {
		return IrisConfig{}, fmt.Errorf("parse iris.reconnect_max: %w", err)
	}

	cfg := IrisConfig{
		Enabled:           r.Enabled,
		WSURL:             r.WSURL,
		HTTPURL:           r.HTTPURL,
		RequestTimeout:    requestTimeout,
		ReconnectMin:      reconnectMin,
		ReconnectMax:      reconnectMax,
		RoomWorkerEnabled: r.RoomWorkerEnabled,
		RoomWorkerCount:   r.RoomWorkerCount,
	}

	// Materialize explicit instances.
	for i, raw := range r.Instances {
		inst, err := raw.materialize(cfg, i)
		if err != nil {
			return IrisConfig{}, err
		}
		cfg.Instances = append(cfg.Instances, inst)
	}

	// Backward compatibility: if no instances defined but WSURL/HTTPURL set,
	// synthesize a single "default" instance.
	if len(cfg.Instances) == 0 && cfg.Enabled && strings.TrimSpace(cfg.WSURL) != "" {
		cfg.Instances = []IrisInstanceConfig{{
			ID:                "default",
			WSURL:             cfg.WSURL,
			HTTPURL:           cfg.HTTPURL,
			RequestTimeout:    cfg.RequestTimeout,
			ReconnectMin:      cfg.ReconnectMin,
			ReconnectMax:      cfg.ReconnectMax,
			RoomWorkerEnabled: cfg.RoomWorkerEnabled,
			RoomWorkerCount:   cfg.RoomWorkerCount,
		}}
	}

	return cfg, nil
}

func (r rawIrisInstanceConfig) materialize(parent IrisConfig, index int) (IrisInstanceConfig, error) {
	id := strings.TrimSpace(r.ID)
	if id == "" {
		return IrisInstanceConfig{}, fmt.Errorf("iris.instances[%d].id must not be empty", index)
	}

	inst := IrisInstanceConfig{
		ID:                id,
		WSURL:             r.WSURL,
		HTTPURL:           r.HTTPURL,
		RequestTimeout:    parent.RequestTimeout,
		ReconnectMin:      parent.ReconnectMin,
		ReconnectMax:      parent.ReconnectMax,
		RoomWorkerEnabled: parent.RoomWorkerEnabled,
		RoomWorkerCount:   parent.RoomWorkerCount,
	}

	// Override with instance-specific values if provided.
	if r.RequestTimeout != "" {
		d, err := time.ParseDuration(r.RequestTimeout)
		if err != nil {
			return IrisInstanceConfig{}, fmt.Errorf("parse iris.instances[%d].request_timeout: %w", index, err)
		}
		inst.RequestTimeout = d
	}
	if r.ReconnectMin != "" {
		d, err := time.ParseDuration(r.ReconnectMin)
		if err != nil {
			return IrisInstanceConfig{}, fmt.Errorf("parse iris.instances[%d].reconnect_min: %w", index, err)
		}
		inst.ReconnectMin = d
	}
	if r.ReconnectMax != "" {
		d, err := time.ParseDuration(r.ReconnectMax)
		if err != nil {
			return IrisInstanceConfig{}, fmt.Errorf("parse iris.instances[%d].reconnect_max: %w", index, err)
		}
		inst.ReconnectMax = d
	}
	if r.RoomWorkerEnabled != nil {
		inst.RoomWorkerEnabled = *r.RoomWorkerEnabled
	}
	if r.RoomWorkerCount != nil {
		inst.RoomWorkerCount = *r.RoomWorkerCount
	}

	return inst, nil
}

// ResolvedInstances returns the effective instance list.
// If Instances is explicitly set, it is returned as-is.
// Otherwise, if WSURL is set, a single "default" instance is synthesized.
func (c IrisConfig) ResolvedInstances() []IrisInstanceConfig {
	if len(c.Instances) > 0 {
		return c.Instances
	}
	if strings.TrimSpace(c.WSURL) != "" {
		return []IrisInstanceConfig{{
			ID:                "default",
			WSURL:             c.WSURL,
			HTTPURL:           c.HTTPURL,
			RequestTimeout:    c.RequestTimeout,
			ReconnectMin:      c.ReconnectMin,
			ReconnectMax:      c.ReconnectMax,
			RoomWorkerEnabled: c.RoomWorkerEnabled,
			RoomWorkerCount:   c.RoomWorkerCount,
		}}
	}
	return nil
}

func (c IrisConfig) validate(problems *[]string) {
	if !c.Enabled {
		return
	}

	if len(c.Instances) == 0 {
		c.validateLegacyURLs(problems)
	}

	c.validateSharedLimits(problems)
	c.validateInstances(problems)
}

func (c IrisConfig) validateLegacyURLs(problems *[]string) {
	if strings.TrimSpace(c.WSURL) == "" {
		*problems = append(*problems, "iris.ws_url must not be empty")
	}
	if strings.TrimSpace(c.HTTPURL) == "" {
		*problems = append(*problems, "iris.http_url must not be empty")
	}
}

func (c IrisConfig) validateSharedLimits(problems *[]string) {
	if c.RequestTimeout <= 0 {
		*problems = append(*problems, "iris.request_timeout must be greater than zero")
	}
	if c.ReconnectMin <= 0 {
		*problems = append(*problems, "iris.reconnect_min must be greater than zero")
	}
	if c.ReconnectMax < c.ReconnectMin {
		*problems = append(*problems, "iris.reconnect_max must be greater than or equal to iris.reconnect_min")
	}
	if c.RoomWorkerCount <= 0 {
		*problems = append(*problems, "iris.room_worker_count must be greater than zero")
	}
}

func (c IrisConfig) validateInstances(problems *[]string) {
	seen := make(map[string]bool, len(c.Instances))
	for i, inst := range c.Instances {
		c.validateInstance(problems, seen, i, inst)
	}
}

func (c IrisConfig) validateInstance(problems *[]string, seen map[string]bool, index int, inst IrisInstanceConfig) {
	if seen[inst.ID] {
		*problems = append(*problems, fmt.Sprintf("iris.instances[%d]: duplicate id %q", index, inst.ID))
	}
	seen[inst.ID] = true

	if strings.TrimSpace(inst.WSURL) == "" {
		*problems = append(*problems, fmt.Sprintf("iris.instances[%d].ws_url must not be empty", index))
	}
	if strings.TrimSpace(inst.HTTPURL) == "" {
		*problems = append(*problems, fmt.Sprintf("iris.instances[%d].http_url must not be empty", index))
	}
	if inst.RequestTimeout <= 0 {
		*problems = append(*problems, fmt.Sprintf("iris.instances[%d].request_timeout must be greater than zero", index))
	}
	if inst.ReconnectMin <= 0 {
		*problems = append(*problems, fmt.Sprintf("iris.instances[%d].reconnect_min must be greater than zero", index))
	}
	if inst.ReconnectMax < inst.ReconnectMin {
		*problems = append(*problems, fmt.Sprintf("iris.instances[%d].reconnect_max must be >= reconnect_min", index))
	}
	if inst.RoomWorkerCount <= 0 {
		*problems = append(*problems, fmt.Sprintf("iris.instances[%d].room_worker_count must be greater than zero", index))
	}
}
