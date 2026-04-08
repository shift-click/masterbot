package bot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/commandmeta"
	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/intent"
	"github.com/shift-click/masterbot/internal/metrics"
	"github.com/shift-click/masterbot/internal/transport"
)

type routerRecorder struct {
	events []metrics.Event
}

func (r *routerRecorder) Record(_ context.Context, event metrics.Event) {
	r.events = append(r.events, event)
}

type routerTestHandler struct {
	descriptor     commandmeta.Descriptor
	supportsSlash  bool
	executeErr     error
	fallbackErr    error
	autoCandidate  bool
	executeCalls   int
	fallbackCalls  int
}

func newRouterTestHandler(descriptorID string) *routerTestHandler {
	return &routerTestHandler{
		descriptor:    commandmeta.Must(descriptorID),
		supportsSlash: true,
	}
}

func (h *routerTestHandler) Name() string { return h.descriptor.Name }

func (h *routerTestHandler) Aliases() []string {
	return append([]string(nil), h.descriptor.SlashAliases...)
}

func (h *routerTestHandler) Description() string { return h.descriptor.Description }

func (h *routerTestHandler) Descriptor() commandmeta.Descriptor { return h.descriptor }

func (h *routerTestHandler) SupportsSlashCommands() bool { return h.supportsSlash }

func (h *routerTestHandler) Execute(_ context.Context, cmd CommandContext) error {
	h.executeCalls++
	if h.executeErr != nil {
		return h.executeErr
	}
	return cmd.Reply(context.Background(), Reply{Type: transport.ReplyTypeText, Text: h.descriptor.Name})
}

func (h *routerTestHandler) HandleFallback(_ context.Context, _ CommandContext) error {
	h.fallbackCalls++
	return h.fallbackErr
}

func (h *routerTestHandler) MatchAutoQueryCandidate(context.Context, string) bool {
	return h.autoCandidate
}

func TestRouterBuildDispatchPlanVariants(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(intent.DefaultCatalog())
	handler := newRouterTestHandler("help")
	if err := registry.Register(handler); err != nil {
		t.Fatalf("register: %v", err)
	}
	chartHandler := newRouterTestHandler("chart")
	if err := registry.Register(chartHandler); err != nil {
		t.Fatalf("register chart: %v", err)
	}
	router := NewRouter("/", nil, registry, nil)

	slash := router.buildDispatchPlan(transport.Message{Msg: "/help"})
	if slash.kind != dispatchPlanIgnore || !slash.recordUnmatched {
		t.Fatalf("slash plan = %+v", slash)
	}

	jamo := router.buildDispatchPlan(transport.Message{Msg: "ㅎㅎ"})
	if jamo.kind != dispatchPlanIgnore || !jamo.recordUnmatched {
		t.Fatalf("jamo plan = %+v", jamo)
	}

	explicit := router.buildDispatchPlan(transport.Message{Msg: "도움 옵션"})
	if explicit.kind != dispatchPlanExplicit {
		t.Fatalf("explicit plan = %+v", explicit)
	}
	if len(explicit.args) != 1 || explicit.args[0] != "옵션" {
		t.Fatalf("explicit args = %v", explicit.args)
	}

	chartPrefixless := router.buildDispatchPlan(transport.Message{Msg: "차트 단어"})
	if chartPrefixless.kind != dispatchPlanPrefixless {
		t.Fatalf("chart prefixless plan = %+v", chartPrefixless)
	}

	prefixless := router.buildDispatchPlan(transport.Message{Msg: "안녕"})
	if prefixless.kind != dispatchPlanPrefixless {
		t.Fatalf("prefixless plan = %+v", prefixless)
	}
}

