package bot_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/metrics"
	"github.com/shift-click/masterbot/internal/transport"
)

type spyRecorder struct {
	events []metrics.Event
}

type notifierSpy struct {
	command    string
	errorClass string
	errorMsg   string
	calls      int
}

func (s *spyRecorder) Record(_ context.Context, e metrics.Event) {
	s.events = append(s.events, e)
}

func (s *spyRecorder) Close() error { return nil }

func (n *notifierSpy) Notify(command, errorClass, errorMsg string) {
	n.command = command
	n.errorClass = errorClass
	n.errorMsg = errorMsg
	n.calls++
}

func makeTestCmd(replyFn func(context.Context, bot.Reply) error) bot.CommandContext {
	if replyFn == nil {
		replyFn = func(_ context.Context, _ bot.Reply) error { return nil }
	}
	return bot.CommandContext{
		Command: "stock",
		Message: transport.Message{
			Raw: transport.RawChatLog{ID: "1", ChatID: "room1", UserID: "user1"},
		},
		Reply: replyFn,
		Now:   time.Now,
	}
}

func TestLoggingMiddleware_RecordsFailureForHandledWithFailure(t *testing.T) {
	rec := &spyRecorder{}
	mw := bot.NewLoggingMiddleware(slog.Default(), rec)

	handler := bot.HandlerFunc(func(_ context.Context, _ bot.CommandContext) error {
		return bot.ErrHandledWithFailure
	})
	wrapped := mw.Wrap(handler)
	_ = wrapped(context.Background(), makeTestCmd(nil))

	found := false
	for _, e := range rec.events {
		if e.EventName == metrics.EventCommandFailed {
			found = true
			if e.ErrorClass != "fetch_error" {
				t.Errorf("expected error class 'fetch_error', got %q", e.ErrorClass)
			}
		}
	}
	if !found {
		t.Error("expected EventCommandFailed to be recorded")
	}
}

func TestLoggingMiddleware_DoesNotNotifyInvalidInputFailures(t *testing.T) {
	rec := &spyRecorder{}
	notifier := &notifierSpy{}
	mw := bot.NewLoggingMiddlewareWithNotifier(slog.Default(), rec, notifier)

	handler := bot.HandlerFunc(func(_ context.Context, _ bot.CommandContext) error {
		return bot.NewHandledFailure("invalid_input", false, "invalid stock input: 123456", nil)
	})
	wrapped := mw.Wrap(handler)
	_ = wrapped(context.Background(), makeTestCmd(nil))

	if notifier.calls != 0 {
		t.Fatalf("unexpected notifier calls: %+v", notifier)
	}
	found := false
	for _, e := range rec.events {
		if e.EventName == metrics.EventCommandFailed {
			found = true
			if e.ErrorClass != "invalid_input" {
				t.Fatalf("error class = %q, want invalid_input", e.ErrorClass)
			}
		}
	}
	if !found {
		t.Fatal("expected EventCommandFailed to be recorded")
	}
}

func TestErrorMiddleware_SkipsGenericMessageForHandledWithFailure(t *testing.T) {
	mw := bot.NewErrorMiddleware(slog.Default())

	replyCalled := false
	handler := bot.HandlerFunc(func(_ context.Context, _ bot.CommandContext) error {
		return bot.ErrHandledWithFailure
	})
	wrapped := mw.Wrap(handler)

	cmd := makeTestCmd(func(_ context.Context, _ bot.Reply) error {
		replyCalled = true
		return nil
	})
	err := wrapped(context.Background(), cmd)

	if replyCalled {
		t.Error("errorMiddleware should NOT send generic message for ErrHandledWithFailure")
	}
	if !errors.Is(err, bot.ErrHandledWithFailure) {
		t.Errorf("expected ErrHandledWithFailure to pass through, got %v", err)
	}
}

func TestErrorMiddleware_SendsGenericMessageForOtherErrors(t *testing.T) {
	mw := bot.NewErrorMiddleware(slog.Default())

	replyCalled := false
	handler := bot.HandlerFunc(func(_ context.Context, _ bot.CommandContext) error {
		return errors.New("some error")
	})
	wrapped := mw.Wrap(handler)

	cmd := makeTestCmd(func(_ context.Context, _ bot.Reply) error {
		replyCalled = true
		return nil
	})
	_ = wrapped(context.Background(), cmd)

	if !replyCalled {
		t.Error("errorMiddleware should send generic message for regular errors")
	}
}
