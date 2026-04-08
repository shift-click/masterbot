package config

import (
	"fmt"
	"strings"
	"time"
)

type AutoQueryConfig struct {
	DefaultPolicy AutoQueryPolicyConfig
	Rooms         []AutoQueryRoomConfig
}

type AutoQueryPolicyConfig struct {
	Mode              string
	AllowedHandlers   []string
	BudgetPerHour     int
	CooldownWindow    time.Duration
	DegradationTarget string
}

type AutoQueryRoomConfig struct {
	ChatID            string
	Mode              string
	AllowedHandlers   []string
	BudgetPerHour     int
	CooldownWindow    time.Duration
	DegradationTarget string
}

type rawAutoQueryConfig struct {
	DefaultPolicy rawAutoQueryPolicy `koanf:"default_policy"`
	Rooms         []rawAutoQueryRoom `koanf:"rooms"`
}

type rawAutoQueryPolicy struct {
	Mode              string   `koanf:"mode"`
	AllowedHandlers   []string `koanf:"allowed_handlers"`
	BudgetPerHour     int      `koanf:"budget_per_hour"`
	CooldownWindow    string   `koanf:"cooldown_window"`
	DegradationTarget string   `koanf:"degradation_target"`
}

type rawAutoQueryRoom struct {
	ChatID            string   `koanf:"chat_id"`
	Mode              string   `koanf:"mode"`
	AllowedHandlers   []string `koanf:"allowed_handlers"`
	BudgetPerHour     int      `koanf:"budget_per_hour"`
	CooldownWindow    string   `koanf:"cooldown_window"`
	DegradationTarget string   `koanf:"degradation_target"`
}

const (
	autoQueryModeOff          = "off"
	autoQueryModeExplicitOnly = "explicit-only"
	autoQueryModeLocalAuto    = "local-auto"
	autoQueryRoomsPrefix      = "auto_query.rooms["
)

func defaultAutoQueryConfig() AutoQueryConfig {
	return AutoQueryConfig{
		DefaultPolicy: AutoQueryPolicyConfig{
			Mode:              autoQueryModeExplicitOnly,
			AllowedHandlers:   []string{"coin", "stock", "coupang"},
			BudgetPerHour:     30,
			CooldownWindow:    30 * time.Second,
			DegradationTarget: autoQueryModeExplicitOnly,
		},
	}
}

func defaultRawAutoQueryConfig() rawAutoQueryConfig {
	cfg := defaultAutoQueryConfig()
	return rawAutoQueryConfig{
		DefaultPolicy: rawAutoQueryPolicy{
			Mode:              cfg.DefaultPolicy.Mode,
			AllowedHandlers:   append([]string(nil), cfg.DefaultPolicy.AllowedHandlers...),
			BudgetPerHour:     cfg.DefaultPolicy.BudgetPerHour,
			CooldownWindow:    cfg.DefaultPolicy.CooldownWindow.String(),
			DegradationTarget: cfg.DefaultPolicy.DegradationTarget,
		},
		Rooms: toRawAutoQueryRooms(cfg.Rooms),
	}
}

func (r rawAutoQueryConfig) materialize() (AutoQueryConfig, error) {
	defaultCooldown, err := time.ParseDuration(r.DefaultPolicy.CooldownWindow)
	if err != nil {
		return AutoQueryConfig{}, fmt.Errorf("parse auto_query.default_policy.cooldown_window: %w", err)
	}
	rooms, err := materializeAutoQueryRooms(r.Rooms)
	if err != nil {
		return AutoQueryConfig{}, err
	}
	return AutoQueryConfig{
		DefaultPolicy: AutoQueryPolicyConfig{
			Mode:              strings.TrimSpace(r.DefaultPolicy.Mode),
			AllowedHandlers:   compactStrings(r.DefaultPolicy.AllowedHandlers),
			BudgetPerHour:     r.DefaultPolicy.BudgetPerHour,
			CooldownWindow:    defaultCooldown,
			DegradationTarget: strings.TrimSpace(r.DefaultPolicy.DegradationTarget),
		},
		Rooms: rooms,
	}, nil
}

func (c AutoQueryConfig) validate(problems *[]string) {
	validateAutoQueryDefaultPolicy(c.DefaultPolicy, problems)
	validateAutoQueryRooms(c.Rooms, problems)
}

