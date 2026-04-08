package bot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/shift-click/masterbot/internal/intent"
	"github.com/shift-click/masterbot/internal/store"
	"github.com/shift-click/masterbot/internal/transport"
)

type AutoQueryMode string

const (
	AutoQueryModeOff          AutoQueryMode = "off"
	AutoQueryModeExplicitOnly AutoQueryMode = "explicit-only"
	AutoQueryModeLocalAuto    AutoQueryMode = "local-auto"
)

type AutoQueryPolicy struct {
	Mode              AutoQueryMode `json:"mode"`
	AllowedHandlers   []string      `json:"allowed_handlers,omitempty"`
	BudgetPerHour     int           `json:"budget_per_hour"`
	CooldownWindow    time.Duration `json:"cooldown_window"`
	DegradationTarget AutoQueryMode `json:"degradation_target,omitempty"`
}

type AutoQueryDecision struct {
	Allowed bool
	Reason  string
}

func DefaultAutoQueryPolicy(catalog *intent.Catalog) AutoQueryPolicy {
	if catalog == nil {
		catalog = intent.DefaultCatalog()
	}
	allowed := make([]string, 0)
	for _, entry := range catalog.Entries() {
		if entry.AllowAutoQuery {
			allowed = append(allowed, entry.ID)
		}
	}
	return AutoQueryPolicy{
		Mode:              AutoQueryModeLocalAuto,
		AllowedHandlers:   allowed,
		BudgetPerHour:     30,
		CooldownWindow:    30 * time.Second,
		DegradationTarget: AutoQueryModeExplicitOnly,
	}
}

func (p AutoQueryPolicy) Normalize(catalog *intent.Catalog) AutoQueryPolicy {
	if catalog == nil {
		catalog = intent.DefaultCatalog()
	}
	defaults := DefaultAutoQueryPolicy(catalog)
	if p.Mode == "" {
		p.Mode = defaults.Mode
	}
	if len(p.AllowedHandlers) == 0 {
		p.AllowedHandlers = append([]string(nil), defaults.AllowedHandlers...)
	} else {
		normalized := make([]string, 0, len(p.AllowedHandlers))
		for _, raw := range p.AllowedHandlers {
			if id, ok := catalog.Normalize(raw); ok {
				normalized = append(normalized, id)
			}
		}
		p.AllowedHandlers = normalized
	}
	if p.BudgetPerHour <= 0 {
		p.BudgetPerHour = defaults.BudgetPerHour
	}
	if p.CooldownWindow <= 0 {
		p.CooldownWindow = defaults.CooldownWindow
	}
	if p.DegradationTarget == "" {
		p.DegradationTarget = defaults.DegradationTarget
	}
	return p
}

func (p AutoQueryPolicy) AllowsHandler(handlerID string) bool {
	catalog := intent.DefaultCatalog()
	normalized, ok := catalog.Normalize(handlerID)
	if !ok || normalized == "" {
		return false
	}
	for _, allowed := range p.Normalize(catalog).AllowedHandlers {
		if allowed == normalized {
			return true
		}
	}
	return false
}

type AutoQueryStore struct {
	store store.Store
}

func NewAutoQueryStore(s store.Store) *AutoQueryStore {
	if s == nil {
		return nil
	}
	return &AutoQueryStore{store: s}
}

func (s *AutoQueryStore) Get(ctx context.Context, room string) (AutoQueryPolicy, error) {
	if s == nil || s.store == nil {
		return AutoQueryPolicy{}, store.ErrNotFound
	}
	data, err := s.store.Get(ctx, roomPolicyKey(room))
	if err != nil {
		return AutoQueryPolicy{}, err
	}

	var policy AutoQueryPolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		return AutoQueryPolicy{}, err
	}
	return policy, nil
}

func (s *AutoQueryStore) Set(ctx context.Context, room string, policy AutoQueryPolicy) error {
	if s == nil || s.store == nil {
		return errors.New("auto query store is not configured")
	}
	payload, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	return s.store.Set(ctx, roomPolicyKey(room), payload)
}

func roomPolicyKey(room string) string {
	return "auto_query_policy/" + strings.TrimSpace(room)
}

type AutoQueryManager struct {
	catalog       *intent.Catalog
	store         *AutoQueryStore
	defaultPolicy AutoQueryPolicy
	now           func() time.Time

	mu             sync.Mutex
	roomHits       map[string][]time.Time
	queryCooldowns map[string]time.Time
}

func NewAutoQueryManager(catalog *intent.Catalog, policyStore *AutoQueryStore, defaultPolicy AutoQueryPolicy) *AutoQueryManager {
	if catalog == nil {
		catalog = intent.DefaultCatalog()
	}
	return &AutoQueryManager{
		catalog:        catalog,
		store:          policyStore,
		defaultPolicy:  defaultPolicy.Normalize(catalog),
		now:            time.Now,
		roomHits:       make(map[string][]time.Time),
		queryCooldowns: make(map[string]time.Time),
	}
}

