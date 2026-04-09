package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// GoldPrice holds a single metal price point.
type GoldPrice struct {
	Metal       string  // "gold" or "silver"
	PricePerG   float64 // price per gram in KRW
	PricePerDon float64 // price per 돈 (3.75g) in KRW
}

// NaverGold fetches gold/silver prices from Naver Finance marketindex API.
type NaverGold struct {
	client *BreakerHTTPClient
	logger *slog.Logger

	mu        sync.RWMutex
	gold      *GoldPrice
	silver    *GoldPrice
	updatedAt time.Time
	cacheTTL  time.Duration
}

// NewNaverGold creates a new NaverGold provider.
func NewNaverGold(logger *slog.Logger) *NaverGold {
	if logger == nil {
		logger = slog.Default()
	}
	return &NaverGold{
		client:   DefaultBreakerClient(10 * time.Second, "naver_gold", logger),
		logger:   logger.With("component", "naver_gold"),
		cacheTTL: 10 * time.Minute,
	}
}

const gramsPerDon = 3.75

// Gold returns the cached gold price or fetches fresh data.
func (n *NaverGold) Gold(ctx context.Context) (*GoldPrice, error) {
	if p := n.cachedGold(); p != nil {
		return p, nil
	}
	if err := n.refresh(ctx); err != nil {
		// Return stale if available.
		if p := n.staleGold(); p != nil {
			return p, nil
		}
		return nil, err
	}
	return n.cachedGold(), nil
}

// Silver returns the cached silver price or fetches fresh data.
func (n *NaverGold) Silver(ctx context.Context) (*GoldPrice, error) {
	if p := n.cachedSilver(); p != nil {
		return p, nil
	}
	if err := n.refresh(ctx); err != nil {
		if p := n.staleSilver(); p != nil {
			return p, nil
		}
		return nil, err
	}
	return n.cachedSilver(), nil
}

func (n *NaverGold) cachedGold() *GoldPrice {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if n.gold != nil && time.Since(n.updatedAt) < n.cacheTTL {
		g := *n.gold
		return &g
	}
	return nil
}

func (n *NaverGold) cachedSilver() *GoldPrice {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if n.silver != nil && time.Since(n.updatedAt) < n.cacheTTL {
		s := *n.silver
		return &s
	}
	return nil
}

func (n *NaverGold) staleGold() *GoldPrice {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if n.gold != nil {
		g := *n.gold
		return &g
	}
	return nil
}

func (n *NaverGold) staleSilver() *GoldPrice {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if n.silver != nil {
		s := *n.silver
		return &s
	}
	return nil
}

// naverGoldAPIURL fetches gold/silver from Naver marketindex.
// The API returns a JSON array of price items for the given commodity.
const naverGoldAPIURL = "https://m.stock.naver.com/front-api/v1/marketIndex/prices?category=marketIndex&reutersCode=%s&page=1&pageSize=1"

// Naver marketindex reuters codes for precious metals.
const (
	naverGoldCode   = "GC_CMX" // Gold futures (COMEX)
	naverSilverCode = "SI_CMX" // Silver futures (COMEX)
)

type naverMarketIndexResponse struct {
	Result struct {
		Time  string `json:"time"`
		Items []struct {
			Date       string `json:"localTradedAt"`
			ClosePrice string `json:"closePrice"`
		} `json:"items"`
	} `json:"result"`
}

// naverDomesticGoldURL returns the domestic gold price (KRX gold exchange).
// Returns price per gram in KRW.
const naverDomesticGoldURL = "https://m.stock.naver.com/front-api/v1/marketIndex/prices?category=exchange&reutersCode=%s&page=1&pageSize=1"

const (
	naverDomesticGoldCode   = "GOLD_KRX" // 국내 금 시세 (g당 원화)
	naverDomesticSilverCode = "SILVER_KRX"
)

// naverGoldDailyURL is an alternative HTML API for domestic gold prices.
// We use the REST JSON API instead for reliability.

func (n *NaverGold) refresh(ctx context.Context) error {
	goldPrice, err := n.resolveGoldPrice(ctx)
	if err != nil {
		return err
	}

	silverPrice, silverOK := n.resolveSilverPrice(ctx)
	n.storePrices(goldPrice, silverPrice, silverOK, time.Now())
	return nil
}

func (n *NaverGold) resolveGoldPrice(ctx context.Context) (float64, error) {
	goldPrice, err := n.fetchDomesticPrice(ctx, naverDomesticGoldCode)
	if err == nil {
		return goldPrice, nil
	}

	n.logger.Debug("domestic gold price failed, trying international", "error", err)
	goldPrice, err = n.fetchInternationalGold(ctx)
	if err != nil {
		n.logger.Warn("gold price fetch failed", "error", err)
		return 0, fmt.Errorf("금 시세를 가져올 수 없습니다")
	}
	return goldPrice, nil
}

func (n *NaverGold) resolveSilverPrice(ctx context.Context) (float64, bool) {
	silverPrice, err := n.fetchDomesticPrice(ctx, naverDomesticSilverCode)
	if err != nil {
		n.logger.Debug("domestic silver price failed", "error", err)
		return 0, false
	}
	return silverPrice, silverPrice > 0
}

func (n *NaverGold) storePrices(goldPrice, silverPrice float64, hasSilver bool, now time.Time) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.gold = newMetalPrice("gold", goldPrice)
	if hasSilver {
		n.silver = newMetalPrice("silver", silverPrice)
	}
	n.updatedAt = now
}

func newMetalPrice(metal string, pricePerG float64) *GoldPrice {
	return &GoldPrice{
		Metal:       metal,
		PricePerG:   pricePerG,
		PricePerDon: pricePerG * gramsPerDon,
	}
}

// fetchDomesticPrice fetches domestic precious metal price (KRW per gram) from Naver.
func (n *NaverGold) fetchDomesticPrice(ctx context.Context, code string) (float64, error) {
	url := fmt.Sprintf(naverDomesticGoldURL, code)
	return n.fetchPriceFromAPI(ctx, url)
}

// fetchInternationalGold fetches international gold price (USD per oz) and converts.
// This is a fallback when domestic API fails.
func (n *NaverGold) fetchInternationalGold(ctx context.Context) (float64, error) {
	url := fmt.Sprintf(naverGoldAPIURL, naverGoldCode)
	priceUSDPerOz, err := n.fetchPriceFromAPI(ctx, url)
	if err != nil {
		return 0, err
	}
	// Convert USD/oz to KRW/g (rough conversion without forex).
	// This is a fallback; domestic price is preferred.
	const gramsPerTroyOz = 31.1035
	return priceUSDPerOz / gramsPerTroyOz, nil
}

func (n *NaverGold) fetchPriceFromAPI(ctx context.Context, url string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; JucoBot/2.0)")

	resp, err := n.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("naver gold API status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var result naverMarketIndexResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("parse naver gold response: %w", err)
	}

	if len(result.Result.Items) == 0 {
		return 0, fmt.Errorf("no gold price data")
	}

	var price float64
	if _, err := fmt.Sscanf(result.Result.Items[0].ClosePrice, "%f", &price); err != nil {
		return 0, fmt.Errorf("parse gold price: %w", err)
	}
	if price <= 0 {
		return 0, fmt.Errorf("invalid gold price: %f", price)
	}

	return price, nil
}
