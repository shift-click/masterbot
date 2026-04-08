package alerting

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewInlineNotifier_NilOnMissingConfig(t *testing.T) {
	t.Parallel()

	n := NewInlineNotifier(InlineNotifierConfig{}, nil)
	if n != nil {
		t.Error("expected nil notifier when config is empty")
	}

	n = NewInlineNotifier(InlineNotifierConfig{TelegramBotToken: "token"}, nil)
	if n != nil {
		t.Error("expected nil notifier when chatID is missing")
	}
}

func TestNewInlineNotifier_CreatedWithValidConfig(t *testing.T) {
	t.Parallel()

	n := NewInlineNotifier(InlineNotifierConfig{
		TelegramBotToken: "token",
		TelegramChatID:   "123",
	}, nil)
	if n == nil {
		t.Fatal("expected non-nil notifier with valid config")
	}
	if n.appName != "jucobot" {
		t.Errorf("expected default app name 'jucobot', got %q", n.appName)
	}
}

func TestInlineNotifier_NilNotifierIsNoop(t *testing.T) {
	t.Parallel()

	var n *InlineNotifier
	// Should not panic.
	n.Notify("stock", "fetch_error", "HTTP 409")
}

func TestInlineNotifier_Dedup(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var calls int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	n := NewInlineNotifier(InlineNotifierConfig{
		TelegramBotToken: "token",
		TelegramChatID:   "123",
		TelegramAPIBase:  srv.URL,
		DedupWindow:      1 * time.Hour,
	}, nil)

	// First call should go through.
	n.Notify("stock", "fetch_error", "HTTP 409")
	// Second call with same error class should be deduplicated.
	n.Notify("stock", "fetch_error", "HTTP 409 again")
	// Different error class should go through.
	n.Notify("stock", "deadline_exceeded", "timeout")

	// Wait for goroutines to complete.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if calls != 2 {
		t.Errorf("expected 2 calls (dedup same class), got %d", calls)
	}
}

func TestInlineNotifier_DedupKeyIncludesCommand(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var calls int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	n := NewInlineNotifier(InlineNotifierConfig{
		TelegramBotToken: "token",
		TelegramChatID:   "123",
		TelegramAPIBase:  srv.URL,
		DedupWindow:      1 * time.Hour,
	}, nil)

	n.Notify("stock", "fetch_error", "HTTP 409")
	n.Notify("chart", "fetch_error", "HTTP 409")

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if calls != 2 {
		t.Errorf("expected 2 calls for different commands, got %d", calls)
	}
}

func TestFormatInlineAlert(t *testing.T) {
	t.Parallel()

	msg := FormatInlineAlert("jucobot", "stock", "fetch_error", "HTTP 409 from api.stock.naver.com")
	if !strings.Contains(msg, "[jucobot]") {
		t.Error("expected app name in message")
	}
	if !strings.Contains(msg, "command=stock") {
		t.Error("expected command in message")
	}
	if !strings.Contains(msg, "error_class=fetch_error") {
		t.Error("expected error class in message")
	}
	if !strings.Contains(msg, "HTTP 409") {
		t.Error("expected error message in message")
	}
}