func (m *AutoQueryManager) Policy(ctx context.Context, msg transport.Message) (AutoQueryPolicy, error) {
	if m == nil {
		return DefaultAutoQueryPolicy(intent.DefaultCatalog()), nil
	}

	roomID := messageRoomID(msg)
	if roomID == "" || m.store == nil {
		return m.defaultPolicy, nil
	}

	policy, err := m.store.Get(ctx, roomID)
	switch {
	case err == nil:
		return policy.Normalize(m.catalog), nil
	case errors.Is(err, store.ErrNotFound):
		return m.defaultPolicy, nil
	default:
		return AutoQueryPolicy{}, err
	}
}

func (m *AutoQueryManager) SetRoomPolicy(ctx context.Context, room string, policy AutoQueryPolicy) error {
	if m == nil || m.store == nil {
		return errors.New("auto query manager has no backing store")
	}
	return m.store.Set(ctx, room, policy.Normalize(m.catalog))
}

func (m *AutoQueryManager) AllowAutomatic(
	ctx context.Context,
	msg transport.Message,
	query string,
	allowedHandlers []string,
) (bool, error) {
	decision, err := m.EvaluateAutomatic(ctx, msg, query, allowedHandlers)
	if err != nil {
		return false, err
	}
	return decision.Allowed, nil
}

func (m *AutoQueryManager) EvaluateAutomatic(
	ctx context.Context,
	msg transport.Message,
	query string,
	allowedHandlers []string,
) (AutoQueryDecision, error) {
	if m == nil {
		return AutoQueryDecision{Reason: "manager-disabled"}, nil
	}

	policy, err := m.Policy(ctx, msg)
	if err != nil {
		return AutoQueryDecision{}, err
	}
	if policy.Mode != AutoQueryModeLocalAuto {
		return AutoQueryDecision{Reason: "room-policy"}, nil
	}

	if !hasAllowedAutoQueryHandler(policy, allowedHandlers) {
		return AutoQueryDecision{Reason: "handler-not-allowed"}, nil
	}

	now := m.now()
	roomID := autoQueryRoomID(msg)
	queryKey := autoQueryKey(roomID, query)

	m.mu.Lock()
	decision, degrade := m.evaluateAutomaticLocked(roomID, queryKey, now, policy)
	m.mu.Unlock()

	// Apply degradation policy outside the lock to avoid blocking
	// other evaluations during disk I/O.
	if degrade != nil {
		if err := m.applyDegradationPolicy(ctx, degrade.roomID, degrade.policy); err != nil {
			return AutoQueryDecision{}, err
		}
	}

	return decision, nil
}

func messageRoomID(msg transport.Message) string {
	switch {
	case strings.TrimSpace(msg.Raw.ChatID) != "":
		return strings.TrimSpace(msg.Raw.ChatID)
	case strings.TrimSpace(msg.Room) != "":
		return strings.TrimSpace(msg.Room)
	default:
		return ""
	}
}

func hasAllowedAutoQueryHandler(policy AutoQueryPolicy, allowedHandlers []string) bool {
	for _, handler := range allowedHandlers {
		if policy.AllowsHandler(handler) {
			return true
		}
	}
	return false
}

func autoQueryRoomID(msg transport.Message) string {
	roomID := messageRoomID(msg)
	if roomID == "" {
		return "_default"
	}
	return roomID
}

// deferredDegrade holds the info needed to apply a degradation policy
// outside the mutex to avoid blocking on disk I/O.
type deferredDegrade struct {
	roomID string
	policy AutoQueryPolicy
}

func (m *AutoQueryManager) evaluateAutomaticLocked(
	roomID, queryKey string,
	now time.Time,
	policy AutoQueryPolicy,
) (AutoQueryDecision, *deferredDegrade) {
	if cooldownUntil, ok := m.queryCooldowns[queryKey]; ok && now.Before(cooldownUntil) {
		return AutoQueryDecision{Reason: "cooldown"}, nil
	}

	hits := pruneWindow(m.roomHits[roomID], now, time.Hour)
	if len(hits) >= policy.BudgetPerHour {
		m.roomHits[roomID] = hits
		var degrade *deferredDegrade
		if policy.DegradationTarget != "" && policy.DegradationTarget != policy.Mode && m.store != nil {
			degrade = &deferredDegrade{roomID: roomID, policy: policy}
		}
		return AutoQueryDecision{Reason: "budget"}, degrade
	}

	m.roomHits[roomID] = append(hits, now)
	m.queryCooldowns[queryKey] = now.Add(policy.CooldownWindow)
	return AutoQueryDecision{Allowed: true}, nil
}

func (m *AutoQueryManager) applyDegradationPolicy(ctx context.Context, roomID string, policy AutoQueryPolicy) error {
	if policy.DegradationTarget == "" || policy.DegradationTarget == policy.Mode || m.store == nil {
		return nil
	}
	degraded := policy
	degraded.Mode = policy.DegradationTarget
	return m.store.Set(ctx, roomID, degraded.Normalize(m.catalog))
}

func autoQueryKey(room, query string) string {
	return strings.ToLower(strings.TrimSpace(room)) + "|" + strings.ToLower(strings.TrimSpace(query))
}
