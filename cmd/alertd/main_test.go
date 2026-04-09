package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/alerting"
	"github.com/shift-click/masterbot/internal/metrics"
)

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestFormatAlertMessageAndGetenvHelpers(t *testing.T) {
	now := time.Date(2026, 3, 19, 4, 5, 6, 0, time.UTC)
	msg := formatAlertMessage(
		"jucobot",
		alerting.Alert{
			Severity: "critical",
			Title:    "title",
			Detail:   "detail",
		},
		metrics.Reliability{
			ErrorRate:        0.12,
			FailedCommands:   6,
			TotalCommands:    50,
			ReplyFailedCount: 2,
		},
		metrics.Reliability{P95LatencyMS: 1700, TotalCommands: 11, LatencySamples: 31},
		now,
	)
	if !strings.Contains(msg, "[jucobot][CRITICAL]") {
		t.Fatalf("formatted alert missing header: %q", msg)
	}
	if !strings.Contains(msg, "error_rate_3m=12.00% (6/50)") {
		t.Fatalf("formatted alert missing rate details: %q", msg)
	}
	if !strings.Contains(msg, "p95_10m=1700ms") {
		t.Fatalf("formatted alert missing p95: %q", msg)
	}
	if !strings.Contains(msg, "commands_10m=11") || !strings.Contains(msg, "latency_samples_10m=31") {
		t.Fatalf("formatted alert missing sample counts: %q", msg)
	}

	t.Setenv("ALERTD_X_STR", "")
	if got := getenvDefault("ALERTD_X_STR", "fallback"); got != "fallback" {
		t.Fatalf("getenvDefault empty = %q", got)
	}
	t.Setenv("ALERTD_X_STR", "ok")
	if got := getenvDefault("ALERTD_X_STR", "fallback"); got != "ok" {
		t.Fatalf("getenvDefault value = %q", got)
	}

	t.Setenv("ALERTD_X_DUR", "invalid")
	if got := getenvDuration("ALERTD_X_DUR", time.Second); got != time.Second {
		t.Fatalf("getenvDuration invalid = %v", got)
	}
	t.Setenv("ALERTD_X_DUR", "3s")
	if got := getenvDuration("ALERTD_X_DUR", time.Second); got != 3*time.Second {
		t.Fatalf("getenvDuration parsed = %v", got)
	}

	t.Setenv("ALERTD_X_FLOAT", "bad")
	if got := getenvFloat("ALERTD_X_FLOAT", 1.5); got != 1.5 {
		t.Fatalf("getenvFloat invalid = %.2f", got)
	}
	t.Setenv("ALERTD_X_FLOAT", "2.25")
	if got := getenvFloat("ALERTD_X_FLOAT", 1.5); got != 2.25 {
		t.Fatalf("getenvFloat parsed = %.2f", got)
	}

	t.Setenv("ALERTD_X_INT", "bad")
	if got := getenvInt64("ALERTD_X_INT", 7); got != 7 {
		t.Fatalf("getenvInt64 invalid = %d", got)
	}
	t.Setenv("ALERTD_X_INT", "42")
	if got := getenvInt64("ALERTD_X_INT", 7); got != 42 {
		t.Fatalf("getenvInt64 parsed = %d", got)
	}
}

func TestLoadConfig(t *testing.T) {
	t.Setenv("ALERTD_TELEGRAM_BOT_TOKEN", "")
	t.Setenv("ALERTD_TELEGRAM_CHAT_ID", "")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected loadConfig error when required env is missing")
	}

	t.Setenv("ALERTD_TELEGRAM_BOT_TOKEN", "token")
	t.Setenv("ALERTD_TELEGRAM_CHAT_ID", "chat")
	t.Setenv("ALERTD_METRICS_DB_PATH", "metrics.db")
	t.Setenv("ALERTD_POLL_INTERVAL", "15s")
	t.Setenv("ALERTD_CRITICAL_WINDOW", "4m")
	t.Setenv("ALERTD_WARNING_WINDOW", "12m")
	t.Setenv("ALERTD_ERROR_RATE_THRESHOLD", "0.07")
	t.Setenv("ALERTD_P95_THRESHOLD_MS", "1700")
	t.Setenv("ALERTD_MIN_COMMANDS", "20")
	t.Setenv("ALERTD_DEDUP_WINDOW", "30m")
	t.Setenv("ALERTD_APP_NAME", "myapp")
	t.Setenv("ALERTD_TELEGRAM_API_BASE", "https://example.test/")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.TelegramBotToken != "token" || cfg.TelegramChatID != "chat" {
		t.Fatalf("unexpected telegram config: %+v", cfg)
	}
	if cfg.PollInterval != 15*time.Second || cfg.CriticalWindow != 4*time.Minute || cfg.WarningWindow != 12*time.Minute {
		t.Fatalf("unexpected duration config: %+v", cfg)
	}
	if cfg.ErrorRateThreshold != 0.07 || cfg.P95ThresholdMS != 1700 || cfg.MinCommands != 20 {
		t.Fatalf("unexpected numeric config: %+v", cfg)
	}
	if cfg.TelegramAPIBase != "https://example.test/" {
		t.Fatalf("unexpected telegram base: %q", cfg.TelegramAPIBase)
	}
}

