package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// CoinGecko fetches market cap data and maintains a coin ID list.
type CoinGecko struct {
	client *BreakerHTTPClient
	logger *slog.Logger

	mu         sync.RWMutex
	marketCaps map[string]float64 // symbol -> market cap USD
	idList     map[string]string  // uppercase symbol -> coingecko id
}

// NewCoinGecko creates a new CoinGecko provider.
func NewCoinGecko(logger *slog.Logger) *CoinGecko {
	if logger == nil {
		logger = slog.Default()
	}
	return &CoinGecko{
		client:     DefaultBreakerClient(15*time.Second, "coingecko", logger),
		logger:     logger.With("component", "coingecko"),
		marketCaps: make(map[string]float64),
		idList:     make(map[string]string),
	}
}

// SetTransport replaces the underlying HTTP transport (for testing).
func (c *CoinGecko) SetTransport(rt http.RoundTripper) { c.client.SetTransport(rt) }

// MarketCap returns the cached market cap for a symbol (uppercase).
func (c *CoinGecko) MarketCap(symbol string) (float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cap, ok := c.marketCaps[strings.ToUpper(symbol)]
	return cap, ok
}

// LookupID returns the CoinGecko ID for a symbol (case-insensitive).
func (c *CoinGecko) LookupID(symbol string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	id, ok := c.idList[strings.ToUpper(symbol)]
	return id, ok
}

// FetchMarketCaps fetches market caps for top coins.
func (c *CoinGecko) FetchMarketCaps(ctx context.Context) error {
	const url = "https://api.coingecko.com/api/v3/coins/markets?vs_currency=usd&order=market_cap_desc&per_page=250&page=1"

	body, err := c.doGet(ctx, url)
	if err != nil {
		return fmt.Errorf("coingecko markets: %w", err)
	}

	var items []struct {
		ID        string  `json:"id"`
		Symbol    string  `json:"symbol"`
		MarketCap float64 `json:"market_cap"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		return fmt.Errorf("coingecko markets parse: %w", err)
	}

	c.mu.Lock()
	for _, item := range items {
		sym := strings.ToUpper(item.Symbol)
		c.marketCaps[sym] = item.MarketCap
		if _, exists := c.idList[sym]; !exists {
			c.idList[sym] = item.ID
		}
	}
	c.mu.Unlock()

	c.logger.Debug("market caps updated", "count", len(items))
	return nil
}

// FetchIDList fetches the full coin ID list from CoinGecko.
func (c *CoinGecko) FetchIDList(ctx context.Context) error {
	const url = "https://api.coingecko.com/api/v3/coins/list"

	body, err := c.doGet(ctx, url)
	if err != nil {
		return fmt.Errorf("coingecko coin list: %w", err)
	}

	var items []struct {
		ID     string `json:"id"`
		Symbol string `json:"symbol"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		return fmt.Errorf("coingecko coin list parse: %w", err)
	}

	c.mu.Lock()
	for _, item := range items {
		sym := strings.ToUpper(item.Symbol)
		// Only store first occurrence (most popular coin for that symbol).
		if _, exists := c.idList[sym]; !exists {
			c.idList[sym] = item.ID
		}
	}
	c.mu.Unlock()

	c.logger.Debug("coin ID list updated", "count", len(items))
	return nil
}

// FetchPrice fetches the current price for a CoinGecko ID.
func (c *CoinGecko) FetchPrice(ctx context.Context, id string) (*CEXQuote, error) {
	url := fmt.Sprintf("https://api.coingecko.com/api/v3/coins/%s?localization=false&tickers=false&community_data=false&developer_data=false", id)

	body, err := c.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("coingecko coin detail: %w", err)
	}

	var resp struct {
		Symbol     string `json:"symbol"`
		Name       string `json:"name"`
		MarketData struct {
			CurrentPrice         map[string]float64 `json:"current_price"`
			PriceChangePercent24 float64            `json:"price_change_percentage_24h"`
			PriceChange24        float64            `json:"price_change_24h"`
			MarketCap            map[string]float64 `json:"market_cap"`
		} `json:"market_data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("coingecko coin detail parse: %w", err)
	}

	usdPrice := resp.MarketData.CurrentPrice["usd"]
	quote := &CEXQuote{
		Symbol:       strings.ToUpper(resp.Symbol),
		Name:         resp.Name,
		USDPrice:     usdPrice,
		USDChangePct: resp.MarketData.PriceChangePercent24,
		USDChange:    resp.MarketData.PriceChange24,
		USDPrevClose: usdPrice - resp.MarketData.PriceChange24,
		MarketCap:    resp.MarketData.MarketCap["usd"],
		UpdatedAt:    time.Now(),
	}
	return quote, nil
}

// StartPolling starts background polling for market caps and ID list.
func (c *CoinGecko) StartPolling(ctx context.Context, marketCapInterval, idListInterval time.Duration) {
	c.logger.Info("coingecko polling started",
		"market_cap_interval", marketCapInterval,
		"id_list_interval", idListInterval,
	)

	// Initial fetches.
	if err := c.FetchIDList(ctx); err != nil {
		c.logger.Warn("initial coin list fetch failed", "error", err)
	}
	if err := c.FetchMarketCaps(ctx); err != nil {
		c.logger.Warn("initial market caps fetch failed", "error", err)
	}

	mcTicker := time.NewTicker(marketCapInterval)
	idTicker := time.NewTicker(idListInterval)
	defer mcTicker.Stop()
	defer idTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("coingecko polling stopped")
			return
		case <-mcTicker.C:
			if err := c.FetchMarketCaps(ctx); err != nil {
				c.logger.Warn("market caps poll failed", "error", err)
			}
		case <-idTicker.C:
			if err := c.FetchIDList(ctx); err != nil {
				c.logger.Warn("coin list poll failed", "error", err)
			}
		}
	}
}

func (c *CoinGecko) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	return io.ReadAll(resp.Body)
}