func TestDispatchDeterministicFallbacksDeniedRecordsAccessDenied(t *testing.T) {
	t.Parallel()

	catalog := intent.DefaultCatalog()
	access := NewAccessController(catalog, config.AccessConfig{
		DefaultPolicy: config.AccessPolicyDeny,
		Rooms:         []config.AccessRoomConfig{{ChatID: "room-1"}},
	})
	router := NewRouter("/", access, NewRegistry(catalog), nil)
	recorder := &routerRecorder{}
	router.SetMetricsRecorder(recorder)

	handled, err := router.dispatchDeterministicFallbacks(
		context.Background(),
		transport.Message{Raw: transport.RawChatLog{ChatID: "room-1"}},
		func(context.Context, Reply) error { return nil },
		[]fallbackEntry{{id: "coin", handler: newRouterTestHandler("coin"), scope: fallbackScopeDeterministic}},
		NewFallbackPolicy(access, nil),
	)
	if err != nil {
		t.Fatalf("dispatchDeterministicFallbacks: %v", err)
	}
	if !handled {
		t.Fatal("expected denied deterministic fallback to mark handled")
	}
	if len(recorder.events) != 1 || recorder.events[0].EventName != metrics.EventAccessDenied {
		t.Fatalf("unexpected recorder events: %+v", recorder.events)
	}
	if recorder.events[0].CommandSource != metrics.CommandSourceDeterministic {
		t.Fatalf("command source = %q", recorder.events[0].CommandSource)
	}
	if recorder.events[0].FeatureKey != "coin" || recorder.events[0].Attribution != string(metrics.CommandSourceDeterministic) {
		t.Fatalf("access denied fields = %+v", recorder.events[0])
	}
}

func TestDispatchDeterministicFallbacks_ACLExemptBypassesDeny(t *testing.T) {
	t.Parallel()

	catalog := intent.DefaultCatalog()
	access := NewAccessController(catalog, config.AccessConfig{
		DefaultPolicy: config.AccessPolicyDeny,
		Rooms:         []config.AccessRoomConfig{{ChatID: "room-1"}},
	})
	router := NewRouter("/", access, NewRegistry(catalog), nil)
	recorder := &routerRecorder{}
	router.SetMetricsRecorder(recorder)

	exemptHandler := newRouterTestHandler("calc")
	exemptHandler.fallbackErr = ErrHandled

	handled, err := router.dispatchDeterministicFallbacks(
		context.Background(),
		transport.Message{Raw: transport.RawChatLog{ChatID: "room-1"}},
		func(context.Context, Reply) error { return nil },
		[]fallbackEntry{{id: "calc", handler: exemptHandler, scope: fallbackScopeDeterministic, aclExempt: true}},
		NewFallbackPolicy(access, nil),
	)
	if err != nil {
		t.Fatalf("dispatchDeterministicFallbacks: %v", err)
	}
	if !handled {
		t.Fatal("expected ACL-exempt fallback to be handled")
	}
	if exemptHandler.fallbackCalls != 1 {
		t.Fatalf("fallback calls = %d, want 1", exemptHandler.fallbackCalls)
	}
}

func TestDispatchDeterministicFallbacks_NonExemptStillDenied(t *testing.T) {
	t.Parallel()

	catalog := intent.DefaultCatalog()
	access := NewAccessController(catalog, config.AccessConfig{
		DefaultPolicy: config.AccessPolicyDeny,
		Rooms:         []config.AccessRoomConfig{{ChatID: "room-1"}},
	})
	router := NewRouter("/", access, NewRegistry(catalog), nil)
	recorder := &routerRecorder{}
	router.SetMetricsRecorder(recorder)

	nonExemptHandler := newRouterTestHandler("coin")

	handled, err := router.dispatchDeterministicFallbacks(
		context.Background(),
		transport.Message{Raw: transport.RawChatLog{ChatID: "room-1"}},
		func(context.Context, Reply) error { return nil },
		[]fallbackEntry{{id: "coin", handler: nonExemptHandler, scope: fallbackScopeDeterministic, aclExempt: false}},
		NewFallbackPolicy(access, nil),
	)
	if err != nil {
		t.Fatalf("dispatchDeterministicFallbacks: %v", err)
	}
	if !handled {
		t.Fatal("expected denied non-exempt fallback to mark handled (denied path)")
	}
	if nonExemptHandler.fallbackCalls != 0 {
		t.Fatalf("non-exempt handler should not have been called, got %d calls", nonExemptHandler.fallbackCalls)
	}
	if len(recorder.events) != 1 || recorder.events[0].EventName != metrics.EventAccessDenied {
		t.Fatalf("expected access denied event, got: %+v", recorder.events)
	}
}

