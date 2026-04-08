package bot

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/metrics"
	"github.com/shift-click/masterbot/internal/transport"
)

type middlewareRecorder struct {
	events []metrics.Event
}

func (r *middlewareRecorder) Record(_ context.Context, event metrics.Event) {
	r.events = append(r.events, event)
}

type notifierSpy struct {
	command    string
	errorClass string
	errorMsg   string
	calls      int
}

func (n *notifierSpy) Notify(command, errorClass, errorMsg string) {
	n.command = command
	n.errorClass = errorClass
	n.errorMsg = errorMsg
	n.calls++
}

func middlewareCommandContext(source string, now time.Time, replyFn ReplyFunc) CommandContext {
	if replyFn == nil {
		replyFn = func(context.Context, Reply) error { return nil }
	}
	return CommandContext{
		Command: "stock",
		Source:  source,
		Message: transport.Message{
			Room: "운영방",
			Raw: transport.RawChatLog{
				ID:     "req-1",
				ChatID: "room-1",
				UserID: "user-1",
			},
		},
		Reply: replyFn,
		Now: func() time.Time {
			return now
		},
	}
}

func TestNewCommandMetricEvent(t *testing.T) {
	t.Parallel()

	success := true
	cmd := CommandContext{
		Command: "stock",
		Source:  string(metrics.CommandSourceAuto),
		Message: transport.Message{
			Room: "운영방",
			Raw: transport.RawChatLog{
				ID:     "req-1",
				ChatID: "room-1",
				UserID: "user-1",
			},
		},
	}

	event := newCommandMetricEvent(time.Unix(30, 0), cmd, metrics.EventCommandSucceeded, commandEventOptions{
		success:     &success,
		errorClass:  "none",
		latency:     15 * time.Millisecond,
		rateLimited: true,
	})

	if event.RequestID != "req-1" || event.CommandID != "stock" {
		t.Fatalf("unexpected event routing fields: %+v", event)
	}
	if event.CommandSource != metrics.CommandSourceAuto {
		t.Fatalf("command source = %q", event.CommandSource)
	}
	if event.RoomName != "운영방" || event.RawUserID != "user-1" {
		t.Fatalf("unexpected audience fields: %+v", event)
	}
	if event.Success == nil || !*event.Success || !event.RateLimited {
		t.Fatalf("unexpected status fields: %+v", event)
	}
	if event.ErrorClass != "none" || event.Latency != 15*time.Millisecond {
		t.Fatalf("unexpected error/latency fields: %+v", event)
	}
}

func TestMiddlewareConstructorsApplyDefaults(t *testing.T) {
	t.Parallel()

	logging := NewLoggingMiddleware(nil, nil).(loggingMiddleware)
	if logging.logger == nil {
		t.Fatal("expected default logger to be configured")
	}

	withNotifier := NewLoggingMiddlewareWithNotifier(nil, nil, &notifierSpy{}).(loggingMiddleware)
	if withNotifier.logger == nil {
		t.Fatal("expected notifier middleware to use default logger")
	}

	rateLimited := NewRateLimitMiddleware(0, 0, nil).(*rateLimitMiddleware)
	if rateLimited.limit != 3 {
		t.Fatalf("default limit = %d, want 3", rateLimited.limit)
	}
	if rateLimited.window != time.Second {
		t.Fatalf("default window = %s, want %s", rateLimited.window, time.Second)
	}

	errMiddleware := NewErrorMiddleware(nil).(errorMiddleware)
	if errMiddleware.logger == nil {
		t.Fatal("expected default error logger to be configured")
	}
}

func TestLoggingMiddlewareRecordsSuccessFollowupsForAutoSource(t *testing.T) {
	t.Parallel()

	recorder := &middlewareRecorder{}
	middleware := NewLoggingMiddleware(slog.Default(), recorder).(loggingMiddleware)
	cmd := middlewareCommandContext(string(metrics.CommandSourceAuto), time.Unix(10, 0), nil)

	err := middleware.Wrap(func(context.Context, CommandContext) error { return nil })(context.Background(), cmd)
	if err != nil {
		t.Fatalf("wrapped handler: %v", err)
	}

	if len(recorder.events) != 4 {
		t.Fatalf("event count = %d, want 4", len(recorder.events))
	}
	if recorder.events[0].EventName != metrics.EventCommandSucceeded {
		t.Fatalf("first event = %s", recorder.events[0].EventName)
	}
	if recorder.events[1].EventName != metrics.EventEngagement {
		t.Fatalf("second event = %s", recorder.events[1].EventName)
	}
	if recorder.events[2].EventName != metrics.EventConversion {
		t.Fatalf("third event = %s", recorder.events[2].EventName)
	}
	if recorder.events[3].EventName != metrics.EventRetentionReturn {
		t.Fatalf("fourth event = %s", recorder.events[3].EventName)
	}
}

