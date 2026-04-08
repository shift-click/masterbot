package bot

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/intent"
	"github.com/shift-click/masterbot/internal/store"
	"github.com/shift-click/masterbot/internal/transport"
)

const AdminACLIntentID = "admin.acl"

type AccessSnapshot struct {
	Rooms      []config.AccessRoomConfig
	AdminRooms []string
	AdminUsers []string
}

type AccessController struct {
	catalog                  *intent.Catalog
	defaultPolicy            config.AccessDefaultPolicy
	runtimeEnabled           bool
	bootstrapAdminRoomChatID string
	bootstrapAdminUserID     string
	superAdminUsers          map[string]struct{}

	mu         sync.RWMutex
	rooms      map[string]config.AccessRoomConfig
	adminRooms map[string]struct{}
	adminUsers map[string]struct{}
}

func NewAccessController(catalog *intent.Catalog, cfg config.AccessConfig) *AccessController {
	if catalog == nil {
		catalog = intent.DefaultCatalog()
	}

	controller := &AccessController{
		catalog:                  catalog,
		defaultPolicy:            cfg.DefaultPolicy,
		runtimeEnabled:           cfg.RuntimeEnabled(),
		bootstrapAdminRoomChatID: strings.TrimSpace(cfg.BootstrapAdminRoomChatID),
		bootstrapAdminUserID:     strings.TrimSpace(cfg.BootstrapAdminUserID),
		superAdminUsers:          makeSet(cfg.BootstrapSuperAdminUsers),
	}
	controller.applySnapshot(AccessSnapshot{
		Rooms: cfg.Rooms,
	})
	return controller
}

func (a *AccessController) RuntimeEnabled() bool {
	if a == nil {
		return false
	}
	return a.runtimeEnabled
}

func (a *AccessController) NormalizeIntentID(intentID string) string {
	if a == nil || a.catalog == nil {
		if id, ok := intent.DefaultCatalog().Normalize(intentID); ok {
			return id
		}
		return strings.TrimSpace(strings.ToLower(intentID))
	}
	if id, ok := a.catalog.Normalize(intentID); ok {
		return id
	}
	return strings.TrimSpace(strings.ToLower(intentID))
}

func (a *AccessController) IsAllowed(chatID, intentID string) bool {
	if a == nil {
		return true
	}

	chatID = strings.TrimSpace(chatID)
	intentID = a.NormalizeIntentID(intentID)
	if chatID == "" || intentID == "" {
		return a.defaultPolicy == config.AccessPolicyAllow
	}

	a.mu.RLock()
	room, exists := a.rooms[chatID]
	a.mu.RUnlock()
	if !exists {
		return a.defaultPolicy == config.AccessPolicyAllow
	}

	for _, allowedIntent := range room.AllowIntents {
		if a.NormalizeIntentID(allowedIntent) == intentID {
			return true
		}
	}
	return false
}

func (a *AccessController) CanExecute(msg transport.Message, intentID string) bool {
	if a.IsBootstrapSuperAdmin(msg) {
		return true
	}
	normalized := a.NormalizeIntentID(intentID)
	if normalized == "admin" || strings.TrimSpace(intentID) == AdminACLIntentID {
		return a.IsAuthorizedAdmin(msg)
	}
	return a.IsAllowed(msg.Raw.ChatID, normalized)
}

func (a *AccessController) IsAuthorizedAdmin(msg transport.Message) bool {
	if a == nil {
		return false
	}
	return a.IsRuntimeAdmin(msg) || a.IsBootstrapSuperAdmin(msg)
}

func (a *AccessController) IsRuntimeAdmin(msg transport.Message) bool {
	if a == nil || !a.runtimeEnabled {
		return false
	}

	chatID := strings.TrimSpace(msg.Raw.ChatID)
	userID := strings.TrimSpace(msg.Raw.UserID)
	if chatID == "" || userID == "" {
		return false
	}

	a.mu.RLock()
	_, roomOK := a.adminRooms[chatID]
	_, userOK := a.adminUsers[userID]
	a.mu.RUnlock()
	return roomOK && userOK
}

func (a *AccessController) IsBootstrapSuperAdmin(msg transport.Message) bool {
	if a == nil || !a.runtimeEnabled {
		return false
	}
	userID := strings.TrimSpace(msg.Raw.UserID)
	if userID == "" {
		return false
	}
	if _, ok := a.superAdminUsers[userID]; ok {
		return true
	}
	return strings.TrimSpace(msg.Raw.ChatID) == a.bootstrapAdminRoomChatID &&
		userID == a.bootstrapAdminUserID
}

