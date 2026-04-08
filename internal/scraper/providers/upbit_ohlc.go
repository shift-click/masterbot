package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"time"
)

// UpbitOHLC fetches historical candlestick data from the Upbit REST API.
// Uses public /v1/candles endpoints (no API key required).
type UpbitOHLC struct {
	client *BreakerHTTPClient
	logger *slog.Logger
}

// NewUpbitOHLC creates a new Upbit OHLC provider.
func NewUpbitOHLC(logger *slog.Logger) *UpbitOHLC {
	if logger == nil {
		logger = slog.Default()
	}
	return &UpbitOHLC{
		client: DefaultBreakerClient(10*time.Second, "upbit_ohlc", logger),
		logger: logger.With("component", "upbit_ohlc"),
	}
}

type upbitCandle struct {
	Market               string  `json:"market"`
	CandleDateTimeKST    string  `json:"candle_date_time_kst"`
	OpeningPrice         float64 `json:"opening_price"`
	HighPrice            float64 `json:"high_price"`
	LowPrice             float64 `json:"low_price"`
	TradePrice           float64 `json:"trade_price"`
	CandleAccTradeVolume float64 `json:"candle_acc_trade_volume"`
}

const upbitCandleDateTimeLayout = "2006-01-02T15:04:05"

// Fetch retrieves OHLC data for a coin symbol (e.g. "BTC") from Upbit in KRW.
func (u *UpbitOHLC) Fetch(ctx context.Context, symbol string, tf Timeframe) (OHLCData, error) {
	days := TimeframeDays(tf)
	market := "KRW-" + symbol

	var allCandles []upbitCandle
	for remaining, to := days, ""; remaining > 0; {
		candles, err := u.fetchPage(ctx, market, min(remaining, 200), to)
		if err != nil {
			return OHLCData{}, err
		}
		if len(candles) == 0 {
			break
		}

		allCandles = append(allCandles, candles...)
		remaining -= len(candles)

		if remaining > 0 {
			nextCursor, ok := nextUpbitCursor(candles[len(candles)-1])
			if !ok {
				break
			}
			to = nextCursor
		}
	}

	if len(allCandles) == 0 {
		return OHLCData{}, fmt.Errorf("upbit ohlc: no data for %s", symbol)
	}

	points := make([]OHLCPoint, 0, len(allCandles))
	for _, c := range allCandles {
		t, err := time.Parse(upbitCandleDateTimeLayout, c.CandleDateTimeKST)
		if err != nil {
			u.logger.Warn("skip upbit candle", "error", err)
			continue
		}
		points = append(points, OHLCPoint{
			Time:   t,
			Open:   c.OpeningPrice,
			High:   c.HighPrice,
			Low:    c.LowPrice,
			Close:  c.TradePrice,
			Volume: c.CandleAccTradeVolume,
		})
	}

	// Sort chronologically (Upbit returns newest first)
	sort.Slice(points, func(i, j int) bool {
		return points[i].Time.Before(points[j].Time)
	})

	return OHLCData{Symbol: symbol, Points: points}, nil
}

func nextUpbitCursor(oldest upbitCandle) (string, bool) {
	t, err := time.Parse(upbitCandleDateTimeLayout, oldest.CandleDateTimeKST)
	if err != nil {
		return "", false
	}
	return t.UTC().Format(upbitCandleDateTimeLayout), true
}

func (u *UpbitOHLC) fetchPage(ctx context.Context, market string, count int, to string) ([]upbitCandle, error) {
	url := fmt.Sprintf(
		"https://api.upbit.com/v1/candles/days?market=%s&count=%d",
		market, count,
	)
	if to != "" {
		url += "&to=" + to
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("upbit ohlc request: %w", err)
	}

	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upbit ohlc fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("upbit ohlc: status %d: %s", resp.StatusCode, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("upbit ohlc read: %w", err)
	}

	var candles []upbitCandle
	if err := json.Unmarshal(body, &candles); err != nil {
		return nil, fmt.Errorf("upbit ohlc parse: %w", err)
	}

	return candles, nil
}
