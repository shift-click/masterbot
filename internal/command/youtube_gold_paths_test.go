package command

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
)

type runtimeAdapterStub struct {
	mu       sync.Mutex
	requests []transport.ReplyRequest
	ch       chan transport.ReplyRequest
}

func newRuntimeAdapterStub() *runtimeAdapterStub {
	return &runtimeAdapterStub{ch: make(chan transport.ReplyRequest, 16)}
}

func (r *runtimeAdapterStub) Start(context.Context, func(context.Context, transport.Message) error) error {
	return nil
}

func (r *runtimeAdapterStub) Reply(_ context.Context, req transport.ReplyRequest) error {
	r.mu.Lock()
	r.requests = append(r.requests, req)
	r.mu.Unlock()
	select {
	case r.ch <- req:
	default:
	}
	return nil
}

func (r *runtimeAdapterStub) Close() error { return nil }

func (r *runtimeAdapterStub) waitContains(t *testing.T, contains string, timeout time.Duration) transport.ReplyRequest {
	t.Helper()
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case req := <-r.ch:
			if text, ok := req.Data.(string); ok && strings.Contains(text, contains) {
				return req
			}
		case <-timer.C:
			t.Fatalf("did not receive reply containing %q", contains)
		}
	}
}

func TestYouTubeHandlerValidationAndQueueLimit(t *testing.T) {
	h := NewYouTubeHandler(nil, slog.Default())
	h.SetAdapter(newRuntimeAdapterStub())

	replyWith := func(t *testing.T, args []string) string {
		t.Helper()
		var got bot.Reply
		err := h.Execute(context.Background(), bot.CommandContext{
			Args: args,
			Reply: func(_ context.Context, r bot.Reply) error {
				got = r
				return nil
			},
		})
		if err != nil {
			t.Fatalf("Execute error: %v", err)
		}
		return got.Text
	}

	if text := replyWith(t, nil); !strings.Contains(text, "유튜브 URL") {
		t.Fatalf("unexpected empty-url text: %q", text)
	}
	if text := replyWith(t, []string{"not-a-url"}); !strings.Contains(text, "유효한 유튜브 URL") {
		t.Fatalf("unexpected invalid-url text: %q", text)
	}

	for i := 0; i < cap(h.exec.sem); i++ {
		h.exec.sem <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(h.exec.sem); i++ {
			<-h.exec.sem
		}
	}()
	var fullReply bot.Reply
	err := h.startSummarize(context.Background(), bot.CommandContext{
		Message: transport.Message{Raw: transport.RawChatLog{ChatID: "room-1"}},
		Reply: func(_ context.Context, r bot.Reply) error {
			fullReply = r
			return nil
		},
	}, "https://youtu.be/dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("startSummarize error: %v", err)
	}
	if !strings.Contains(fullReply.Text, "요약 처리가 많습니다") {
		t.Fatalf("unexpected queue-limit reply: %q", fullReply.Text)
	}
}

func TestYouTubeHandlerAckAndFallback(t *testing.T) {
	adapter := newRuntimeAdapterStub()
	h := NewYouTubeHandler(nil, slog.Default())
	h.SetAdapter(adapter)

	replyCalled := false
	err := h.Execute(context.Background(), bot.CommandContext{
		Args:    []string{"https://youtu.be/dQw4w9WgXcQ"},
		Message: transport.Message{Raw: transport.RawChatLog{ChatID: "room-yt"}},
		Reply: func(_ context.Context, r bot.Reply) error {
			replyCalled = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if replyCalled {
		t.Fatal("expected no immediate ack reply")
	}

	// Wait for nil-gemini goroutine to panic-recover and release semaphore.
	time.Sleep(100 * time.Millisecond)

	err = h.HandleFallback(context.Background(), bot.CommandContext{
		Message: transport.Message{Msg: "https://youtu.be/dQw4w9WgXcQ", Raw: transport.RawChatLog{ChatID: "room-yt"}},
		Reply: func(context.Context, bot.Reply) error {
			return nil
		},
	})
	if err != bot.ErrHandled {
		t.Fatalf("HandleFallback err = %v, want ErrHandled", err)
	}

	err = h.HandleFallback(context.Background(), bot.CommandContext{
		Message: transport.Message{Msg: "그냥 대화"},
		Reply:   func(context.Context, bot.Reply) error { return nil },
	})
	if err != nil {
		t.Fatalf("HandleFallback non-youtube err = %v", err)
	}

	h.sendError(context.Background(), "room-yt", "오류")
	adapter.waitContains(t, "오류", 0)
}

func TestGoldHandlerExecuteAndFallbackPaths(t *testing.T) {
	provider := providers.NewNaverGold(nil)
	setUnexportedField(t, provider, "gold", &providers.GoldPrice{Metal: "gold", PricePerG: 100, PricePerDon: 375})
	setUnexportedField(t, provider, "silver", &providers.GoldPrice{Metal: "silver", PricePerG: 5, PricePerDon: 18.75})
	setUnexportedField(t, provider, "updatedAt", time.Now())
	setUnexportedField(t, provider, "cacheTTL", 10*time.Minute)

	h := NewGoldHandler(provider, slog.Default())
	if h.SupportsSlashCommands() {
		t.Fatal("gold handler should not support slash commands")
	}
	if h.ID() != "gold" {
		t.Fatalf("unexpected handler id: %s", h.ID())
	}
	if ok := h.MatchAutoQueryCandidate(context.Background(), strings.Repeat("금", 30)); ok {
		t.Fatal("long auto-query should not match")
	}
	if ok := h.MatchAutoQueryCandidate(context.Background(), "금 시세"); !ok {
		t.Fatal("expected auto-query candidate match")
	}

	run := func(ctx context.Context, msg string) bot.Reply {
		var reply bot.Reply
		err := h.Execute(ctx, bot.CommandContext{
			Message: transport.Message{Msg: msg},
			Reply: func(_ context.Context, r bot.Reply) error {
				reply = r
				return nil
			},
		})
		if err != nil {
			t.Fatalf("Execute(%q) error: %v", msg, err)
		}
		return reply
	}

	reply := run(context.Background(), "금 2돈")
	if !strings.Contains(reply.Text, "금") || !strings.Contains(reply.Text, "2") {
		t.Fatalf("unexpected gold reply: %q", reply.Text)
	}

	reply = run(context.Background(), "은 3g")
	if !strings.Contains(reply.Text, "은") || !strings.Contains(reply.Text, "3") {
		t.Fatalf("unexpected silver reply: %q", reply.Text)
	}

	reply = run(context.Background(), "비트")
	if !strings.Contains(reply.Text, "조회할 수 없습니다") {
		t.Fatalf("unexpected invalid-query reply: %q", reply.Text)
	}

	providerErr := providers.NewNaverGold(nil)
	hErr := NewGoldHandler(providerErr, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var errReply bot.Reply
	err := hErr.Execute(ctx, bot.CommandContext{
		Message: transport.Message{Msg: "금"},
		Reply: func(_ context.Context, r bot.Reply) error {
			errReply = r
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Execute canceled error: %v", err)
	}
	if !strings.Contains(errReply.Text, "가져올 수 없습니다") {
		t.Fatalf("unexpected fetch-error reply: %q", errReply.Text)
	}

	err = h.HandleFallback(context.Background(), bot.CommandContext{
		Message: transport.Message{Msg: "금"},
		Reply: func(context.Context, bot.Reply) error {
			return nil
		},
	})
	if err != bot.ErrHandled {
		t.Fatalf("HandleFallback err = %v, want ErrHandled", err)
	}
}