func TestDispatchDeterministicFallbacks_SuperAdminBypassesDeny(t *testing.T) {
	t.Parallel()

	catalog := intent.DefaultCatalog()
	access := NewAccessController(catalog, config.AccessConfig{
		DefaultPolicy:            config.AccessPolicyDeny,
		RuntimeDBPath:            "data/access.db",
		BootstrapSuperAdminUsers: []string{"super-admin-user"},
		Rooms:                    []config.AccessRoomConfig{{ChatID: "room-1"}},
	})
	router := NewRouter("/", access, NewRegistry(catalog), nil)
	recorder := &routerRecorder{}
	router.SetMetricsRecorder(recorder)

	handler := newRouterTestHandler("coin")
	handler.fallbackErr = ErrHandled

	superAdminMsg := transport.Message{Raw: transport.RawChatLog{ChatID: "room-1", UserID: "super-admin-user"}}

	handled, err := router.dispatchDeterministicFallbacks(
		context.Background(),
		superAdminMsg,
		func(context.Context, Reply) error { return nil },
		[]fallbackEntry{{id: "coin", handler: handler, scope: fallbackScopeDeterministic, aclExempt: false}},
		NewFallbackPolicy(access, nil),
	)
	if err != nil {
		t.Fatalf("dispatchDeterministicFallbacks: %v", err)
	}
	if !handled {
		t.Fatal("expected super admin to bypass deny and handle fallback")
	}
	if handler.fallbackCalls != 1 {
		t.Fatalf("fallback calls = %d, want 1", handler.fallbackCalls)
	}
}

func TestDispatchDeterministicFallbacks_NonAdminStillDenied(t *testing.T) {
	t.Parallel()

	catalog := intent.DefaultCatalog()
	access := NewAccessController(catalog, config.AccessConfig{
		DefaultPolicy:            config.AccessPolicyDeny,
		BootstrapSuperAdminUsers: []string{"super-admin-user"},
		Rooms:                    []config.AccessRoomConfig{{ChatID: "room-1"}},
	})
	router := NewRouter("/", access, NewRegistry(catalog), nil)
	recorder := &routerRecorder{}
	router.SetMetricsRecorder(recorder)

	handler := newRouterTestHandler("coin")

	normalUserMsg := transport.Message{Raw: transport.RawChatLog{ChatID: "room-1", UserID: "normal-user"}}

	handled, err := router.dispatchDeterministicFallbacks(
		context.Background(),
		normalUserMsg,
		func(context.Context, Reply) error { return nil },
		[]fallbackEntry{{id: "coin", handler: handler, scope: fallbackScopeDeterministic, aclExempt: false}},
		NewFallbackPolicy(access, nil),
	)
	if err != nil {
		t.Fatalf("dispatchDeterministicFallbacks: %v", err)
	}
	if !handled {
		t.Fatal("expected denied result to mark handled")
	}
	if handler.fallbackCalls != 0 {
		t.Fatalf("non-admin handler should not have been called, got %d calls", handler.fallbackCalls)
	}
	if len(recorder.events) != 1 || recorder.events[0].EventName != metrics.EventAccessDenied {
		t.Fatalf("expected access denied event, got: %+v", recorder.events)
	}
}

