package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// UpbitWS connects to Upbit WebSocket for real-time KRW price streams.
type UpbitWS struct {
	symbols  []string // e.g. ["BTC", "ETH", "SOL"]
	callback func(UpbitTickerUpdate)
	logger   *slog.Logger

	mu   sync.Mutex
	conn *websocket.Conn
}

// NewUpbitWS creates a new Upbit WebSocket provider.
// symbols should be uppercase coin symbols (e.g. "BTC", "ETH").
func NewUpbitWS(symbols []string, callback func(UpbitTickerUpdate), logger *slog.Logger) *UpbitWS {
	if logger == nil {
		logger = slog.Default()
	}
	return &UpbitWS{
		symbols:  symbols,
		callback: callback,
		logger:   logger.With("component", "upbit_ws"),
	}
}

// Start connects and streams. Blocks until ctx is cancelled.
func (u *UpbitWS) Start(ctx context.Context) {
	u.logger.Info("upbit WS starting", "symbols", len(u.symbols))

	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			u.logger.Info("upbit WS stopped")
			return
		default:
		}

		err := u.connect(ctx)
		if err != nil {
			u.logger.Warn("upbit WS connection error", "error", err, "reconnect_in", backoff)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
	}
}

func (u *UpbitWS) connect(ctx context.Context) error {
	const wsURL = "wss://api.upbit.com/websocket/v1"

	u.logger.Debug("connecting to upbit WS")

	dialer := websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("upbit WS dial: %w", err)
	}

	u.mu.Lock()
	u.conn = conn
	u.mu.Unlock()

	defer func() {
		conn.Close()
		u.mu.Lock()
		u.conn = nil
		u.mu.Unlock()
	}()

	// Send subscription message.
	if err := u.subscribe(conn); err != nil {
		return fmt.Errorf("upbit WS subscribe: %w", err)
	}

	u.logger.Info("upbit WS connected", "symbols", len(u.symbols))

	// Read loop.
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, message, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("upbit WS read: %w", err)
		}

		u.handleMessage(message)
	}
}

func (u *UpbitWS) subscribe(conn *websocket.Conn) error {
	// Build market codes: ["KRW-BTC", "KRW-ETH", ...]
	codes := make([]string, len(u.symbols))
	for i, sym := range u.symbols {
		codes[i] = "KRW-" + sym
	}

	// Upbit subscription format: array of objects.
	sub := []interface{}{
		map[string]string{"ticket": "jucobot-coin"},
		map[string]interface{}{
			"type":  "ticker",
			"codes": codes,
		},
		map[string]string{"format": "DEFAULT"},
	}

	data, err := json.Marshal(sub)
	if err != nil {
		return err
	}

	return conn.WriteMessage(websocket.TextMessage, data)
}

func (u *UpbitWS) handleMessage(data []byte) {
	var ticker struct {
		Type              string  `json:"type"`
		Code              string  `json:"code"`                // e.g. "KRW-BTC"
		TradePrice        float64 `json:"trade_price"`         // current price
		PrevClosingPrice  float64 `json:"prev_closing_price"`  // 00:00 KST close
		SignedChangePrice float64 `json:"signed_change_price"` // change amount
		SignedChangeRate  float64 `json:"signed_change_rate"`  // change rate (0.0225)
	}

	if err := json.Unmarshal(data, &ticker); err != nil {
		return
	}

	if ticker.Type != "ticker" || ticker.Code == "" {
		return
	}

	// Strip "KRW-" prefix to get symbol.
	symbol := strings.TrimPrefix(ticker.Code, "KRW-")
	if symbol == ticker.Code {
		return // not a KRW pair
	}

	update := UpbitTickerUpdate{
		Symbol:    symbol,
		TradePrice: ticker.TradePrice,
		PrevClose: ticker.PrevClosingPrice,
		Change:    ticker.SignedChangePrice,
		ChangePct: ticker.SignedChangeRate,
	}

	if u.callback != nil {
		u.callback(update)
	}
}

// Close gracefully closes the WebSocket connection.
func (u *UpbitWS) Close() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.conn != nil {
		u.conn.Close()
		u.conn = nil
	}
}
