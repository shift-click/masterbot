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

// BinanceWS connects to Binance WebSocket for real-time USDT price streams.
type BinanceWS struct {
	symbols  []string // e.g. ["BTC", "ETH", "SOL"]
	callback func(BinanceTickerUpdate)
	logger   *slog.Logger

	mu   sync.Mutex
	conn *websocket.Conn
}

// NewBinanceWS creates a new Binance WebSocket provider.
// symbols should be uppercase coin symbols (e.g. "BTC", "ETH").
// callback is called for every price update.
func NewBinanceWS(symbols []string, callback func(BinanceTickerUpdate), logger *slog.Logger) *BinanceWS {
	if logger == nil {
		logger = slog.Default()
	}
	return &BinanceWS{
		symbols:  symbols,
		callback: callback,
		logger:   logger.With("component", "binance_ws"),
	}
}

// Start connects and streams. Blocks until ctx is cancelled.
// Automatically reconnects on disconnection with exponential backoff.
func (b *BinanceWS) Start(ctx context.Context) {
	b.logger.Info("binance WS starting", "symbols", len(b.symbols))

	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			b.logger.Info("binance WS stopped")
			return
		default:
		}

		err := b.connect(ctx)
		if err != nil {
			b.logger.Warn("binance WS connection error", "error", err, "reconnect_in", backoff)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
	}
}

func (b *BinanceWS) connect(ctx context.Context) error {
	url := b.buildURL()
	b.logger.Debug("connecting to binance WS", "url", url)

	dialer := websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("binance WS dial: %w", err)
	}

	b.mu.Lock()
	b.conn = conn
	b.mu.Unlock()

	defer func() {
		conn.Close()
		b.mu.Lock()
		b.conn = nil
		b.mu.Unlock()
	}()

	b.logger.Info("binance WS connected", "symbols", len(b.symbols))

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
			return fmt.Errorf("binance WS read: %w", err)
		}

		b.logger.Debug("binance WS message received", "len", len(message))
		b.handleMessage(message)
	}
}

func (b *BinanceWS) handleMessage(data []byte) {
	// Binance combined stream format: {"stream":"btcusdt@ticker","data":{...}}
	var wrapper struct {
		Stream string          `json:"stream"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		// Try direct ticker format.
		b.parseTicker(data)
		return
	}
	b.parseTicker(wrapper.Data)
}

func (b *BinanceWS) parseTicker(data []byte) {
	// Use map to avoid Go's case-insensitive JSON field matching.
	// Binance has both "c"/"C", "p"/"P", "e"/"E" fields that collide.
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return
	}

	symbol, _ := m["s"].(string) // e.g. "BTCUSDT"
	if symbol == "" {
		return
	}

	// Strip "USDT" suffix to get symbol.
	sym := strings.TrimSuffix(symbol, "USDT")
	if sym == symbol {
		return // not a USDT pair
	}

	update := BinanceTickerUpdate{
		Symbol:    sym,
		Price:     jsonFloat(m["c"]), // current price
		PrevClose: jsonFloat(m["x"]), // prev close
		Change:    jsonFloat(m["p"]), // price change
		ChangePct: jsonFloat(m["P"]), // price change percent
	}

	if b.callback != nil {
		b.callback(update)
	}
}

// jsonFloat extracts a float64 from a JSON value that may be string or number.
func jsonFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		return parseFloat(val)
	default:
		return 0
	}
}

// buildURL builds the combined stream URL for all symbols.
func (b *BinanceWS) buildURL() string {
	streams := make([]string, len(b.symbols))
	for i, sym := range b.symbols {
		streams[i] = strings.ToLower(sym) + "usdt@ticker"
	}
	return "wss://stream.binance.com:9443/stream?streams=" + strings.Join(streams, "/")
}

// Close gracefully closes the WebSocket connection.
func (b *BinanceWS) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn != nil {
		b.conn.Close()
		b.conn = nil
	}
}

func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}
