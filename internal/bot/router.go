package bot

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/shift-click/masterbot/internal/intent"
	"github.com/shift-click/masterbot/internal/metrics"
)

const (
	// DeniedIntentMessage is the canonical reply text the router sends when
	// an intent is rejected by the ACL evaluator. It is exported so out-of-
	// package callers (admin smoke runner, tests) can detect deny outcomes
	// from a single source of truth instead of duplicating the literal.
	DeniedIntentMessage     = "이 방에서는 사용할 수 없는 기능입니다."
	unknownCommandReplyText = "알 수 없는 명령어입니다. 도움 으로 사용 가능한 명령어를 확인하세요."
)

type FallbackHandler interface {
	HandleFallback(context.Context, CommandContext) error
}

type FallbackHandlerFunc func(context.Context, CommandContext) error

func (f FallbackHandlerFunc) HandleFallback(ctx context.Context, cmd CommandContext) error {
	return f(ctx, cmd)
}

type fallbackScope string

const (
	fallbackScopeDeterministic fallbackScope = "deterministic"
	fallbackScopeAuto          fallbackScope = "auto"
)

type fallbackEntry struct {
	id         string
	handler    FallbackHandler
	scope      fallbackScope
	aclExempt  bool
}

type fallbackDispatchResult struct {
	denied bool
}

type routerEventOptions struct {
	commandID     string
	commandSource metrics.CommandSource
	featureKey    string
	attribution   string
	success       *bool
	errorClass    string
	latency       time.Duration
	denied        bool
	metadata      map[string]any
}

type dispatchPlanKind string

const (
	dispatchPlanIgnore     dispatchPlanKind = "ignore"
	dispatchPlanExplicit   dispatchPlanKind = "explicit"
	dispatchPlanPrefixless dispatchPlanKind = "prefixless"
)

type dispatchPlan struct {
	kind            dispatchPlanKind
	recordUnmatched bool
	entry           intent.Entry
	handler         Handler
	args            []string
}

type Router struct {
	prefix string
	logger *slog.Logger
	access *AccessController

	mu          sync.RWMutex
	registry    *Registry
	middlewares []Middleware
	autoQueries *AutoQueryManager
	recorder    metrics.Recorder
}

func NewRouter(prefix string, access *AccessController, registry *Registry, logger *slog.Logger) *Router {
	if prefix == "" {
		prefix = "/"
	}
	if logger == nil {
		logger = slog.Default()
	}
	if registry == nil {
		registry = NewRegistry(intent.DefaultCatalog())
	}

	return &Router{
		prefix:   prefix,
		logger:   logger.With("component", "router"),
		access:   access,
		registry: registry,
	}
}

func (r *Router) Registry() *Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.registry
}

func (r *Router) Catalog() *intent.Catalog {
	return r.Registry().Catalog()
}

func (r *Router) Register(handler Handler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.registry.Register(handler)
}

func (r *Router) Use(middlewares ...Middleware) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.middlewares = append(r.middlewares, middlewares...)
}

func (r *Router) SetAutoQueryManager(manager *AutoQueryManager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.autoQueries = manager
}

func (r *Router) SetMetricsRecorder(recorder metrics.Recorder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recorder = recorder
}

func (r *Router) ValidateAccess() error {
	if r.access == nil {
		return nil
	}
	return r.access.Validate(r.IntentIDs())
}

func (r *Router) ValidateRegistry() error {
	return r.Registry().Validate()
}

func (r *Router) IntentIDs() []string {
	return r.Registry().IntentIDs()
}

func (r *Router) RegisterExplicit(_ string, _ ...string) error {
	return nil
}

func (r *Router) SetFallback(handler FallbackHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registry.fallbacks = nil
	_ = r.registry.AddAutoFallback(handler)
}

func (r *Router) AddFallback(handler FallbackHandler) {
	_ = r.AddAutoFallback(handler)
}

func (r *Router) AddAutoFallback(handler FallbackHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.registry.AddAutoFallback(handler)
}

func (r *Router) AddDeterministicFallback(handler FallbackHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.registry.AddDeterministicFallback(handler)
}

func (r *Router) VisibleEntries(chatID string) []intent.Entry {
	return r.Registry().VisibleEntries(chatID, r.access)
}