func TestExecuteFallbackEntryRecordsHandledAndFailureOutcomes(t *testing.T) {
	t.Parallel()

	router := NewRouter("/", nil, NewRegistry(intent.DefaultCatalog()), nil)
	recorder := &routerRecorder{}
	router.SetMetricsRecorder(recorder)
	reply := func(context.Context, Reply) error { return nil }
	msg := transport.Message{Raw: transport.RawChatLog{ID: "req-1", ChatID: "room-1", UserID: "user-1"}}

	successHandler := newRouterTestHandler("coin")
	successHandler.fallbackErr = ErrHandled
	handled, err := router.executeFallbackEntry(context.Background(), msg, reply, fallbackEntry{
		id:      "coin",
		handler: successHandler,
		scope:   fallbackScopeAuto,
	}, fallbackScopeAuto)
	if !handled || !errors.Is(err, ErrHandled) {
		t.Fatalf("handled fallback = (%v, %v)", handled, err)
	}

	failureHandler := newRouterTestHandler("coin")
	failureHandler.fallbackErr = ErrHandledWithFailure
	handled, err = router.executeFallbackEntry(context.Background(), msg, reply, fallbackEntry{
		id:      "coin",
		handler: failureHandler,
		scope:   fallbackScopeAuto,
	}, fallbackScopeAuto)
	if !handled || !errors.Is(err, ErrHandledWithFailure) {
		t.Fatalf("failed fallback = (%v, %v)", handled, err)
	}

	if len(recorder.events) != 4 {
		t.Fatalf("event count = %d, want 4", len(recorder.events))
	}
	if recorder.events[0].EventName != metrics.EventCommandDispatched || recorder.events[1].EventName != metrics.EventCommandSucceeded {
		t.Fatalf("success events = %+v", recorder.events[:2])
	}
	if recorder.events[2].EventName != metrics.EventCommandDispatched || recorder.events[3].EventName != metrics.EventCommandFailed {
		t.Fatalf("failure events = %+v", recorder.events[2:])
	}
	if recorder.events[3].ErrorClass != "fetch_error" {
		t.Fatalf("failure error class = %q", recorder.events[3].ErrorClass)
	}
	for _, event := range recorder.events {
		if event.CommandID != "coin" || event.CommandSource != metrics.CommandSourceAuto {
			t.Fatalf("fallback event routing drift: %+v", event)
		}
		if event.FeatureKey != "coin" || event.Attribution != string(metrics.CommandSourceAuto) {
			t.Fatalf("fallback event semantic drift: %+v", event)
		}
	}
}

