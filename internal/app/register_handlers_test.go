package app

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/command"
	"github.com/shift-click/masterbot/internal/commandmeta"
	"github.com/shift-click/masterbot/internal/coupang"
	"github.com/shift-click/masterbot/internal/intent"
	"github.com/shift-click/masterbot/internal/scraper"
	"github.com/shift-click/masterbot/internal/store"
	"github.com/shift-click/masterbot/internal/transport"
)

type stubCoupangLookup struct {
	result *scraper.CoupangLookupResult
}

func (s stubCoupangLookup) Lookup(_ context.Context, _ string, _ ...coupang.LookupOption) (*scraper.CoupangLookupResult, error) {
	return s.result, nil
}

type stubScopedFallbackHandler struct {
	descriptor  commandmeta.Descriptor
	handleCalls atomic.Int32
	handleFn    func(bot.CommandContext) error
}

func (h *stubScopedFallbackHandler) Name() string { return h.descriptor.Name }

func (h *stubScopedFallbackHandler) Aliases() []string {
	return append([]string(nil), h.descriptor.SlashAliases...)
}

func (h *stubScopedFallbackHandler) Description() string { return h.descriptor.Description }

func (h *stubScopedFallbackHandler) Execute(context.Context, bot.CommandContext) error { return nil }

func (h *stubScopedFallbackHandler) Descriptor() commandmeta.Descriptor { return h.descriptor }

func (h *stubScopedFallbackHandler) HandleFallback(_ context.Context, cmd bot.CommandContext) error {
	h.handleCalls.Add(1)
	if h.handleFn != nil {
		return h.handleFn(cmd)
	}
	return nil
}

func TestRegisterHandlersRegistersCoupangAsDeterministicFallback(t *testing.T) {
	t.Parallel()

	router := bot.NewRouter("/", nil, bot.NewRegistry(intent.DefaultCatalog()), nil)
	handler := command.NewCoupangHandler(stubCoupangLookup{
		result: &scraper.CoupangLookupResult{
			Product: store.CoupangProductRecord{
				ProductID: "8616986273",
				Name:      "테스트 쿠팡 상품",
				Snapshot: store.CoupangSnapshot{
					TrackID:    "8616986273",
					Price:      14900,
					LastSeenAt: time.Now().Add(-time.Hour),
				},
			},
			History: []store.PricePoint{
				{Price: 15900, FetchedAt: time.Now().Add(-48 * time.Hour)},
				{Price: 14900, FetchedAt: time.Now().Add(-time.Hour)},
			},
			SampleCount:     2,
			DistinctDays:    2,
			HistorySpanDays: 2,
			StatsEligible:   false,
		},
	}, nil)

	if err := registerFeatureRuntimes(router, []featureRuntime{{name: "coupang", handlers: []bot.Handler{handler}}}); err != nil {
		t.Fatalf("register feature runtimes: %v", err)
	}

	var reply bot.Reply
	err := router.Dispatch(context.Background(), transport.Message{
		Msg: "https://www.coupang.com/vp/products/8616986273?itemId=25224712537&vendorItemId=93359412302",
		Raw: transport.RawChatLog{
			ChatID: "room-1",
		},
	}, func(_ context.Context, r bot.Reply) error {
		reply = r
		return nil
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if reply.Type != transport.ReplyTypeText {
		t.Fatalf("reply type = %s, want text", reply.Type)
	}
	if !strings.Contains(reply.Text, "현재가 14,900원") {
		t.Fatalf("reply text %q does not contain coupang response", reply.Text)
	}
}

func TestRegisterHandlerUsesDescriptorFallbackScope(t *testing.T) {
	t.Parallel()

	router := bot.NewRouter("/", nil, bot.NewRegistry(intent.DefaultCatalog()), nil)
	handler := &stubScopedFallbackHandler{
		descriptor: commandmeta.Must("youtube"),
		handleFn: func(cmd bot.CommandContext) error {
			if cmd.Message.Msg != "https://youtu.be/demo" {
				t.Fatalf("message = %q", cmd.Message.Msg)
			}
			return bot.ErrHandled
		},
	}

	if err := registerHandler(router, handler); err != nil {
		t.Fatalf("register handler: %v", err)
	}

	err := router.Dispatch(context.Background(), transport.Message{
		Msg: "https://youtu.be/demo",
		Raw: transport.RawChatLog{ChatID: "room-1"},
	}, func(context.Context, bot.Reply) error {
		return nil
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if handler.handleCalls.Load() != 1 {
		t.Fatalf("handle calls = %d, want 1", handler.handleCalls.Load())
	}
}

func TestRegisterHandlerRejectsFallbackWithoutScopeMetadata(t *testing.T) {
	t.Parallel()

	router := bot.NewRouter("/", nil, bot.NewRegistry(intent.DefaultCatalog()), nil)
	handler := &stubScopedFallbackHandler{
		descriptor: commandmeta.Must("help"),
		handleFn: func(bot.CommandContext) error {
			return bot.ErrHandled
		},
	}

	err := registerHandler(router, handler)
	if err == nil {
		t.Fatal("expected missing fallback scope error")
	}
	if !strings.Contains(err.Error(), "missing fallback scope metadata") {
		t.Fatalf("error = %v", err)
	}
}

func TestRegisterHandlersIncludesHiddenFallbackHandlers(t *testing.T) {
	t.Parallel()

	router := bot.NewRouter("/", nil, bot.NewRegistry(intent.DefaultCatalog()), nil)
	if err := registerFeatureRuntimes(router, []featureRuntime{{
		name:      "fallbacks",
		fallbacks: []bot.FallbackHandler{command.NewForexConvertHandler(nil), command.NewCalcHandler()},
	}}); err != nil {
		t.Fatalf("register feature runtimes: %v", err)
	}

	if got := len(router.Registry().Fallbacks()); got != 2 {
		t.Fatalf("fallback count = %d, want 2", got)
	}

	var reply bot.Reply
	err := router.Dispatch(context.Background(), transport.Message{
		Msg: "100*2",
		Raw: transport.RawChatLog{ChatID: "room-1"},
	}, func(_ context.Context, r bot.Reply) error {
		reply = r
		return nil
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if reply.Type != transport.ReplyTypeText || !strings.Contains(reply.Text, "200") {
		t.Fatalf("reply = %+v, want calc result", reply)
	}
}

func TestRegisterHandlerSkipsTypedNilHandler(t *testing.T) {
	t.Parallel()

	router := bot.NewRouter("/", nil, bot.NewRegistry(intent.DefaultCatalog()), nil)
	var nilYouTube *command.YouTubeHandler
	if err := registerHandler(router, nilYouTube); err != nil {
		t.Fatalf("register typed nil handler: %v", err)
	}
}
