package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/shift-click/masterbot/internal/alerting"
	"github.com/shift-click/masterbot/internal/metrics"
)

type config struct {
	MetricsDBPath      string
	PollInterval       time.Duration
	CriticalWindow     time.Duration
	WarningWindow      time.Duration
	ErrorRateThreshold float64
	P95ThresholdMS     int64
	MinCommands        int64
	DedupWindow        time.Duration
	TelegramBotToken   string
	TelegramChatID     string
	AppName            string
	TelegramAPIBase    string
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		slog.Error("failed to load alertd config", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})).
		With("component", "alertd", "app", cfg.AppName)
	logger.Info("alertd starting", "metrics_db_path", cfg.MetricsDBPath, "poll_interval", cfg.PollInterval.String())

	store, err := metrics.NewSQLiteStore(cfg.MetricsDBPath)
	if err != nil {
		logger.Error("failed to open metrics store", "error", err)
		os.Exit(1)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			logger.Warn("failed to close metrics store", "error", closeErr)
		}
	}()

	client := &telegramClient{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   10,
				MaxConnsPerHost:       20,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   5 * time.Second,
				ResponseHeaderTimeout: 10 * time.Second,
				ForceAttemptHTTP2:     true,
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
			},
		},
		token:      cfg.TelegramBotToken,
		chatID:     cfg.TelegramChatID,
		baseURL:    strings.TrimRight(cfg.TelegramAPIBase, "/"),
	}
	deduper := alerting.NewDeduper(cfg.DedupWindow)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	for {
		if err := runOnce(ctx, store, client, deduper, logger, cfg); err != nil {
			logger.Error("alert check failed", "error", err)
		}

		select {
		case <-ctx.Done():
			logger.Info("alertd stopped")
			return
		case <-ticker.C:
		}
	}
}

func runOnce(
	ctx context.Context,
	store *metrics.SQLiteStore,
	client *telegramClient,
	deduper *alerting.Deduper,
	logger *slog.Logger,
	cfg config,
) error {
	now := time.Now()
	critical, err := store.QueryReliability(ctx, now.Add(-cfg.CriticalWindow), now, "", "")
	if err != nil {
		return fmt.Errorf("query critical reliability: %w", err)
	}
	warning, err := store.QueryReliability(ctx, now.Add(-cfg.WarningWindow), now, "", "")
	if err != nil {
		return fmt.Errorf("query warning reliability: %w", err)
	}

	alerts := alerting.Evaluate(alerting.Snapshot{
		Now:      now,
		Critical: critical,
		Warning:  warning,
	}, alerting.Thresholds{
		ErrorRateThreshold: cfg.ErrorRateThreshold,
		P95ThresholdMS:     cfg.P95ThresholdMS,
		MinCommands:        cfg.MinCommands,
	})
	if len(alerts) == 0 {
		return nil
	}

	for _, alert := range alerts {
		if !deduper.Allow(alert.Key, now) {
			continue
		}
		message := formatAlertMessage(cfg.AppName, alert, critical, warning, now)
		if err := client.SendMessage(ctx, message); err != nil {
			logger.Error("failed to send telegram alert", "key", alert.Key, "error", err)
			continue
		}
		logger.Info("telegram alert sent", "key", alert.Key, "severity", alert.Severity)
	}
	return nil
}

func formatAlertMessage(appName string, alert alerting.Alert, critical, warning metrics.Reliability, now time.Time) string {
	return fmt.Sprintf(
		"[%s][%s] %s\n%s\nerror_rate_3m=%.2f%% (%d/%d)\nreply_failed_3m=%d\np95_10m=%dms\ncommands_10m=%d\nlatency_samples_10m=%d\nat=%s",
		appName,
		strings.ToUpper(alert.Severity),
		alert.Title,
		alert.Detail,
		critical.ErrorRate*100,
		critical.FailedCommands,
		critical.TotalCommands,
		critical.ReplyFailedCount,
		warning.P95LatencyMS,
		warning.TotalCommands,
		warning.LatencySamples,
		now.UTC().Format(time.RFC3339),
	)
}

func loadConfig() (config, error) {
	cfg := config{
		MetricsDBPath:      getenvDefault("ALERTD_METRICS_DB_PATH", "/app/data/admin-metrics.db"),
		PollInterval:       getenvDuration("ALERTD_POLL_INTERVAL", time.Minute),
		CriticalWindow:     getenvDuration("ALERTD_CRITICAL_WINDOW", 3*time.Minute),
		WarningWindow:      getenvDuration("ALERTD_WARNING_WINDOW", 10*time.Minute),
		ErrorRateThreshold: getenvFloat("ALERTD_ERROR_RATE_THRESHOLD", 0.05),
		P95ThresholdMS:     getenvInt64("ALERTD_P95_THRESHOLD_MS", 1500),
		MinCommands:        getenvInt64("ALERTD_MIN_COMMANDS", 10),
		DedupWindow:        getenvDuration("ALERTD_DEDUP_WINDOW", 15*time.Minute),
		TelegramBotToken:   strings.TrimSpace(os.Getenv("ALERTD_TELEGRAM_BOT_TOKEN")),
		TelegramChatID:     strings.TrimSpace(os.Getenv("ALERTD_TELEGRAM_CHAT_ID")),
		AppName:            getenvDefault("ALERTD_APP_NAME", "jucobot"),
		TelegramAPIBase:    getenvDefault("ALERTD_TELEGRAM_API_BASE", "https://api.telegram.org"),
	}

	var problems []string
	if cfg.TelegramBotToken == "" {
		problems = append(problems, "ALERTD_TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.TelegramChatID == "" {
		problems = append(problems, "ALERTD_TELEGRAM_CHAT_ID is required")
	}
	if cfg.MetricsDBPath == "" {
		problems = append(problems, "ALERTD_METRICS_DB_PATH is required")
	}
	if len(problems) > 0 {
		return config{}, errors.New(strings.Join(problems, "; "))
	}
	return cfg, nil
}

func getenvDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func getenvFloat(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func getenvInt64(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

type telegramClient struct {
	httpClient *http.Client
	token      string
	chatID     string
	baseURL    string
}

func (c *telegramClient) SendMessage(ctx context.Context, text string) error {
	payload := map[string]any{
		"chat_id": c.chatID,
		"text":    text,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/bot%s/sendMessage", c.baseURL, c.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("telegram api error: status=%d description=%s", resp.StatusCode, result.Description)
	}
	return nil
}