func TestRouterHelperFunctions(t *testing.T) {
	t.Parallel()

	deterministic := fallbackEntry{id: "coin", handler: newRouterTestHandler("coin"), scope: fallbackScopeDeterministic}
	auto := fallbackEntry{id: "help", handler: newRouterTestHandler("help"), scope: fallbackScopeAuto}
	entries := []fallbackEntry{deterministic, auto}

	filtered := filterFallbacks(entries, fallbackScopeDeterministic)
	if len(filtered) != 1 || filtered[0].id != "coin" {
		t.Fatalf("filtered fallbacks = %+v", filtered)
	}

	ids := fallbackIDs(entries)
	if len(ids) != 2 || ids[0] != "coin" || ids[1] != "help" {
		t.Fatalf("fallback ids = %v", ids)
	}

	if commandSourceFromScope(fallbackScopeDeterministic) != metrics.CommandSourceDeterministic {
		t.Fatal("deterministic scope should map to deterministic source")
	}
	if commandSourceFromScope(fallbackScopeAuto) != metrics.CommandSourceAuto {
		t.Fatal("auto scope should map to auto source")
	}

	if got := classifyError(context.DeadlineExceeded); got != "deadline_exceeded" {
		t.Fatalf("deadline classify = %q", got)
	}
	if got := classifyError(context.Canceled); got != "canceled" {
		t.Fatalf("canceled classify = %q", got)
	}
	if got := classifyError(ErrHandledWithFailure); got != "fetch_error" {
		t.Fatalf("handled-with-failure classify = %q", got)
	}
	if got := classifyError(NewHandledFailure("invalid_input", false, "bad input", nil)); got != "invalid_input" {
		t.Fatalf("classified handled failure = %q", got)
	}
	if got := classifyError(errors.New("boom")); got != "handler_error" {
		t.Fatalf("generic classify = %q", got)
	}

	if got := fallbackHandlerID(intent.DefaultCatalog(), newRouterTestHandler("coin")); got != "coin" {
		t.Fatalf("fallback handler id = %q", got)
	}
	if got := fallbackCommandName(newRouterTestHandler("help"), "fallback-help"); got != "도움" {
		t.Fatalf("fallback command name = %q", got)
	}

	if err := swallowHandled(ErrHandled); err != nil {
		t.Fatalf("swallowHandled should clear ErrHandled, got %v", err)
	}
	plainErr := errors.New("plain")
	if err := swallowHandled(plainErr); !errors.Is(err, plainErr) {
		t.Fatalf("swallowHandled should preserve plain error, got %v", err)
	}

	plainHandler := struct{ Handler }{}
	if !supportsSlashCommands(plainHandler) {
		t.Fatal("plain handler should allow slash commands by default")
	}

	slashless := newRouterTestHandler("help")
	slashless.supportsSlash = false
	if supportsSlashCommands(slashless) {
		t.Fatal("slashless handler should report false")
	}

	policy, err := autoQueryPolicy(context.Background(), nil, transport.Message{})
	if err != nil {
		t.Fatalf("autoQueryPolicy nil manager: %v", err)
	}
	if policy.Mode != AutoQueryModeLocalAuto {
		t.Fatalf("default auto query mode = %s", policy.Mode)
	}

	recorder := &routerRecorder{}
	router := NewRouter("/", nil, NewRegistry(intent.DefaultCatalog()), nil)
	router.SetMetricsRecorder(recorder)
	router.recordEvent(context.Background(), transport.Message{
		Room: "운영방",
		Raw:  transport.RawChatLog{ID: "req-2", ChatID: "room-2", UserID: "user-2"},
	}, metrics.Event{CommandID: "coin"})
	if len(recorder.events) != 1 {
		t.Fatalf("recorded events = %d", len(recorder.events))
	}
	if recorder.events[0].Audience != "customer" || recorder.events[0].FeatureKey != "coin" {
		t.Fatalf("recorded event defaults = %+v", recorder.events[0])
	}
	if recorder.events[0].OccurredAt.IsZero() {
		t.Fatal("expected occurredAt to be populated")
	}
}

func TestRouterPublicMethodsAndRegistrationHelpers(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(intent.DefaultCatalog())
	helpHandler := newRouterTestHandler("help")
	coinHandler := newRouterTestHandler("coin")
	if err := registry.Register(helpHandler); err != nil {
		t.Fatalf("register help: %v", err)
	}
	if err := registry.Register(coinHandler); err != nil {
		t.Fatalf("register coin: %v", err)
	}

	router := NewRouter("/", nil, registry, nil)
	router.Use(NewLoggingMiddleware(nil, nil))
	if len(router.middlewares) != 1 {
		t.Fatalf("middleware len = %d, want 1", len(router.middlewares))
	}

	if got := router.Catalog(); got == nil {
		t.Fatal("expected catalog to be available")
	}
	if got := router.Registry(); got == nil {
		t.Fatal("expected registry to be available")
	}
	if err := router.ValidateAccess(); err != nil {
		t.Fatalf("validate access with nil controller: %v", err)
	}
	if err := router.ValidateRegistry(); err == nil {
		t.Fatal("expected partial registry validation to fail")
	}

	ids := router.IntentIDs()
	if len(ids) != 2 || ids[0] != "coin" || ids[1] != "help" {
		t.Fatalf("intent ids = %v", ids)
	}

	entry, handler, args, ok := router.parseSlash("/help now")
	if !ok || entry.ID != "help" || handler == nil {
		t.Fatalf("parseSlash = (%+v, %T, %v, %v)", entry, handler, args, ok)
	}
	if len(args) != 1 || args[0] != "now" {
		t.Fatalf("parseSlash args = %v", args)
	}

	visible := router.VisibleEntries("")
	if len(visible) == 0 {
		t.Fatal("expected visible entries to include registered handlers")
	}

	handlers := router.Handlers()
	if len(handlers) != 2 || handlers[0].Name() != "도움" || handlers[1].Name() != "코인" {
		t.Fatalf("handlers = %#v", handlers)
	}

	if err := router.RegisterExplicit("ignored"); err != nil {
		t.Fatalf("RegisterExplicit should be a no-op: %v", err)
	}

	router.SetFallback(helpHandler)
	if len(router.Registry().Fallbacks()) != 1 {
		t.Fatalf("fallback len after SetFallback = %d", len(router.Registry().Fallbacks()))
	}
	router.AddFallback(coinHandler)
	if len(router.Registry().Fallbacks()) != 2 {
		t.Fatalf("fallback len after AddFallback = %d", len(router.Registry().Fallbacks()))
	}
}