func toRawAutoQueryRooms(rooms []AutoQueryRoomConfig) []rawAutoQueryRoom {
	if len(rooms) == 0 {
		return nil
	}

	out := make([]rawAutoQueryRoom, 0, len(rooms))
	for _, room := range rooms {
		out = append(out, rawAutoQueryRoom{
			ChatID:            room.ChatID,
			Mode:              room.Mode,
			AllowedHandlers:   append([]string(nil), room.AllowedHandlers...),
			BudgetPerHour:     room.BudgetPerHour,
			CooldownWindow:    room.CooldownWindow.String(),
			DegradationTarget: room.DegradationTarget,
		})
	}
	return out
}

func materializeAutoQueryRooms(rawRooms []rawAutoQueryRoom) ([]AutoQueryRoomConfig, error) {
	if len(rawRooms) == 0 {
		return nil, nil
	}

	rooms := make([]AutoQueryRoomConfig, 0, len(rawRooms))
	for i, room := range rawRooms {
		cooldown := time.Duration(0)
		if strings.TrimSpace(room.CooldownWindow) != "" {
			parsed, err := time.ParseDuration(room.CooldownWindow)
			if err != nil {
				return nil, fmt.Errorf("parse auto_query.rooms[%d].cooldown_window: %w", i, err)
			}
			cooldown = parsed
		}
		rooms = append(rooms, AutoQueryRoomConfig{
			ChatID:            strings.TrimSpace(room.ChatID),
			Mode:              strings.TrimSpace(room.Mode),
			AllowedHandlers:   compactStrings(room.AllowedHandlers),
			BudgetPerHour:     room.BudgetPerHour,
			CooldownWindow:    cooldown,
			DegradationTarget: strings.TrimSpace(room.DegradationTarget),
		})
	}
	return rooms, nil
}

func validateAutoQueryDefaultPolicy(policy AutoQueryPolicyConfig, problems *[]string) {
	if !isAllowedAutoQueryMode(policy.Mode) {
		*problems = append(*problems, "auto_query.default_policy.mode must be one of: off, explicit-only, local-auto")
	}
	if policy.BudgetPerHour <= 0 {
		*problems = append(*problems, "auto_query.default_policy.budget_per_hour must be greater than zero")
	}
	if policy.CooldownWindow <= 0 {
		*problems = append(*problems, "auto_query.default_policy.cooldown_window must be greater than zero")
	}
	if !isAllowedAutoQueryMode(policy.DegradationTarget) {
		*problems = append(*problems, "auto_query.default_policy.degradation_target must be one of: off, explicit-only, local-auto")
	}
}

func validateAutoQueryRooms(rooms []AutoQueryRoomConfig, problems *[]string) {
	seen := make(map[string]struct{}, len(rooms))
	for i, room := range rooms {
		chatID := strings.TrimSpace(room.ChatID)
		if chatID == "" {
			*problems = append(*problems, roomFieldPath(i, "chat_id")+" must not be empty")
			continue
		}
		if _, exists := seen[chatID]; exists {
			*problems = append(*problems, roomFieldPath(i, "chat_id")+" "+quote(chatID)+" is duplicated")
		}
		seen[chatID] = struct{}{}

		if !isAllowedAutoQueryMode(room.Mode) {
			*problems = append(*problems, roomFieldPath(i, "mode")+" must be one of: off, explicit-only, local-auto")
		}
		if room.BudgetPerHour < 0 {
			*problems = append(*problems, roomFieldPath(i, "budget_per_hour")+" must not be negative")
		}
		if room.CooldownWindow < 0 {
			*problems = append(*problems, roomFieldPath(i, "cooldown_window")+" must not be negative")
		}
		if !isAllowedAutoQueryMode(room.DegradationTarget) {
			*problems = append(*problems, roomFieldPath(i, "degradation_target")+" must be one of: off, explicit-only, local-auto")
		}
	}
}

func isAllowedAutoQueryMode(mode string) bool {
	switch strings.TrimSpace(mode) {
	case "", autoQueryModeOff, autoQueryModeExplicitOnly, autoQueryModeLocalAuto:
		return true
	default:
		return false
	}
}

func roomFieldPath(index int, field string) string {
	return autoQueryRoomsPrefix + itoa(index) + "]." + field
}
