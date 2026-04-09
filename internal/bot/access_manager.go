package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/store"
)

type AccessActor struct {
	ChatID string
	UserID string
}

type AccessManager struct {
	controller *AccessController
	store      store.ACLStore
	cfg        config.AccessConfig
	logger     *slog.Logger
}

func NewAccessManager(controller *AccessController, aclStore store.ACLStore, cfg config.AccessConfig, logger *slog.Logger) *AccessManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &AccessManager{
		controller: controller,
		store:      aclStore,
		cfg:        cfg,
		logger:     logger.With("component", "access_manager"),
	}
}

func (m *AccessManager) Bootstrap(ctx context.Context) error {
	if m == nil || m.store == nil {
		return nil
	}
	if err := m.store.SeedBootstrap(ctx, store.ACLBootstrap{
		Rooms:           m.cfg.Rooms,
		AdminRoomChatID: m.cfg.BootstrapAdminRoomChatID,
		AdminUserID:     m.cfg.BootstrapAdminUserID,
	}); err != nil {
		return err
	}
	ensured, err := m.ensureBootstrapAdminPrincipals(ctx)
	if err != nil {
		return err
	}
	if err := m.Reload(ctx); err != nil {
		return err
	}
	if len(ensured.rooms) > 0 || len(ensured.users) > 0 {
		m.logger.Warn(
			"runtime ACL bootstrap principal drift repaired",
			"admin_rooms_added", ensured.rooms,
			"admin_users_added", ensured.users,
		)
	}
	return nil
}

func (m *AccessManager) Reload(ctx context.Context) error {
	if m == nil || m.store == nil || m.controller == nil {
		return nil
	}
	snapshot, err := m.store.Snapshot(ctx)
	if err != nil {
		return err
	}
	m.controller.LoadRuntimeSnapshot(snapshot)
	return nil
}

func (m *AccessManager) Snapshot() AccessSnapshot {
	if m == nil || m.controller == nil {
		return AccessSnapshot{}
	}
	return m.controller.Snapshot()
}

func (m *AccessManager) FindRoomByAlias(alias string) (store.ACLRoom, bool) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return store.ACLRoom{}, false
	}
	for _, room := range m.Snapshot().Rooms {
		if room.Alias == alias {
			return store.ACLRoom{
				ChatID:       room.ChatID,
				Alias:        room.Alias,
				AllowIntents: append([]string(nil), room.AllowIntents...),
			}, true
		}
	}
	return store.ACLRoom{}, false
}

func (m *AccessManager) UpsertRoom(ctx context.Context, actor AccessActor, room store.ACLRoom) (bool, error) {
	changed, err := m.store.UpsertRoom(ctx, room)
	if err != nil {
		return false, err
	}
	if err := m.afterMutation(ctx, changed, actor, "room.upsert", "room", room.ChatID, fmt.Sprintf("alias=%s", room.Alias)); err != nil {
		return false, err
	}
	return changed, nil
}

func (m *AccessManager) DeleteRoom(ctx context.Context, actor AccessActor, chatID string) (bool, error) {
	changed, err := m.store.DeleteRoom(ctx, chatID)
	if err != nil {
		return false, err
	}
	if err := m.afterMutation(ctx, changed, actor, "room.delete", "room", chatID, ""); err != nil {
		return false, err
	}
	return changed, nil
}

func (m *AccessManager) AddRoomIntent(ctx context.Context, actor AccessActor, chatID, intentID string) (bool, error) {
	changed, err := m.store.UpsertRoomIntent(ctx, chatID, intentID)
	if err != nil {
		return false, err
	}
	if err := m.afterMutation(ctx, changed, actor, "room_intent.add", "room_intent", chatID+":"+intentID, ""); err != nil {
		return false, err
	}
	return changed, nil
}

func (m *AccessManager) RemoveRoomIntent(ctx context.Context, actor AccessActor, chatID, intentID string) (bool, error) {
	changed, err := m.store.DeleteRoomIntent(ctx, chatID, intentID)
	if err != nil {
		return false, err
	}
	if err := m.afterMutation(ctx, changed, actor, "room_intent.remove", "room_intent", chatID+":"+intentID, ""); err != nil {
		return false, err
	}
	return changed, nil
}