func TestDispatchAutomaticFallbacksBranches(t *testing.T) {
	t.Parallel()

	router := NewRouter("/", nil, NewRegistry(intent.DefaultCatalog()), nil)
	recorder := &routerRecorder{}
	router.SetMetricsRecorder(recorder)
	reply := func(context.Context, Reply) error { return nil }
	msg := transport.Message{Msg: "비트", Raw: transport.RawChatLog{ID: "req-3", ChatID: "room-3", UserID: "user-3"}}
	policy := NewFallbackPolicy(nil, nil)

	noCandidate := newRouterTestHandler("coin")
	noCandidate.autoCandidate = false
	if err := router.dispatchAutomaticFallbacks(
		context.Background(),
		msg,
		reply,
		[]fallbackEntry{{id: "coin", handler: noCandidate, scope: fallbackScopeAuto}},
		AutoQueryPolicy{Mode: AutoQueryModeLocalAuto, AllowedHandlers: []string{"coin"}},
		policy,
	); err != nil {
		t.Fatalf("dispatchAutomaticFallbacks no candidate: %v", err)
	}
	if len(recorder.events) != 1 || recorder.events[0].EventName != metrics.EventUnmatchedMessage {
		t.Fatalf("unexpected unmatched events: %+v", recorder.events)
	}
	if recorder.events[0].RequestID != "req-3" || recorder.events[0].Audience != "customer" {
		t.Fatalf("unexpected unmatched defaults: %+v", recorder.events[0])
	}

	recorder.events = nil
	candidate := newRouterTestHandler("coin")
	candidate.autoCandidate = true
	if err := router.dispatchAutomaticFallbacks(
		context.Background(),
		msg,
		reply,
		[]fallbackEntry{{id: "coin", handler: candidate, scope: fallbackScopeAuto}},
		AutoQueryPolicy{Mode: AutoQueryModeExplicitOnly, AllowedHandlers: []string{"coin"}},
		policy,
	); err != nil {
		t.Fatalf("dispatchAutomaticFallbacks policy skip: %v", err)
	}
	if len(recorder.events) != 1 || recorder.events[0].EventName != metrics.EventPolicySkip {
		t.Fatalf("unexpected policy skip events: %+v", recorder.events)
	}
	if recorder.events[0].CommandSource != metrics.CommandSourceAuto || recorder.events[0].Attribution != string(metrics.CommandSourceAuto) {
		t.Fatalf("unexpected policy skip source fields: %+v", recorder.events[0])
	}
	if recorder.events[0].RequestID != "req-3" || recorder.events[0].Audience != "customer" {
		t.Fatalf("unexpected policy skip defaults: %+v", recorder.events[0])
	}
}

func TestDispatchFallbacksAllowsWhitelistedHandlers(t *testing.T) {
	t.Parallel()

	router := NewRouter("/", nil, NewRegistry(intent.DefaultCatalog()), nil)
	recorder := &routerRecorder{}
	router.SetMetricsRecorder(recorder)
	fallback := newRouterTestHandler("coin")

	result, err := router.dispatchFallbacks(
		context.Background(),
		transport.Message{Raw: transport.RawChatLog{ID: "req-4", ChatID: "room-4", UserID: "user-4"}},
		func(context.Context, Reply) error { return nil },
		[]fallbackEntry{{id: "coin", handler: fallback, scope: fallbackScopeAuto}},
		fallbackScopeAuto,
		AutoQueryPolicy{Mode: AutoQueryModeLocalAuto, AllowedHandlers: []string{"coin"}},
		NewFallbackPolicy(nil, nil),
	)
	if err != nil {
		t.Fatalf("dispatchFallbacks: %v", err)
	}
	if result.denied {
		t.Fatal("expected dispatchFallbacks not to be denied")
	}
	if fallback.fallbackCalls != 1 {
		t.Fatalf("fallback calls = %d, want 1", fallback.fallbackCalls)
	}
}

