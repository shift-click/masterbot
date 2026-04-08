package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DexScreener fetches DEX token data from DexScreener API.
type DexScreener struct {
	client *BreakerHTTPClient
	logger *slog.Logger
}

// NewDexScreener creates a new DexScreener provider.
func NewDexScreener(logger *slog.Logger) *DexScreener {
	if logger == nil {
		logger = slog.Default()
	}
	return &DexScreener{
		client: DefaultBreakerClient(10 * time.Second, "dexscreener", logger),
		logger: logger.With("component", "dexscreener"),
	}
}

// dexPair is the internal representation of a DexScreener pair.
type dexPair struct {
	ChainID   string `json:"chainId"`
	DEXName   string `json:"dexId"`
	PairAddr  string `json:"pairAddress"`
	BaseToken struct {
		Address string `json:"address"`
		Name    string `json:"name"`
		Symbol  string `json:"symbol"`
	} `json:"baseToken"`
	PriceUSD  string `json:"priceUsd"`
	Volume    struct {
		H24 float64 `json:"h24"`
	} `json:"volume"`
	Liquidity struct {
		USD float64 `json:"usd"`
	} `json:"liquidity"`
	FDV       float64 `json:"fdv"`
	MarketCap float64 `json:"marketCap"`
	PriceChange struct {
		H24 float64 `json:"h24"`
	} `json:"priceChange"`
}

// Search searches for tokens by name or symbol.
// Returns the most liquid result.
func (d *DexScreener) Search(ctx context.Context, query string) (*DEXQuote, error) {
	u := fmt.Sprintf("https://api.dexscreener.com/latest/dex/search?q=%s", url.QueryEscape(query))

	body, err := d.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("dexscreener search: %w", err)
	}

	var resp struct {
		Pairs []dexPair `json:"pairs"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("dexscreener search parse: %w", err)
	}

	if len(resp.Pairs) == 0 {
		return nil, fmt.Errorf("no DEX pairs found for %q", query)
	}

	// DexScreener returns results sorted by liquidity/volume.
	// Take the first (most liquid) result.
	return d.pairToQuote(resp.Pairs[0]), nil
}

// FetchByAddress fetches token data by contract address.
func (d *DexScreener) FetchByAddress(ctx context.Context, address string) (*DEXQuote, error) {
	u := fmt.Sprintf("https://api.dexscreener.com/latest/dex/tokens/%s", address)

	body, err := d.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("dexscreener token: %w", err)
	}

	var resp struct {
		Pairs []dexPair `json:"pairs"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("dexscreener token parse: %w", err)
	}

	if len(resp.Pairs) == 0 {
		return nil, fmt.Errorf("no DEX pairs found for address %q", address)
	}

	return d.pairToQuote(resp.Pairs[0]), nil
}

// FetchBatch fetches data for multiple token addresses in one call (max 30).
// Returns a map of contract_address → DEXQuote.
func (d *DexScreener) FetchBatch(ctx context.Context, addresses []string) (map[string]*DEXQuote, error) {
	if len(addresses) == 0 {
		return nil, nil
	}
	if len(addresses) > 30 {
		addresses = addresses[:30]
	}

	u := fmt.Sprintf("https://api.dexscreener.com/latest/dex/tokens/%s", strings.Join(addresses, ","))

	body, err := d.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("dexscreener batch: %w", err)
	}

	var resp struct {
		Pairs []dexPair `json:"pairs"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("dexscreener batch parse: %w", err)
	}

	// Group pairs by base token address, take the most liquid pair per token.
	result := make(map[string]*DEXQuote)
	for _, pair := range resp.Pairs {
		addr := strings.ToLower(pair.BaseToken.Address)
		if _, exists := result[addr]; !exists {
			result[addr] = d.pairToQuote(pair)
		}
	}

	return result, nil
}

func (d *DexScreener) pairToQuote(p dexPair) *DEXQuote {
	return &DEXQuote{
		Symbol:          strings.ToUpper(p.BaseToken.Symbol),
		Name:            p.BaseToken.Name,
		ChainID:         p.ChainID,
		DEXName:         p.DEXName,
		ContractAddress: p.BaseToken.Address,
		PairAddress:     p.PairAddr,
		USDPrice:        parseFloat(p.PriceUSD),
		USDChangePct24h: p.PriceChange.H24,
		Volume24h:       p.Volume.H24,
		Liquidity:       p.Liquidity.USD,
		MarketCap:       p.MarketCap,
		FDV:             p.FDV,
		UpdatedAt:       time.Now(),
	}
}

func (d *DexScreener) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	return io.ReadAll(resp.Body)
}
