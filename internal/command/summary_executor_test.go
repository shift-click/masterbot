package command

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/transport"
)

func TestSummaryExecutorWaitsForJobsOnShutdown(t *testing.T) {
	t.Parallel()

	exec := NewSummaryExecutor(slog.Default(), 1, time.Second)
	rootCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- exec.Run(rootCtx)
	}()
	deadline := time.Now().Add(time.Second)
	for exec.currentRootContext() != rootCtx {
		if time.Now().After(deadline) {
			t.Fatal("executor did not bind runtime context")
		}
		time.Sleep(time.Millisecond)
	}

	jobCanceled := make(chan struct{})
	if err := exec.Submit(context.Background(), "test", func(ctx context.Context) {
		<-ctx.Done()
		close(jobCanceled)
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	cancel()

	select {
	case <-jobCanceled:
	case <-time.After(time.Second):
		t.Fatal("job did not observe shutdown cancellation")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("executor did not wait for in-flight job")
	}
}

func TestSummaryExecutorRejectsUnavailableRoot(t *testing.T) {
	t.Parallel()

	exec := NewSummaryExecutor(slog.Default(), 1, time.Second)
	rootCtx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() {
		done <- exec.Run(rootCtx)
	}()
	deadline := time.Now().Add(time.Second)
	for exec.currentRootContext() != rootCtx {
		if time.Now().After(deadline) {
			t.Fatal("executor did not bind canceled runtime context")
		}
		time.Sleep(time.Millisecond)
	}
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	err := exec.Submit(context.Background(), "test", func(context.Context) {})
	if err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("Submit() error = %v, want unavailable", err)
	}
}

func TestYouTubeHandlerReportsShutdownExecutor(t *testing.T) {
	t.Parallel()

	h := NewYouTubeHandler(nil, slog.Default())
	exec := NewSummaryExecutor(slog.Default(), 1, time.Second)
	rootCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- exec.Run(rootCtx)
	}()
	cancel()
	<-done
	h.SetExecutor(exec)

	var reply bot.Reply
	err := h.Execute(context.Background(), bot.CommandContext{
		Args:    []string{"https://youtu.be/dQw4w9WgXcQ"},
		Message: transport.Message{Raw: transport.RawChatLog{ChatID: "room-yt"}},
		Reply: func(_ context.Context, r bot.Reply) error {
			reply = r
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(reply.Text, "종료 중") {
		t.Fatalf("reply = %q, want shutdown message", reply.Text)
	}
}

func TestURLSummaryHandlerReportsShutdownExecutor(t *testing.T) {
	t.Parallel()

	h := NewURLSummaryHandler(nil, slog.Default())
	exec := NewSummaryExecutor(slog.Default(), 1, time.Second)
	rootCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- exec.Run(rootCtx)
	}()
	cancel()
	<-done
	h.SetExecutor(exec)

	var reply bot.Reply
	err := h.Execute(context.Background(), bot.CommandContext{
		Args:    []string{"https://example.com/article"},
		Message: transport.Message{Raw: transport.RawChatLog{ChatID: "room-url"}},
		Reply: func(_ context.Context, r bot.Reply) error {
			reply = r
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(reply.Text, "종료 중") {
		t.Fatalf("reply = %q, want shutdown message", reply.Text)
	}
}
