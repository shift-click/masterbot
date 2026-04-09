package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// InlineNotifier sends immediate Telegram alerts from the bot process.
// It deduplicates by error class to prevent alert floods.
type InlineNotifier struct {
	httpClient *http.Client
	token      string
	chatID     string
	baseURL    string
	appName    string
	deduper    *Deduper
	logger     *slog.Logger
}

// InlineNotifierConfig holds configuration for InlineNotifier.
type InlineNotifierConfig struct {
	TelegramBotToken string
	TelegramChatID   string
	TelegramAPIBase  string // defaults to "https://api.telegram.org"
	AppName          string // defaults to "jucobot"
	DedupWindow      time.Duration // defaults to 5 minutes
}

// NewInlineNotifier creates a new InlineNotifier.
// Returns a noop notifier if token or chatID is empty.
func NewInlineNotifier(cfg InlineNotifierConfig, logger *slog.Logger) *InlineNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("component", "inline_notifier")

	if cfg.TelegramBotToken == "" || cfg.TelegramChatID == "" {
		logger.Warn("inline alerting disabled: missing TELEGRAM_BOT_TOKEN or TELEGRAM_CHAT_ID")
		return nil
	}

	if cfg.TelegramAPIBase == "" {
		cfg.TelegramAPIBase = "https://api.telegram.org"
	}
	if cfg.AppName == "" {
		cfg.AppName = "jucobot"
	}
	if cfg.DedupWindow <= 0 {
		cfg.DedupWindow = 5 * time.Minute
	}

	logger.Info("inline alerting enabled",
		"chat_id", cfg.TelegramChatID,
		"dedup_window", cfg.DedupWindow,
	)

	return &InlineNotifier{
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
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
		baseURL:    cfg.TelegramAPIBase,
		appName:    cfg.AppName,
		deduper:    NewDeduper(cfg.DedupWindow),
		logger:     logger,
	}
}

// Notify sends an alert if not deduplicated. Fire-and-forget via goroutine.
func (n *InlineNotifier) Notify(command, errorClass, errorMsg string) {
	if n == nil {
		return
	}

	dedupKey := fmt.Sprintf("%s:%s", command, errorClass)
	if !n.deduper.Allow(dedupKey, time.Now()) {
		return
	}

	text := FormatInlineAlert(n.appName, command, errorClass, errorMsg)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if err := n.sendMessage(ctx, text); err != nil {
			n.logger.Error("inline alert send failed", "error", err)
		}
	}()
}

func (n *InlineNotifier) sendMessage(ctx context.Context, text string) error {
	payload := map[string]any{
		"chat_id": n.chatID,
		"text":    text,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/bot%s/sendMessage", n.baseURL, n.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
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
		return fmt.Errorf("telegram api: status=%d desc=%s", resp.StatusCode, result.Description)
	}
	return nil
}

// FormatInlineAlert formats an alert message.
func FormatInlineAlert(appName, command, errorClass, errorMsg string) string {
	return fmt.Sprintf("[%s] command=%s error_class=%s\n%s\nat=%s",
		appName, command, errorClass, errorMsg,
		time.Now().Format(time.RFC3339),
	)
}
