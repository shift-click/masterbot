package config

import (
	"strings"
)

type AccessDefaultPolicy string

const (
	AccessPolicyAllow AccessDefaultPolicy = "allow"
	AccessPolicyDeny  AccessDefaultPolicy = "deny"
)

type AccessConfig struct {
	DefaultPolicy            AccessDefaultPolicy
	RuntimeDBPath            string
	BootstrapAdminRoomChatID string
	BootstrapAdminUserID     string
	BootstrapSuperAdminUsers []string
	Rooms                    []AccessRoomConfig
}

type AccessRoomConfig struct {
	ChatID       string
	Alias        string
	AllowIntents []string
}

type rawAccessConfig struct {
	DefaultPolicy            string          `koanf:"default_policy"`
	RuntimeDBPath            string          `koanf:"runtime_db_path"`
	BootstrapAdminRoomChatID string          `koanf:"bootstrap_admin_room_chat_id"`
	BootstrapAdminUserID     string          `koanf:"bootstrap_admin_user_id"`
	BootstrapSuperAdminUsers []string        `koanf:"bootstrap_super_admin_users"`
	Rooms                    []rawAccessRoom `koanf:"rooms"`
}

type rawAccessRoom struct {
	ChatID       string   `koanf:"chat_id"`
	Alias        string   `koanf:"alias"`
	AllowIntents []string `koanf:"allow_intents"`
}

func defaultAccessConfig() AccessConfig {
	return AccessConfig{
		DefaultPolicy: AccessPolicyDeny,
		RuntimeDBPath: "",
		Rooms:         nil,
	}
}

func defaultRawAccessConfig() rawAccessConfig {
	cfg := defaultAccessConfig()
	return rawAccessConfig{
		DefaultPolicy:            string(cfg.DefaultPolicy),
		RuntimeDBPath:            cfg.RuntimeDBPath,
		BootstrapAdminRoomChatID: cfg.BootstrapAdminRoomChatID,
		BootstrapAdminUserID:     cfg.BootstrapAdminUserID,
		BootstrapSuperAdminUsers: append([]string(nil), cfg.BootstrapSuperAdminUsers...),
		Rooms:                    toRawAccessRooms(cfg.Rooms),
	}
}

func (r rawAccessConfig) materialize() (AccessConfig, error) {
	return AccessConfig{
		DefaultPolicy:            AccessDefaultPolicy(strings.TrimSpace(r.DefaultPolicy)),
		RuntimeDBPath:            strings.TrimSpace(r.RuntimeDBPath),
		BootstrapAdminRoomChatID: strings.TrimSpace(r.BootstrapAdminRoomChatID),
		BootstrapAdminUserID:     strings.TrimSpace(r.BootstrapAdminUserID),
		BootstrapSuperAdminUsers: compactStrings(r.BootstrapSuperAdminUsers),
		Rooms:                    materializeAccessRooms(r.Rooms),
	}, nil
}

func (c AccessConfig) validate(problems *[]string) {
	switch c.DefaultPolicy {
	case AccessPolicyAllow, AccessPolicyDeny:
	default:
		*problems = append(*problems, "access.default_policy must be one of: allow, deny")
	}

	runtimeDBPath := strings.TrimSpace(c.RuntimeDBPath)
	bootstrapAdminRoom := strings.TrimSpace(c.BootstrapAdminRoomChatID)
	bootstrapAdminUser := strings.TrimSpace(c.BootstrapAdminUserID)
	superAdminUsers := compactStrings(c.BootstrapSuperAdminUsers)
	validateAccessRuntimeSettings(runtimeDBPath, bootstrapAdminRoom, bootstrapAdminUser, superAdminUsers, problems)
	validateAccessRooms(c.Rooms, problems)
}

func (c AccessConfig) RuntimeEnabled() bool {
	return strings.TrimSpace(c.RuntimeDBPath) != ""
}

func toRawAccessRooms(rooms []AccessRoomConfig) []rawAccessRoom {
	if len(rooms) == 0 {
		return nil
	}

	out := make([]rawAccessRoom, 0, len(rooms))
	for _, room := range rooms {
		out = append(out, rawAccessRoom{
			ChatID:       room.ChatID,
			Alias:        room.Alias,
			AllowIntents: append([]string(nil), room.AllowIntents...),
		})
	}
	return out
}

func materializeAccessRooms(rawRooms []rawAccessRoom) []AccessRoomConfig {
	if len(rawRooms) == 0 {
		return nil
	}

	rooms := make([]AccessRoomConfig, 0, len(rawRooms))
	for _, room := range rawRooms {
		allowIntents := make([]string, 0, len(room.AllowIntents))
		for _, intent := range room.AllowIntents {
			intent = strings.TrimSpace(intent)
			if intent == "" {
				continue
			}
			allowIntents = append(allowIntents, intent)
		}
		rooms = append(rooms, AccessRoomConfig{
			ChatID:       strings.TrimSpace(room.ChatID),
			Alias:        strings.TrimSpace(room.Alias),
			AllowIntents: allowIntents,
		})
	}
	return rooms
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func validateAccessRuntimeSettings(
	runtimeDBPath, bootstrapAdminRoom, bootstrapAdminUser string,
	superAdminUsers []string,
	problems *[]string,
) {
	if runtimeDBPath == "" {
		if bootstrapAdminRoom != "" || bootstrapAdminUser != "" || len(superAdminUsers) > 0 {
			*problems = append(*problems, "access.runtime_db_path is required when bootstrap admin settings are configured")
		}
		return
	}
	if len(superAdminUsers) == 0 && bootstrapAdminRoom == "" {
		*problems = append(*problems, "access.bootstrap_admin_room_chat_id is required when access.runtime_db_path is set")
	}
	if len(superAdminUsers) == 0 && bootstrapAdminUser == "" {
		*problems = append(*problems, "access.bootstrap_admin_user_id is required when access.runtime_db_path is set")
	}
}

func validateAccessRooms(rooms []AccessRoomConfig, problems *[]string) {
	seenChatIDs := make(map[string]struct{}, len(rooms))
	for i, room := range rooms {
		chatID := strings.TrimSpace(room.ChatID)
		if chatID == "" {
			*problems = append(*problems, accessRoomFieldPath(i, "chat_id")+" must not be empty")
			continue
		}
		if _, exists := seenChatIDs[chatID]; exists {
			*problems = append(*problems, accessRoomFieldPath(i, "chat_id")+" "+quote(chatID)+" is duplicated")
		}
		seenChatIDs[chatID] = struct{}{}
	}
}

func accessRoomFieldPath(index int, field string) string {
	return "access.rooms[" + itoa(index) + "]." + field
}
