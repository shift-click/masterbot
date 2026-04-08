package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// GeckoTerminalOHLC fetches OHLCV data from the free GeckoTerminal API.
// No API key required. Rate limit: 30 req/min.
type GeckoTerminalOHLC struct {
	client *BreakerHTTPClient
	logger *slog.Logger
}

// NewGeckoTerminalOHLC creates a new GeckoTerminal OHLC provider.
func NewGeckoTerminalOHLC(logger *slog.Logger) *GeckoTerminalOHLC {
	if logger == nil {
		logger = slog.Default()
	}
	return &GeckoTerminalOHLC{
		client: DefaultBreakerClient(15*time.Second, "geckoterminal_ohlc", logger),
		logger: logger.With("component", "geckoterminal_ohlc"),
	}
}

// chainIDToNetwork maps DexScreener chain IDs to GeckoTerminal network IDs.
var chainIDToNetwork = map[string]string{
	"ethereum":  "eth",
	"solana":    "solana",
	"bsc":       "bsc",
	"base":      "base",
	"arbitrum":  "arbitrum",
	"polygon":   "polygon_pos",
	"avalanche": "avax",
	"optimism":  "optimism",
	"fantom":    "ftm",
	"cronos":    "cronos",
	"sui":       "sui-network",
	"ton":       "ton",
	"tron":      "tron",
}

// geckoterminalTimeframe resolves GeckoTerminal API parameters for a given timeframe.
func geckoterminalTimeframe(tf Timeframe) (timeframe string, aggregate string, limit int) {
	switch tf {
	case Timeframe1D:
		return "hour", "1", 24
	case Timeframe1W:
		return "hour", "4", 42 // 7 days / 4h = 42
	case Timeframe1M:
		return "day", "1", 30
	case Timeframe3M:
		return "day", "1", 90
	case Timeframe6M:
		return "day", "1", 180
	case Timeframe1Y:
		return "day", "1", 365
	default:
		return "day", "1", 30
	}
}

// Fetch retrieves OHLCV data for a DEX token from GeckoTerminal.
func (g *GeckoTerminalOHLC) Fetch(ctx context.Context, chainID, poolAddress string, tf Timeframe) (OHLCData, error) {
	network, ok := chainIDToNetwork[chainID]
	if !ok {
		network = chainID // try as-is
	}

	timeframe, aggregate, limit := geckoterminalTimeframe(tf)

	url := fmt.Sprintf(
		"https://api.geckoterminal.com/api/v2/networks/%s/pools/%s/ohlcv/%s?aggregate=%s&limit=%d&currency=usd",
		network, poolAddress, timeframe, aggregate, limit,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return OHLCData{}, fmt.Errorf("geckoterminal ohlc request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return OHLCData{}, fmt.Errorf("geckoterminal ohlc fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return OHLCData{}, fmt.Errorf("geckoterminal ohlc: status %d: %s", resp.StatusCode, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return OHLCData{}, fmt.Errorf("geckoterminal ohlc read: %w", err)
	}

	var apiResp struct {
		Data struct {
			Attributes struct {
				OHLCVList [][]json.Number `json:"ohlcv_list"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return OHLCData{}, fmt.Errorf("geckoterminal ohlc parse: %w", err)
	}

	ohlcvList := apiResp.Data.Attributes.OHLCVList
	if len(ohlcvList) == 0 {
		return OHLCData{}, fmt.Errorf("geckoterminal ohlc: no data for %s/%s", network, poolAddress)
	}

	points := make([]OHLCPoint, 0, len(ohlcvList))
	for _, row := range ohlcvList {
		if len(row) < 6 {
			continue
		}
		ts, _ := row[0].Int64()
		open, _ := row[1].Float64()
		high, _ := row[2].Float64()
		low, _ := row[3].Float64()
		closePrice, _ := row[4].Float64()
		vol, _ := row[5].Float64()

		points = append(points, OHLCPoint{
			Time:   time.Unix(ts, 0),
			Open:   open,
			High:   high,
			Low:    low,
			Close:  closePrice,
			Volume: vol,
		})
	}

	if len(points) == 0 {
		return OHLCData{}, fmt.Errorf("geckoterminal ohlc: no valid points for %s/%s", network, poolAddress)
	}

	// GeckoTerminal returns newest first — reverse to chronological order
	for i, j := 0, len(points)-1; i < j; i, j = i+1, j-1 {
		points[i], points[j] = points[j], points[i]
	}

	return OHLCData{Symbol: poolAddress, Points: points}, nil
}
