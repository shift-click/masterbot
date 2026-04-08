package transport

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type stubAdapter struct {
	id          string
	startFunc   func(context.Context, func(context.Context, Message) error) error
	replyFunc   func(context.Context, ReplyRequest) error
	closeFunc   func() error
	startCalled atomic.Bool
	closeCalled atomic.Bool
}

func (s *stubAdapter) Start(ctx context.Context, onMessage func(context.Context, Message) error) error {
	s.startCalled.Store(true)
	if s.startFunc != nil {
		return s.startFunc(ctx, onMessage)
	}
	<-ctx.Done()
	return ctx.Err()
}

func (s *stubAdapter) Reply(ctx context.Context, req ReplyRequest) error {
	if s.replyFunc != nil {
		return s.replyFunc(ctx, req)
	}
	return nil
}

func (s *stubAdapter) Close() error {
	s.closeCalled.Store(true)
	if s.closeFunc != nil {
		return s.closeFunc()
	}
	return nil
}

func TestNewCompositeAdapterRejectsEmpty(t *testing.T) {
	t.Parallel()
	_, err := NewCompositeAdapter(map[string]RuntimeAdapter{})
	if err == nil {
		t.Fatal("expected error for empty adapters")
	}
}

func TestCompositeAdapterStartsAllAdapters(t *testing.T) {
	t.Parallel()

	a1 := &stubAdapter{id: "a1"}
	a2 := &stubAdapter{id: "a2"}

	comp, err := NewCompositeAdapter(map[string]RuntimeAdapter{
		"a1": a1,
		"a2": a2,
	})
	if err != nil {
		t.Fatalf("NewCompositeAdapter: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- comp.Start(ctx, func(context.Context, Message) error { return nil }) }()

	// Wait for both adapters to start.
	deadline := time.After(time.Second)
	for {
		if a1.startCalled.Load() && a2.startCalled.Load() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for adapters to start")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	cancel()
	<-done
}

func TestCompositeAdapterTagsOrigin(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var received []Message

	a1 := &stubAdapter{
		id: "main",
		startFunc: func(ctx context.Context, onMessage func(context.Context, Message) error) error {
			_ = onMessage(ctx, Message{Msg: "hello from main"})
			<-ctx.Done()
			return ctx.Err()
		},
	}
	a2 := &stubAdapter{
		id: "sub",
		startFunc: func(ctx context.Context, onMessage func(context.Context, Message) error) error {
			_ = onMessage(ctx, Message{Msg: "hello from sub"})
			<-ctx.Done()
			return ctx.Err()
		},
	}

	comp, _ := NewCompositeAdapter(map[string]RuntimeAdapter{
		"main": a1,
		"sub":  a2,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- comp.Start(ctx, func(_ context.Context, msg Message) error {
			mu.Lock()
			received = append(received, msg)
			mu.Unlock()
			return nil
		})
	}()

	// Wait for messages.
	deadline := time.After(time.Second)
	for {
		mu.Lock()
		n := len(received)
		mu.Unlock()
		if n >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for messages")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()

	origins := make(map[string]bool)
	for _, msg := range received {
		origins[msg.Origin.AdapterID] = true
	}
	if !origins["main"] || !origins["sub"] {
		t.Fatalf("expected origins main+sub, got %v", origins)
	}
}

func TestCompositeAdapterRoutesReply(t *testing.T) {
	t.Parallel()

	var replyTo string
	a1 := &stubAdapter{
		id: "main",
		replyFunc: func(_ context.Context, req ReplyRequest) error {
			replyTo = "main:" + req.Room
			return nil
		},
	}
	a2 := &stubAdapter{
		id: "sub",
		replyFunc: func(_ context.Context, req ReplyRequest) error {
			replyTo = "sub:" + req.Room
			return nil
		},
	}

	comp, _ := NewCompositeAdapter(map[string]RuntimeAdapter{
		"main": a1,
		"sub":  a2,
	})

	if err := comp.Reply(context.Background(), ReplyRequest{AdapterID: "main", Room: "r1"}); err != nil {
		t.Fatalf("Reply to main: %v", err)
	}
	if replyTo != "main:r1" {
		t.Fatalf("expected main:r1, got %q", replyTo)
	}

	if err := comp.Reply(context.Background(), ReplyRequest{AdapterID: "sub", Room: "r2"}); err != nil {
		t.Fatalf("Reply to sub: %v", err)
	}
	if replyTo != "sub:r2" {
		t.Fatalf("expected sub:r2, got %q", replyTo)
	}
}

func TestCompositeAdapterReplyUnknownIDReturnsError(t *testing.T) {
	t.Parallel()

	a1 := &stubAdapter{id: "main"}
	comp, _ := NewCompositeAdapter(map[string]RuntimeAdapter{"main": a1})

	if err := comp.Reply(context.Background(), ReplyRequest{AdapterID: "unknown", Room: "r1"}); err == nil {
		t.Fatal("expected error for unknown adapter ID")
	}
}

func TestCompositeAdapterClosesAll(t *testing.T) {
	t.Parallel()

	a1 := &stubAdapter{id: "a1"}
	a2 := &stubAdapter{id: "a2", closeFunc: func() error { return errors.New("close err") }}

	comp, _ := NewCompositeAdapter(map[string]RuntimeAdapter{
		"a1": a1,
		"a2": a2,
	})

	err := comp.Close()
	if !a1.closeCalled.Load() || !a2.closeCalled.Load() {
		t.Fatal("expected both adapters to be closed")
	}
	// Should return the first error.
	if err == nil {
		t.Fatal("expected close error")
	}
}

func TestCompositeAdapterPartialFailureDoesNotBlockOthers(t *testing.T) {
	t.Parallel()

	a1 := &stubAdapter{
		id: "failing",
		startFunc: func(_ context.Context, _ func(context.Context, Message) error) error {
			return errors.New("a1 failed")
		},
	}
	a2 := &stubAdapter{id: "healthy"}

	comp, _ := NewCompositeAdapter(map[string]RuntimeAdapter{
		"failing": a1,
		"healthy": a2,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := comp.Start(ctx, func(context.Context, Message) error { return nil })
	if err == nil || !errors.Is(err, context.Canceled) {
		// The composite should propagate the first adapter error.
		if err == nil {
			t.Fatal("expected error from failing adapter")
		}
	}
	if !a2.startCalled.Load() {
		t.Fatal("expected healthy adapter to have started")
	}
}
