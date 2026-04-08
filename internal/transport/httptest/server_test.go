package httptest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/transport"
)

func newTestCfg() config.HTTPTestConfig {
	return config.HTTPTestConfig{
		Enabled:      true,
		Addr:         "127.0.0.1:0",
		ReplyTimeout: 5 * time.Second,
	}
}

// startServer starts the server in a goroutine and returns the base URL and a cleanup func.
func startServer(t *testing.T, srv *Server, onMessage func(context.Context, transport.Message) error) (baseURL string, cleanup func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx, onMessage)
	}()

	// Wait for listener to be assigned.
	deadline := time.After(3 * time.Second)
	for {
		if addr := srv.Addr(); addr != "" {
			break
		}
		select {
		case <-deadline:
			cancel()
			t.Fatal("server did not start in time")
		case err := <-errCh:
			cancel()
			t.Fatalf("server exited early: %v", err)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	return fmt.Sprintf("http://%s", srv.Addr()), func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	}
}

func postMessage(t *testing.T, baseURL string, req MessageRequest) MessageResponse {
	t.Helper()
	body, _ := json.Marshal(req)
	resp, err := http.Post(baseURL+"/message", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /message: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	var result MessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return result
}

func TestServer_MessageAndReply(t *testing.T) {
	srv := NewServer(newTestCfg(), nil)

	base, cleanup := startServer(t, srv, func(ctx context.Context, msg transport.Message) error {
		return srv.Reply(ctx, transport.ReplyRequest{
			Type: transport.ReplyTypeText,
			Room: msg.Raw.ChatID,
			Data: "Hello, " + msg.Msg,
		})
	})
	defer cleanup()

	result := postMessage(t, base, MessageRequest{Msg: "world"})

	if len(result.Replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(result.Replies))
	}
	if result.Replies[0].Type != transport.ReplyTypeText {
		t.Errorf("expected reply type text, got %s", result.Replies[0].Type)
	}
	data, ok := result.Replies[0].Data.(string)
	if !ok || data != "Hello, world" {
		t.Errorf("unexpected reply data: %v", result.Replies[0].Data)
	}
}

func TestServer_MultipleReplies(t *testing.T) {
	srv := NewServer(newTestCfg(), nil)

	base, cleanup := startServer(t, srv, func(ctx context.Context, msg transport.Message) error {
		_ = srv.Reply(ctx, transport.ReplyRequest{
			Type: transport.ReplyTypeImage,
			Room: msg.Raw.ChatID,
			Data: "base64image",
		})
		return srv.Reply(ctx, transport.ReplyRequest{
			Type: transport.ReplyTypeText,
			Room: msg.Raw.ChatID,
			Data: "caption",
		})
	})
	defer cleanup()

	result := postMessage(t, base, MessageRequest{Msg: "test"})

	if len(result.Replies) != 2 {
		t.Fatalf("expected 2 replies, got %d", len(result.Replies))
	}
	if result.Replies[0].Type != transport.ReplyTypeImage {
		t.Errorf("first reply type: got %s, want image", result.Replies[0].Type)
	}
	if result.Replies[1].Type != transport.ReplyTypeText {
		t.Errorf("second reply type: got %s, want text", result.Replies[1].Type)
	}
}

func TestServer_NoMatchReturnsEmptyReplies(t *testing.T) {
	srv := NewServer(newTestCfg(), nil)

	base, cleanup := startServer(t, srv, func(_ context.Context, _ transport.Message) error {
		return nil
	})
	defer cleanup()

	result := postMessage(t, base, MessageRequest{Msg: "unknown"})

	if len(result.Replies) != 0 {
		t.Fatalf("expected 0 replies, got %d", len(result.Replies))
	}
}

func TestServer_ReplyTimeout(t *testing.T) {
	cfg := newTestCfg()
	cfg.ReplyTimeout = 200 * time.Millisecond
	srv := NewServer(cfg, nil)

	base, cleanup := startServer(t, srv, func(_ context.Context, _ transport.Message) error {
		time.Sleep(2 * time.Second)
		return nil
	})
	defer cleanup()

	start := time.Now()
	result := postMessage(t, base, MessageRequest{Msg: "slow"})
	elapsed := time.Since(start)

	if elapsed > 1*time.Second {
		t.Errorf("response took too long: %v (expected ~200ms timeout)", elapsed)
	}
	// Should return empty replies on timeout.
	if len(result.Replies) != 0 {
		t.Errorf("expected 0 replies on timeout, got %d", len(result.Replies))
	}
}

func TestServer_Close(t *testing.T) {
	srv := NewServer(newTestCfg(), nil)
	_, cleanup := startServer(t, srv, func(context.Context, transport.Message) error { return nil })
	defer cleanup()

	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestServer_PassesAttachmentThroughRawChatLog(t *testing.T) {
	srv := NewServer(newTestCfg(), nil)

	const attachment = `{"mt":"image/png","url":"https://talk.kakaocdn.net/dn/example-full.png"}`

	base, cleanup := startServer(t, srv, func(ctx context.Context, msg transport.Message) error {
		if msg.Raw.Attachment != attachment {
			t.Fatalf("attachment = %q, want %q", msg.Raw.Attachment, attachment)
		}
		return srv.Reply(ctx, transport.ReplyRequest{
			Type: transport.ReplyTypeText,
			Room: msg.Raw.ChatID,
			Data: "ok",
		})
	})
	defer cleanup()

	result := postMessage(t, base, MessageRequest{
		Msg:        "photo",
		Attachment: attachment,
	})
	if len(result.Replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(result.Replies))
	}
}
