package bot_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/commandmeta"
	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/intent"
	"github.com/shift-click/masterbot/internal/metrics"
	"github.com/shift-click/masterbot/internal/store"
	"github.com/shift-click/masterbot/internal/transport"
)

type stubQueryHandler struct {
	name         string
	executeText  string
	bareMatches  map[string][]string
	executeCalls atomic.Int32
	lastArgs     []string
}

type stubAutoFallbackHandler struct {
	stubQueryHandler
	candidateMatch func(string) bool
	handleCalls    atomic.Int32
}

func (s *stubQueryHandler) Name() string { return s.name }

func (s *stubQueryHandler) Aliases() []string {
	if descriptor, ok := commandmeta.Lookup(s.name); ok {
		return append([]string(nil), descriptor.SlashAliases...)
	}
	return nil
}

func (s *stubQueryHandler) Description() string {
	if descriptor, ok := commandmeta.Lookup(s.name); ok {
		return descriptor.Description
	}
	return s.name
}

func (s *stubQueryHandler) SupportsSlashCommands() bool { return false }

func (s *stubQueryHandler) Descriptor() commandmeta.Descriptor {
	id, _ := commandmeta.NormalizeIntentID(s.name)
	if id == "" {
		id = s.name
	}
	descriptor, _ := commandmeta.Lookup(id)
	return descriptor
}

func (s *stubQueryHandler) MatchBareQuery(_ context.Context, content string) ([]string, bool) {
	args, ok := s.bareMatches[content]
	if !ok {
		return nil, false
	}
	return append([]string(nil), args...), true
}

func (s *stubQueryHandler) Execute(_ context.Context, cmd bot.CommandContext) error {
	s.executeCalls.Add(1)
	s.lastArgs = append([]string(nil), cmd.Args...)
	return cmd.Reply(context.Background(), bot.Reply{
		Type: transport.ReplyTypeText,
		Text: s.executeText,
	})
}

func (s *stubAutoFallbackHandler) MatchAutoQueryCandidate(_ context.Context, content string) bool {
	if s.candidateMatch == nil {
		return false
	}
	return s.candidateMatch(content)
}

func (s *stubAutoFallbackHandler) HandleFallback(_ context.Context, cmd bot.CommandContext) error {
	s.handleCalls.Add(1)
	if err := cmd.Reply(context.Background(), bot.Reply{
		Type: transport.ReplyTypeText,
		Text: s.executeText,
	}); err != nil {
		return err
	}
	return bot.ErrHandled
}

type recordingRecorder struct {
	mu     sync.Mutex
	events []metrics.Event
}

func (r *recordingRecorder) Record(_ context.Context, event metrics.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *recordingRecorder) eventNames() []metrics.EventName {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]metrics.EventName, 0, len(r.events))
	for _, event := range r.events {
		names = append(names, event.EventName)
	}
	return names
}

func (r *recordingRecorder) snapshot() []metrics.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]metrics.Event(nil), r.events...)
}

type stubFallbackHandler struct {
	name        string
	handleFn    func(bot.CommandContext) error
	matchAuto   func(string) bool
	handleCalls atomic.Int32
}

func (s *stubFallbackHandler) Name() string { return s.name }

func (s *stubFallbackHandler) HandleFallback(_ context.Context, cmd bot.CommandContext) error {
	s.handleCalls.Add(1)
	if s.handleFn != nil {
		return s.handleFn(cmd)
	}
	return nil
}

func (s *stubFallbackHandler) MatchAutoQueryCandidate(_ context.Context, content string) bool {
	return s.matchAuto != nil && s.matchAuto(content)
}

func newTestRouter(t *testing.T) *bot.Router {
	t.Helper()
	catalog := intent.DefaultCatalog()
	return bot.NewRouter("/", nil, bot.NewRegistry(catalog), nil)
}

func newAccessRouter(t *testing.T) *bot.Router {
	t.Helper()

	catalog := intent.DefaultCatalog()
	access := bot.NewAccessController(catalog, config.AccessConfig{
		DefaultPolicy: config.AccessPolicyDeny,
		Rooms: []config.AccessRoomConfig{
			{ChatID: "room-1", AllowIntents: []string{"help.show", "coin.quote"}},
		},
	})
	return bot.NewRouter("/", access, bot.NewRegistry(catalog), nil)
}

