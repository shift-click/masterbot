package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shift-click/masterbot/internal/intent"
	"github.com/shift-click/masterbot/internal/store"
)

type AutoQueryBootstrapRoom struct {
	ChatID string
	Policy AutoQueryPolicy
}

type AutoQueryActor struct {
	ChatID string
	UserID string
}

type AutoQueryAuditEntry struct {
	ActorChatID  string          `json:"actor_chat_id"`
	ActorUserID  string          `json:"actor_user_id"`
	TargetChatID string          `json:"target_chat_id"`
	Before       AutoQueryPolicy `json:"before"`
	After        AutoQueryPolicy `json:"after"`
	CreatedAt    time.Time       `json:"created_at"`
}

func (m *AutoQueryManager) Bootstrap(ctx context.Context, rooms []AutoQueryBootstrapRoom) error {
	if m == nil || m.store == nil {
		return nil
	}

	for _, room := range rooms {
		chatID := strings.TrimSpace(room.ChatID)
		if chatID == "" {
			continue
		}
		_, err := m.store.Get(ctx, chatID)
		switch {
		case err == nil:
			continue
		case errors.Is(err, store.ErrNotFound):
			if err := m.store.Set(ctx, chatID, room.Policy.Normalize(m.catalog)); err != nil {
				return err
			}
		default:
			return err
		}
	}
	return nil
}

func (m *AutoQueryManager) PolicyForRoom(ctx context.Context, room string) (AutoQueryPolicy, bool, error) {
	if m == nil {
		return DefaultAutoQueryPolicy(intent.DefaultCatalog()), false, nil
	}

	room = strings.TrimSpace(room)
	if room == "" || m.store == nil {
		return m.defaultPolicy, false, nil
	}

	policy, err := m.store.Get(ctx, room)
	switch {
	case err == nil:
		return policy.Normalize(m.catalog), true, nil
	case errors.Is(err, store.ErrNotFound):
		return m.defaultPolicy, false, nil
	default:
		return AutoQueryPolicy{}, false, err
	}
}

func (m *AutoQueryManager) UpdateRoomPolicy(ctx context.Context, actor AutoQueryActor, room string, policy AutoQueryPolicy) (bool, error) {
	if m == nil || m.store == nil {
		return false, errors.New("auto query manager has no backing store")
	}

	room = strings.TrimSpace(room)
	if room == "" {
		return false, errors.New("room is required")
	}

	before, stored, err := m.PolicyForRoom(ctx, room)
	if err != nil {
		return false, err
	}
	after := policy.Normalize(m.catalog)
	if stored && autoQueryPoliciesEqual(m.catalog, before, after) {
		return false, nil
	}
	if !stored && autoQueryPoliciesEqual(m.catalog, m.defaultPolicy, after) {
		return false, nil
	}

	if err := m.store.Set(ctx, room, after); err != nil {
		return false, err
	}
	if err := m.appendAudit(ctx, AutoQueryAuditEntry{
		ActorChatID:  strings.TrimSpace(actor.ChatID),
		ActorUserID:  strings.TrimSpace(actor.UserID),
		TargetChatID: room,
		Before:       before.Normalize(m.catalog),
		After:        after,
		CreatedAt:    time.Now(),
	}); err != nil {
		return false, err
	}
	return true, nil
}

func (m *AutoQueryManager) appendAudit(ctx context.Context, entry AutoQueryAuditEntry) error {
	if m == nil || m.store == nil || m.store.store == nil {
		return nil
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("auto_query_audit/%s/%s", entry.CreatedAt.UTC().Format(time.RFC3339Nano), entry.TargetChatID)
	return m.store.store.Set(ctx, key, payload)
}

func autoQueryPoliciesEqual(catalog *intent.Catalog, a, b AutoQueryPolicy) bool {
	a = a.Normalize(catalog)
	b = b.Normalize(catalog)
	if a.Mode != b.Mode || a.BudgetPerHour != b.BudgetPerHour || a.CooldownWindow != b.CooldownWindow || a.DegradationTarget != b.DegradationTarget {
		return false
	}
	if len(a.AllowedHandlers) != len(b.AllowedHandlers) {
		return false
	}
	for i := range a.AllowedHandlers {
		if a.AllowedHandlers[i] != b.AllowedHandlers[i] {
			return false
		}
	}
	return true
}
