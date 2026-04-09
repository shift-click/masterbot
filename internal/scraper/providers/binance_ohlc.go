package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// BinanceOHLC fetches historical candlestick data from the Binance REST API.
// Uses the public /api/v3/klines endpoint (no API key required).
type BinanceOHLC struct {
	client *BreakerHTTPClient
	logger *slog.Logger
}

// NewBinanceOHLC creates a new Binance OHLC provider.
func NewBinanceOHLC(logger *slog.Logger) *BinanceOHLC {
	if logger == nil {
		logger = slog.Default()
	}
	return &BinanceOHLC{
		client: DefaultBreakerClient(10*time.Second, "binance_ohlc", logger),
		logger: logger.With("component", "binance_ohlc"),
	}
}

// binanceInterval maps a Timeframe to the Binance kline interval and limit.
func binanceInterval(tf Timeframe) (interval string, limit int) {
	switch tf {
	case Timeframe1D:
		return "1h", 24
	case Timeframe1W:
		return "1d", 7
	case Timeframe1M:
		return "1d", 30
	case Timeframe3M:
		return "1d", 90
	case Timeframe6M:
		return "1w", 26
	case Timeframe1Y:
		return "1w", 52
	default:
		return "1d", 30
	}
}

// Fetch retrieves OHLC data for a coin symbol (e.g. "BTC") from Binance.
func (b *BinanceOHLC) Fetch(ctx context.Context, symbol string, tf Timeframe) (OHLCData, error) {
	interval, limit := binanceInterval(tf)
	url := fmt.Sprintf(
		"https://api.binance.com/api/v3/klines?symbol=%sUSDT&interval=%s&limit=%d",
		symbol, interval, limit,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return OHLCData{}, fmt.Errorf("binance ohlc request: %w", err)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return OHLCData{}, fmt.Errorf("binance ohlc fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return OHLCData{}, fmt.Errorf("binance ohlc: status %d: %s", resp.StatusCode, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return OHLCData{}, fmt.Errorf("binance ohlc read: %w", err)
	}

	// Response is an array of arrays: [[openTime, open, high, low, close, volume, ...], ...]
	var raw [][]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return OHLCData{}, fmt.Errorf("binance ohlc parse: %w", err)
	}

	points := make([]OHLCPoint, 0, len(raw))
	for _, kline := range raw {
		if len(kline) < 6 {
			continue
		}
		point, err := parseBinanceKline(kline)
		if err != nil {
			b.logger.Warn("skip binance kline", "error", err)
			continue
		}
		points = append(points, point)
	}

	if len(points) == 0 {
		return OHLCData{}, fmt.Errorf("binance ohlc: no data for %s", symbol)
	}

	return OHLCData{Symbol: symbol, Points: points}, nil
}

func parseBinanceKline(kline []json.RawMessage) (OHLCPoint, error) {
	var openTimeMs int64
	if err := json.Unmarshal(kline[0], &openTimeMs); err != nil {
		return OHLCPoint{}, fmt.Errorf("parse open time: %w", err)
	}

	parseFloat := func(raw json.RawMessage) (float64, error) {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, err
		}
		return strconv.ParseFloat(s, 64)
	}

	open, err := parseFloat(kline[1])
	if err != nil {
		return OHLCPoint{}, fmt.Errorf("parse open: %w", err)
	}
	high, err := parseFloat(kline[2])
	if err != nil {
		return OHLCPoint{}, fmt.Errorf("parse high: %w", err)
	}
	low, err := parseFloat(kline[3])
	if err != nil {
		return OHLCPoint{}, fmt.Errorf("parse low: %w", err)
	}
	closePrice, err := parseFloat(kline[4])
	if err != nil {
		return OHLCPoint{}, fmt.Errorf("parse close: %w", err)
	}
	vol, err := parseFloat(kline[5])
	if err != nil {
		return OHLCPoint{}, fmt.Errorf("parse volume: %w", err)
	}

	return OHLCPoint{
		Time:   time.UnixMilli(openTimeMs),
		Open:   open,
		High:   high,
		Low:    low,
		Close:  closePrice,
		Volume: vol,
	}, nil
}