func TestRouterDispatch_BareQueryUsesHandlerExecute(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)
	coin := &stubQueryHandler{
		name:        "코인",
		executeText: "coin execute",
		bareMatches: map[string][]string{"비트": {"비트"}},
	}
	if err := router.Register(coin); err != nil {
		t.Fatalf("register: %v", err)
	}

	var reply bot.Reply
	err := router.Dispatch(context.Background(), transport.Message{Msg: "비트"}, func(_ context.Context, r bot.Reply) error {
		reply = r
		return nil
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if coin.executeCalls.Load() != 1 {
		t.Fatalf("execute calls = %d, want 1", coin.executeCalls.Load())
	}
	if got := len(coin.lastArgs); got != 1 || coin.lastArgs[0] != "비트" {
		t.Fatalf("args = %v, want [비트]", coin.lastArgs)
	}
	if reply.Text != "coin execute" {
		t.Fatalf("reply = %q, want %q", reply.Text, "coin execute")
	}
}

func TestRouterDispatch_BareQueryPreservesRegistrationOrder(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)
	coin := &stubQueryHandler{
		name:        "코인",
		executeText: "coin execute",
		bareMatches: map[string][]string{"금": {"금"}},
	}
	gold := &stubQueryHandler{
		name:        "금시세",
		executeText: "gold execute",
		bareMatches: map[string][]string{"금": {"금"}},
	}
	if err := router.Register(gold); err != nil {
		t.Fatalf("register gold: %v", err)
	}
	if err := router.Register(coin); err != nil {
		t.Fatalf("register coin: %v", err)
	}

	var replies []string
	err := router.Dispatch(context.Background(), transport.Message{Msg: "금"}, func(_ context.Context, r bot.Reply) error {
		replies = append(replies, r.Text)
		return nil
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if gold.executeCalls.Load() != 1 {
		t.Fatalf("gold execute calls = %d, want 1", gold.executeCalls.Load())
	}
	if coin.executeCalls.Load() != 0 {
		t.Fatalf("coin execute calls = %d, want 0", coin.executeCalls.Load())
	}
	if len(replies) != 1 || replies[0] != "gold execute" {
		t.Fatalf("replies = %v, want [gold execute]", replies)
	}
}

func TestRouterDispatch_BareCommandWithoutArgs(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)
	help := &stubQueryHandler{
		name:        "도움",
		executeText: "help execute",
		bareMatches: map[string][]string{},
	}
	if err := router.Register(help); err != nil {
		t.Fatalf("register: %v", err)
	}

	var replies []string
	err := router.Dispatch(context.Background(), transport.Message{Msg: "도움"}, func(_ context.Context, r bot.Reply) error {
		replies = append(replies, r.Text)
		return nil
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if help.executeCalls.Load() != 1 {
		t.Fatalf("execute calls = %d, want 1", help.executeCalls.Load())
	}
	if len(help.lastArgs) != 0 {
		t.Fatalf("args = %v, want none", help.lastArgs)
	}
	if len(replies) != 1 || replies[0] != "help execute" {
		t.Fatalf("replies = %v, want [help execute]", replies)
	}
}

func TestRouterDispatch_SlashBlockedAndBareCommandAllowed(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)
	admin := &stubQueryHandler{
		name:        "관리",
		executeText: "admin execute",
		bareMatches: map[string][]string{},
	}
	if err := router.Register(admin); err != nil {
		t.Fatalf("register: %v", err)
	}

	var replies []string
	_ = router.Dispatch(context.Background(), transport.Message{Msg: "/관리 상태"}, func(_ context.Context, r bot.Reply) error {
		replies = append(replies, r.Text)
		return nil
	})
	_ = router.Dispatch(context.Background(), transport.Message{Msg: "관리 상태"}, func(_ context.Context, r bot.Reply) error {
		replies = append(replies, r.Text)
		return nil
	})

	if admin.executeCalls.Load() != 1 {
		t.Fatalf("execute calls = %d, want 1", admin.executeCalls.Load())
	}
	if got := len(admin.lastArgs); got != 1 || admin.lastArgs[0] != "상태" {
		t.Fatalf("args = %v, want [상태]", admin.lastArgs)
	}
	if len(replies) != 1 || replies[0] != "admin execute" {
		t.Fatalf("replies = %v, want [admin execute]", replies)
	}
}

func TestRouterDispatch_ExplicitDenyReturnsMessage(t *testing.T) {
	t.Parallel()

	router := newAccessRouter(t)
	stock := &stubQueryHandler{
		name:        "주식",
		executeText: "stock execute",
		bareMatches: map[string][]string{"삼성전자": {"삼성전자"}},
	}
	if err := router.Register(stock); err != nil {
		t.Fatalf("register: %v", err)
	}

	var replies []string
	err := router.Dispatch(context.Background(), transport.Message{
		Msg: "삼성전자",
		Raw: transport.RawChatLog{ChatID: "room-1"},
	}, func(_ context.Context, r bot.Reply) error {
		replies = append(replies, r.Text)
		return nil
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if stock.executeCalls.Load() != 0 {
		t.Fatalf("execute calls = %d, want 0", stock.executeCalls.Load())
	}
	if len(replies) != 1 || replies[0] != "이 방에서는 사용할 수 없는 기능입니다." {
		t.Fatalf("replies = %v", replies)
	}
}

func TestRouterDispatch_UnmatchedPlainTextRecordsEvent(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)
	recorder := &recordingRecorder{}
	router.SetMetricsRecorder(recorder)

	var replies []string
	err := router.Dispatch(context.Background(), transport.Message{Msg: "안녕"}, func(_ context.Context, r bot.Reply) error {
		replies = append(replies, r.Text)
		return nil
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(replies) != 0 {
		t.Fatalf("replies = %v, want none", replies)
	}
	if got := recorder.eventNames(); len(got) != 2 || got[0] != metrics.EventMessageReceived || got[1] != metrics.EventUnmatchedMessage {
		t.Fatalf("event names = %v", got)
	}
}

func TestRouterDispatch_JamoOnlyInputFiltered(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)
	recorder := &recordingRecorder{}
	router.SetMetricsRecorder(recorder)

	// Register handlers that would match these jamo inputs if not filtered.
	coin := &stubQueryHandler{
		name:        "코인",
		executeText: "coin execute",
		bareMatches: map[string][]string{"ㅋ": {"ㅋ"}},
	}
	finance := &stubQueryHandler{
		name:        "환율",
		executeText: "finance execute",
		bareMatches: map[string][]string{"ㅎㅇ": {"ㅎㅇ"}},
	}
	if err := router.Register(coin); err != nil {
		t.Fatalf("register coin: %v", err)
	}
	if err := router.Register(finance); err != nil {
		t.Fatalf("register finance: %v", err)
	}

	jamoInputs := []string{"ㅎㅇ", "ㅇㅎ", "ㅈ", "ㅋ", "ㅋㅋㅋ", "ㄷㄷ", "ㅁㅊㄴ", "ㅎ ㅇ", "ㅏㅏ"}
	for _, input := range jamoInputs {
		t.Run(input, func(t *testing.T) {
			var replies []string
			err := router.Dispatch(context.Background(), transport.Message{Msg: input}, func(_ context.Context, r bot.Reply) error {
				replies = append(replies, r.Text)
				return nil
			})
			if err != nil {
				t.Fatalf("dispatch(%q): %v", input, err)
			}
			if len(replies) != 0 {
				t.Fatalf("dispatch(%q) got reply %v, want none", input, replies)
			}
		})
	}

	// Verify execute was never called.
	if coin.executeCalls.Load() != 0 {
		t.Fatalf("coin execute calls = %d, want 0", coin.executeCalls.Load())
	}
	if finance.executeCalls.Load() != 0 {
		t.Fatalf("finance execute calls = %d, want 0", finance.executeCalls.Load())
	}
}

func TestRouterDispatch_StopwordFiltersChatExpressions(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)
	stock := &stubQueryHandler{
		name:        "주식",
		executeText: "stock execute",
		bareMatches: map[string][]string{
			"헐":   {"헐"},
			"대박":  {"대박"},
			"레전드": {"레전드"},
		},
	}
	if err := router.Register(stock); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Syllable-based stopwords (caught by bareQueryStopword)
	stopwords := []string{"헐", "대박", "레전드"}
	for _, word := range stopwords {
		t.Run(word, func(t *testing.T) {
			var replies []string
			err := router.Dispatch(context.Background(), transport.Message{Msg: word}, func(_ context.Context, r bot.Reply) error {
				replies = append(replies, r.Text)
				return nil
			})
			if err != nil {
				t.Fatalf("dispatch(%q): %v", word, err)
			}
			if len(replies) != 0 {
				t.Fatalf("dispatch(%q) got reply %v, want none", word, replies)
			}
		})
	}
}

func TestRouterDispatch_ThemeShapedBareQueryUsesStockHandler(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)
	stock := &stubQueryHandler{
		name:        "주식",
		executeText: "stock theme execute",
	}
	if err := router.Register(stock); err != nil {
		t.Fatalf("register: %v", err)
	}

	var replies []string
	err := router.Dispatch(context.Background(), transport.Message{Msg: "네온가스 관련주"}, func(_ context.Context, r bot.Reply) error {
		replies = append(replies, r.Text)
		return nil
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if stock.executeCalls.Load() != 1 {
		t.Fatalf("execute calls = %d, want 1", stock.executeCalls.Load())
	}
	if got := len(stock.lastArgs); got != 1 || stock.lastArgs[0] != "네온가스 관련주" {
		t.Fatalf("args = %v, want [네온가스 관련주]", stock.lastArgs)
	}
	if len(replies) != 1 || replies[0] != "stock theme execute" {
		t.Fatalf("replies = %v", replies)
	}
}

func TestRouterDispatch_StopwordDoesNotResurrectAsAutoFallback(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)
	handler := &stubAutoFallbackHandler{
		stubQueryHandler: stubQueryHandler{
			name:        "주식",
			executeText: "auto stock execute",
		},
		candidateMatch: func(_ string) bool { return true },
	}
	if err := router.Register(handler); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := router.AddAutoFallback(handler); err != nil {
		t.Fatalf("add auto fallback: %v", err)
	}

	var replies []string
	err := router.Dispatch(context.Background(), transport.Message{Msg: "네"}, func(_ context.Context, r bot.Reply) error {
		replies = append(replies, r.Text)
		return nil
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if handler.handleCalls.Load() != 0 {
		t.Fatalf("handle calls = %d, want 0", handler.handleCalls.Load())
	}
	if len(replies) != 0 {
		t.Fatalf("replies = %v, want none", replies)
	}
}

func TestRouterDispatch_ExplicitOnlyIgnoresConversationalAutoCandidate(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)
	catalog := intent.DefaultCatalog()
	manager := bot.NewAutoQueryManager(catalog, bot.NewAutoQueryStore(store.NewMemoryStore()), bot.AutoQueryPolicy{
		Mode:              bot.AutoQueryModeExplicitOnly,
		AllowedHandlers:   []string{"coin"},
		BudgetPerHour:     30,
		CooldownWindow:    time.Second,
		DegradationTarget: bot.AutoQueryModeExplicitOnly,
	})
	router.SetAutoQueryManager(manager)

	handler := &stubAutoFallbackHandler{
		stubQueryHandler: stubQueryHandler{
			name:        "코인",
			executeText: "auto coin execute",
		},
		candidateMatch: func(content string) bool { return content == "오늘 비트 가격" },
	}
	if err := router.Register(handler); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := router.AddAutoFallback(handler); err != nil {
		t.Fatalf("add auto fallback: %v", err)
	}

	var replies []string
	err := router.Dispatch(context.Background(), transport.Message{
		Msg: "오늘 비트 가격",
		Raw: transport.RawChatLog{ChatID: "room-1"},
	}, func(_ context.Context, r bot.Reply) error {
		replies = append(replies, r.Text)
		return nil
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if handler.handleCalls.Load() != 0 {
		t.Fatalf("handle calls = %d, want 0", handler.handleCalls.Load())
	}
	if len(replies) != 0 {
		t.Fatalf("replies = %v, want none", replies)
	}
}

func TestRouterDispatch_DeterministicFallbackRunsBeforeAutoFallback(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)
	recorder := &recordingRecorder{}
	router.SetMetricsRecorder(recorder)

	deterministic := &stubFallbackHandler{
		name: "유튜브",
		handleFn: func(cmd bot.CommandContext) error {
			if cmd.Source != string(metrics.CommandSourceDeterministic) {
				t.Fatalf("source = %s, want deterministic", cmd.Source)
			}
			return bot.ErrHandled
		},
	}
	auto := &stubFallbackHandler{
		name: "코인",
		matchAuto: func(content string) bool {
			return content == "https://youtu.be/demo"
		},
		handleFn: func(bot.CommandContext) error {
			t.Fatal("auto fallback should not run when deterministic fallback handles the message")
			return nil
		},
	}

	if err := router.AddDeterministicFallback(deterministic); err != nil {
		t.Fatalf("add deterministic fallback: %v", err)
	}
	if err := router.AddAutoFallback(auto); err != nil {
		t.Fatalf("add auto fallback: %v", err)
	}

	err := router.Dispatch(context.Background(), transport.Message{Msg: "https://youtu.be/demo"}, func(_ context.Context, r bot.Reply) error {
		t.Fatalf("unexpected reply: %+v", r)
		return nil
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if deterministic.handleCalls.Load() != 1 {
		t.Fatalf("deterministic calls = %d, want 1", deterministic.handleCalls.Load())
	}
	if auto.handleCalls.Load() != 0 {
		t.Fatalf("auto calls = %d, want 0", auto.handleCalls.Load())
	}

	events := recorder.snapshot()
	if len(events) != 3 {
		t.Fatalf("event count = %d, want 3", len(events))
	}
	if events[1].CommandSource != metrics.CommandSourceDeterministic {
		t.Fatalf("command source = %s, want deterministic", events[1].CommandSource)
	}
}

func TestRouterDispatch_AutoFallbackPolicySkipRecordsSilentOutcome(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)
	recorder := &recordingRecorder{}
	router.SetMetricsRecorder(recorder)

	catalog := intent.DefaultCatalog()
	policyStore := bot.NewAutoQueryStore(store.NewMemoryStore())
	manager := bot.NewAutoQueryManager(catalog, policyStore, bot.DefaultAutoQueryPolicy(catalog))
	if err := manager.SetRoomPolicy(context.Background(), "room-2", bot.AutoQueryPolicy{
		Mode:              bot.AutoQueryModeExplicitOnly,
		AllowedHandlers:   []string{"coin"},
		BudgetPerHour:     1,
		CooldownWindow:    time.Second,
		DegradationTarget: bot.AutoQueryModeExplicitOnly,
	}); err != nil {
		t.Fatalf("set room policy: %v", err)
	}
	router.SetAutoQueryManager(manager)

	auto := &stubFallbackHandler{
		name: "코인",
		matchAuto: func(content string) bool {
			return content == "오늘 비트"
		},
		handleFn: func(bot.CommandContext) error {
			t.Fatal("auto fallback should not execute when room policy is explicit-only")
			return nil
		},
	}
	if err := router.AddAutoFallback(auto); err != nil {
		t.Fatalf("add auto fallback: %v", err)
	}

	err := router.Dispatch(context.Background(), transport.Message{
		Msg: "오늘 비트",
		Raw: transport.RawChatLog{ChatID: "room-2"},
	}, func(_ context.Context, r bot.Reply) error {
		t.Fatalf("unexpected reply: %+v", r)
		return nil
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if auto.handleCalls.Load() != 0 {
		t.Fatalf("auto calls = %d, want 0", auto.handleCalls.Load())
	}

	if got := recorder.eventNames(); len(got) != 2 || got[0] != metrics.EventMessageReceived || got[1] != metrics.EventPolicySkip {
		t.Fatalf("event names = %v", got)
	}
}