func (a *AccessController) Snapshot() AccessSnapshot {
	if a == nil {
		return AccessSnapshot{}
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	rooms := make([]config.AccessRoomConfig, 0, len(a.rooms))
	for _, room := range a.rooms {
		rooms = append(rooms, config.AccessRoomConfig{
			ChatID:       room.ChatID,
			Alias:        room.Alias,
			AllowIntents: append([]string(nil), room.AllowIntents...),
		})
	}
	sort.Slice(rooms, func(i, j int) bool {
		return rooms[i].ChatID < rooms[j].ChatID
	})

	adminRooms := make([]string, 0, len(a.adminRooms))
	for chatID := range a.adminRooms {
		adminRooms = append(adminRooms, chatID)
	}
	sort.Strings(adminRooms)

	adminUsers := make([]string, 0, len(a.adminUsers))
	for userID := range a.adminUsers {
		adminUsers = append(adminUsers, userID)
	}
	sort.Strings(adminUsers)

	return AccessSnapshot{
		Rooms:      rooms,
		AdminRooms: adminRooms,
		AdminUsers: adminUsers,
	}
}

func (a *AccessController) LoadRuntimeSnapshot(snapshot store.ACLSnapshot) {
	if a == nil {
		return
	}

	rooms := make([]config.AccessRoomConfig, 0, len(snapshot.Rooms))
	for _, room := range snapshot.Rooms {
		rooms = append(rooms, config.AccessRoomConfig{
			ChatID:       room.ChatID,
			Alias:        room.Alias,
			AllowIntents: append([]string(nil), room.AllowIntents...),
		})
	}
	a.applySnapshot(AccessSnapshot{
		Rooms:      rooms,
		AdminRooms: append([]string(nil), snapshot.AdminRooms...),
		AdminUsers: append([]string(nil), snapshot.AdminUsers...),
	})
}

func (a *AccessController) Validate(knownIntentIDs []string) error {
	if a == nil {
		return nil
	}

	known := make(map[string]struct{}, len(knownIntentIDs))
	for _, intentID := range knownIntentIDs {
		normalized := a.NormalizeIntentID(intentID)
		if normalized == "" {
			continue
		}
		known[normalized] = struct{}{}
	}

	a.mu.RLock()
	rooms := make([]config.AccessRoomConfig, 0, len(a.rooms))
	for _, room := range a.rooms {
		rooms = append(rooms, room)
	}
	a.mu.RUnlock()

	var unknown []string
	for _, room := range rooms {
		for _, intentID := range room.AllowIntents {
			if _, ok := known[a.NormalizeIntentID(intentID)]; ok {
				continue
			}
			unknown = append(unknown, fmt.Sprintf("%s:%s", room.ChatID, intentID))
		}
	}

	if len(unknown) == 0 {
		return nil
	}

	sort.Strings(unknown)
	return fmt.Errorf("unknown intents in access config: %s", strings.Join(unknown, ", "))
}

func (a *AccessController) applySnapshot(snapshot AccessSnapshot) {
	rooms := make(map[string]config.AccessRoomConfig, len(snapshot.Rooms))
	for _, room := range snapshot.Rooms {
		rooms[room.ChatID] = config.AccessRoomConfig{
			ChatID:       room.ChatID,
			Alias:        room.Alias,
			AllowIntents: append([]string(nil), room.AllowIntents...),
		}
	}
	adminRooms := make(map[string]struct{}, len(snapshot.AdminRooms))
	for _, chatID := range snapshot.AdminRooms {
		adminRooms[strings.TrimSpace(chatID)] = struct{}{}
	}
	adminUsers := make(map[string]struct{}, len(snapshot.AdminUsers))
	for _, userID := range snapshot.AdminUsers {
		adminUsers[strings.TrimSpace(userID)] = struct{}{}
	}

	a.mu.Lock()
	a.rooms = rooms
	a.adminRooms = adminRooms
	a.adminUsers = adminUsers
	a.mu.Unlock()
}

func makeSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return map[string]struct{}{}
	}

	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		out[trimmed] = struct{}{}
	}
	return out
}