func TestLoggingMiddlewareFailureNotifiesAndRecordsChurn(t *testing.T) {
	t.Parallel()

	recorder := &middlewareRecorder{}
	notifier := &notifierSpy{}
	middleware := NewLoggingMiddlewareWithNotifier(nil, recorder, notifier).(loggingMiddleware)
	cmd := middlewareCommandContext(string(metrics.CommandSourceExplicit), time.Unix(20, 0), nil)

	err := middleware.Wrap(func(context.Context, CommandContext) error {
		return context.DeadlineExceeded
	})(context.Background(), cmd)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wrapped handler error = %v", err)
	}

	if len(recorder.events) != 2 {
		t.Fatalf("event count = %d, want 2", len(recorder.events))
	}
	if recorder.events[0].EventName != metrics.EventCommandFailed {
		t.Fatalf("first event = %s", recorder.events[0].EventName)
	}
	if recorder.events[0].ErrorClass != "deadline_exceeded" {
		t.Fatalf("error class = %q", recorder.events[0].ErrorClass)
	}
	if recorder.events[1].EventName != metrics.EventChurnSignal {
		t.Fatalf("second event = %s", recorder.events[1].EventName)
	}
	if notifier.calls != 1 || notifier.command != "stock" || notifier.errorClass != "deadline_exceeded" {
		t.Fatalf("unexpected notifier calls: %+v", notifier)
	}
}

func TestRateLimitMiddlewareBlocksAndRecordsEvent(t *testing.T) {
	t.Parallel()

	recorder := &middlewareRecorder{}
	middleware := NewRateLimitMiddleware(1, time.Minute, recorder).(*rateLimitMiddleware)
	replies := make([]Reply, 0, 1)
	cmd := middlewareCommandContext(string(metrics.CommandSourceExplicit), time.Unix(30, 0), func(_ context.Context, reply Reply) error {
		replies = append(replies, reply)
		return nil
	})

	nextCalls := 0
	wrapped := middleware.Wrap(func(context.Context, CommandContext) error {
		nextCalls++
		return nil
	})
	if err := wrapped(context.Background(), cmd); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := wrapped(context.Background(), cmd); err != nil {
		t.Fatalf("second call: %v", err)
	}

	if nextCalls != 1 {
		t.Fatalf("next calls = %d, want 1", nextCalls)
	}
	if len(replies) != 1 || replies[0].Text != "잠시 후 다시 시도해주세요." {
		t.Fatalf("unexpected replies: %+v", replies)
	}
	if len(recorder.events) != 1 || recorder.events[0].EventName != metrics.EventRateLimited {
		t.Fatalf("unexpected recorder events: %+v", recorder.events)
	}
	if !recorder.events[0].RateLimited {
		t.Fatalf("rate-limited flag not set: %+v", recorder.events[0])
	}
}

func TestErrorMiddlewareRecoversPanic(t *testing.T) {
	t.Parallel()

	middleware := NewErrorMiddleware(nil).(errorMiddleware)
	replies := make([]Reply, 0, 1)
	cmd := middlewareCommandContext("", time.Unix(40, 0), func(_ context.Context, reply Reply) error {
		replies = append(replies, reply)
		return nil
	})

	err := middleware.Wrap(func(context.Context, CommandContext) error {
		panic("boom")
	})(context.Background(), cmd)
	if err != nil {
		t.Fatalf("panic recovery returned error: %v", err)
	}
	if len(replies) != 1 || replies[0].Text != "요청을 처리하는 중 문제가 발생했습니다." {
		t.Fatalf("unexpected replies: %+v", replies)
	}
}

func TestPruneWindowDropsExpiredHits(t *testing.T) {
	t.Parallel()

	now := time.Unix(50, 0)
	hits := []time.Time{
		now.Add(-2 * time.Minute),
		now.Add(-30 * time.Second),
		now.Add(-5 * time.Second),
	}

	filtered := pruneWindow(hits, now, time.Minute)
	if len(filtered) != 2 {
		t.Fatalf("filtered len = %d, want 2", len(filtered))
	}
	if !filtered[0].Equal(now.Add(-30*time.Second)) || !filtered[1].Equal(now.Add(-5*time.Second)) {
		t.Fatalf("filtered hits = %v", filtered)
	}
}

func TestRateLimitMiddlewareCleanupEvictsIdleLimiters(t *testing.T) {
	t.Parallel()

	middleware := NewRateLimitMiddleware(3, time.Second, nil).(*rateLimitMiddleware)

	// Seed a limiter entry with old lastSeen.
	middleware.limiters.Store("old:cmd", &userLimiter{
		limiter:  nil,
		lastSeen: time.Now().Add(-2 * time.Hour),
	})
	middleware.limiters.Store("fresh:cmd", &userLimiter{
		limiter:  nil,
		lastSeen: time.Now(),
	})

	// Run one cleanup cycle inline.
	now := time.Now()
	middleware.limiters.Range(func(key, value any) bool {
		entry := value.(*userLimiter)
		if now.Sub(entry.lastSeen) > 1*time.Hour {
			middleware.limiters.Delete(key)
		}
		return true
	})

	if _, ok := middleware.limiters.Load("old:cmd"); ok {
		t.Fatal("expected old limiter to be evicted")
	}
	if _, ok := middleware.limiters.Load("fresh:cmd"); !ok {
		t.Fatal("expected fresh limiter to be retained")
	}
}