func (m *AccessManager) AddAdminRoom(ctx context.Context, actor AccessActor, chatID string) (bool, error) {
	changed, err := m.store.UpsertAdminRoom(ctx, chatID)
	if err != nil {
		return false, err
	}
	if err := m.afterMutation(ctx, changed, actor, "admin_room.add", "admin_room", chatID, ""); err != nil {
		return false, err
	}
	return changed, nil
}

func (m *AccessManager) RemoveAdminRoom(ctx context.Context, actor AccessActor, chatID string) (bool, error) {
	changed, err := m.store.DeleteAdminRoom(ctx, chatID)
	if err != nil {
		return false, err
	}
	if err := m.afterMutation(ctx, changed, actor, "admin_room.remove", "admin_room", chatID, ""); err != nil {
		return false, err
	}
	return changed, nil
}

func (m *AccessManager) AddAdminUser(ctx context.Context, actor AccessActor, userID string) (bool, error) {
	changed, err := m.store.UpsertAdminUser(ctx, userID)
	if err != nil {
		return false, err
	}
	if err := m.afterMutation(ctx, changed, actor, "admin_user.add", "admin_user", userID, ""); err != nil {
		return false, err
	}
	return changed, nil
}

func (m *AccessManager) RemoveAdminUser(ctx context.Context, actor AccessActor, userID string) (bool, error) {
	changed, err := m.store.DeleteAdminUser(ctx, userID)
	if err != nil {
		return false, err
	}
	if err := m.afterMutation(ctx, changed, actor, "admin_user.remove", "admin_user", userID, ""); err != nil {
		return false, err
	}
	return changed, nil
}

func (m *AccessManager) afterMutation(ctx context.Context, changed bool, actor AccessActor, action, targetType, targetID, details string) error {
	if !changed {
		return nil
	}
	if err := m.store.AppendAudit(ctx, store.ACLAuditEntry{
		ActorChatID: strings.TrimSpace(actor.ChatID),
		ActorUserID: strings.TrimSpace(actor.UserID),
		Action:      action,
		TargetType:  targetType,
		TargetID:    strings.TrimSpace(targetID),
		Details:     strings.TrimSpace(details),
		CreatedAt:   time.Now(),
	}); err != nil {
		return err
	}
	if err := m.Reload(ctx); err != nil {
		return err
	}
	m.logger.Info("acl mutation applied", "action", action, "target_type", targetType, "target_id", targetID)
	return nil
}

type bootstrapEnsureResult struct {
	rooms []string
	users []string
}

func (m *AccessManager) ensureBootstrapAdminPrincipals(ctx context.Context) (bootstrapEnsureResult, error) {
	if m == nil || m.store == nil {
		return bootstrapEnsureResult{}, nil
	}

	var result bootstrapEnsureResult

	roomID := strings.TrimSpace(m.cfg.BootstrapAdminRoomChatID)
	if roomID != "" {
		ensured, err := m.ensureBootstrapPrincipal(ctx, roomID, "bootstrap_admin_room.ensure", "admin_room", m.store.UpsertAdminRoom)
		if err != nil {
			return result, err
		}
		if ensured {
			result.rooms = append(result.rooms, roomID)
		}
	}

	userID := strings.TrimSpace(m.cfg.BootstrapAdminUserID)
	if userID != "" {
		ensured, err := m.ensureBootstrapPrincipal(ctx, userID, "bootstrap_admin_user.ensure", "admin_user", m.store.UpsertAdminUser)
		if err != nil {
			return result, err
		}
		if ensured {
			result.users = append(result.users, userID)
		}
	}

	return result, nil
}

func (m *AccessManager) ensureBootstrapPrincipal(
	ctx context.Context,
	targetID string,
	action string,
	targetType string,
	upsert func(context.Context, string) (bool, error),
) (bool, error) {
	changed, err := upsert(ctx, targetID)
	if err != nil || !changed {
		return changed, err
	}
	err = m.store.AppendAudit(ctx, store.ACLAuditEntry{
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Details:    "source=startup",
		CreatedAt:  time.Now(),
	})
	return changed, err
}