func TestRegistryAddFallbackPropagatesACLExempt(t *testing.T) {
	t.Parallel()

	catalog := intent.DefaultCatalog()
	registry := NewRegistry(catalog)

	calcHandler := newRouterTestHandler("calc")
	if err := registry.AddDeterministicFallback(calcHandler); err != nil {
		t.Fatalf("add calc fallback: %v", err)
	}

	coinHandler := newRouterTestHandler("coin")
	if err := registry.AddAutoFallback(coinHandler); err != nil {
		t.Fatalf("add coin fallback: %v", err)
	}

	fallbacks := registry.Fallbacks()
	if len(fallbacks) != 2 {
		t.Fatalf("fallback count = %d, want 2", len(fallbacks))
	}

	var calcEntry, coinEntry fallbackEntry
	for _, fb := range fallbacks {
		switch fb.id {
		case "calc":
			calcEntry = fb
		case "coin":
			coinEntry = fb
		}
	}

	if !calcEntry.aclExempt {
		t.Fatal("calc fallback should have aclExempt=true")
	}
	if coinEntry.aclExempt {
		t.Fatal("coin fallback should have aclExempt=false")
	}
}

func TestDispatchHandlerRecordsCommandEventParity(t *testing.T) {
	t.Parallel()

	catalog := intent.DefaultCatalog()
	entry, ok := catalog.Entry("help")
	if !ok {
		t.Fatal("expected help entry")
	}
	router := NewRouter("/", nil, NewRegistry(catalog), nil)
	recorder := &routerRecorder{}
	router.SetMetricsRecorder(recorder)
	handler := newRouterTestHandler("help")

	err := router.dispatchHandler(
		context.Background(),
		transport.Message{Room: "운영방", Raw: transport.RawChatLog{ID: "req-5", ChatID: "room-5", UserID: "user-5"}},
		func(context.Context, Reply) error { return nil },
		entry,
		handler,
		nil,
		metrics.CommandSourceExplicit,
	)
	if err != nil {
		t.Fatalf("dispatchHandler: %v", err)
	}
	if len(recorder.events) != 2 {
		t.Fatalf("event count = %d, want 2", len(recorder.events))
	}
	for _, event := range recorder.events {
		if event.CommandID != "help" || event.CommandSource != metrics.CommandSourceExplicit {
			t.Fatalf("explicit routing drift: %+v", event)
		}
		if event.FeatureKey != "help" || event.Attribution != string(metrics.CommandSourceExplicit) {
			t.Fatalf("explicit semantic drift: %+v", event)
		}
		if event.RequestID != "req-5" || event.Audience != "customer" {
			t.Fatalf("explicit default fields drift: %+v", event)
		}
	}
	if recorder.events[0].EventName != metrics.EventCommandDispatched || recorder.events[1].EventName != metrics.EventActivation {
		t.Fatalf("unexpected event sequence: %+v", recorder.events)
	}
}

func TestNewRouterMetricEventDefaults(t *testing.T) {
	t.Parallel()

	event := newRouterMetricEvent(time.Time{}, metrics.EventPolicySkip, routerEventOptions{
		commandID:     "coin",
		commandSource: metrics.CommandSourceAuto,
	})
	if event.Audience != "customer" {
		t.Fatalf("audience = %q", event.Audience)
	}
	if event.FeatureKey != "coin" {
		t.Fatalf("feature key = %q", event.FeatureKey)
	}
	if event.Attribution != string(metrics.CommandSourceAuto) {
		t.Fatalf("attribution = %q", event.Attribution)
	}
}