func TestTelegramClientSendMessage(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || !strings.Contains(r.URL.Path, "/bottoken/sendMessage") {
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
			body, _ := io.ReadAll(r.Body)
			text := string(body)
			if !strings.Contains(text, "\"chat_id\":\"chat\"") || !strings.Contains(text, "\"text\":\"hello\"") {
				t.Fatalf("unexpected body: %s", text)
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer srv.Close()

		c := &telegramClient{
			httpClient: srv.Client(),
			token:      "token",
			chatID:     "chat",
			baseURL:    srv.URL,
		}
		if err := c.SendMessage(context.Background(), "hello"); err != nil {
			t.Fatalf("SendMessage() error = %v", err)
		}
	})

	t.Run("api_not_ok", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"description":"bad"}`))
		}))
		defer srv.Close()
		c := &telegramClient{httpClient: srv.Client(), token: "token", chatID: "chat", baseURL: srv.URL}
		if err := c.SendMessage(context.Background(), "hello"); err == nil || !strings.Contains(err.Error(), "telegram api error") {
			t.Fatalf("expected telegram api error, got %v", err)
		}
	})

	t.Run("decode_error", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{`))
		}))
		defer srv.Close()
		c := &telegramClient{httpClient: srv.Client(), token: "token", chatID: "chat", baseURL: srv.URL}
		if err := c.SendMessage(context.Background(), "hello"); err == nil || !strings.Contains(err.Error(), "decode response") {
			t.Fatalf("expected decode response error, got %v", err)
		}
	})

	t.Run("request_error", func(t *testing.T) {
		t.Parallel()
		c := &telegramClient{
			httpClient: &http.Client{
				Transport: rtFunc(func(*http.Request) (*http.Response, error) {
					return nil, errors.New("network")
				}),
			},
			token:   "token",
			chatID:  "chat",
			baseURL: "http://example.invalid",
		}
		if err := c.SendMessage(context.Background(), "hello"); err == nil {
			t.Fatal("expected request error")
		}
	})
}

func TestRunOncePaths(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "metrics.db")
	store, err := metrics.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	var sendCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sendCount.Add(1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	client := &telegramClient{
		httpClient: srv.Client(),
		token:      "token",
		chatID:     "chat",
		baseURL:    srv.URL,
	}
	cfg := config{
		CriticalWindow:     10 * time.Minute,
		WarningWindow:      10 * time.Minute,
		ErrorRateThreshold: 0.01,
		P95ThresholdMS:     10,
		MinCommands:        1,
		AppName:            "jucobot",
	}

	// No events -> no alert send.
	if err := runOnce(context.Background(), store, client, alerting.NewDeduper(time.Minute), slog.Default(), cfg); err != nil {
		t.Fatalf("runOnce empty error = %v", err)
	}
	if sendCount.Load() != 0 {
		t.Fatalf("sendCount = %d, want 0 for empty snapshot", sendCount.Load())
	}

	now := time.Now().UTC()
	success := false
	events := []metrics.StoredEvent{
		{
			OccurredAt: now.Add(-time.Minute),
			RequestID:  "r1",
			EventName:  string(metrics.EventCommandFailed),
			CommandID:  "coin",
			Success:    &success,
			LatencyMS:  1500,
			ErrorClass: "upstream",
		},
		{
			OccurredAt: now.Add(-time.Minute),
			RequestID:  "r2",
			EventName:  string(metrics.EventReplyFailed),
			CommandID:  "coin",
			LatencyMS:  1800,
			ErrorClass: "reply",
		},
	}
	if err := store.InsertEvents(context.Background(), events); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	if err := runOnce(context.Background(), store, client, alerting.NewDeduper(time.Minute), slog.Default(), cfg); err != nil {
		t.Fatalf("runOnce alert error = %v", err)
	}
	if sendCount.Load() == 0 {
		t.Fatal("expected at least one alert to be sent")
	}

	// Closed store should surface query errors.
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	if err := runOnce(context.Background(), store, client, alerting.NewDeduper(time.Minute), slog.Default(), cfg); err == nil {
		t.Fatal("expected runOnce error on closed store")
	}
}
