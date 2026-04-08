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

// forexCurrencies defines the currencies to fetch from Dunamu CDN.
// Order determines display order.
var forexCurrencies = []struct {
	Code         string // Dunamu code suffix, e.g. "USD"
	DunamuCode   string // full Dunamu code, e.g. "FRX.KRWUSD"
	CurrencyUnit int    // 1 or 100
}{
	{"USD", "FRX.KRWUSD", 1},
	{"JPY", "FRX.KRWJPY", 100},
	{"CNY", "FRX.KRWCNY", 1},
	{"EUR", "FRX.KRWEUR", 1},
	{"THB", "FRX.KRWTHB", 1},
	{"TWD", "FRX.KRWTWD", 1},
	{"HKD", "FRX.KRWHKD", 1},
	{"VND", "FRX.KRWVND", 100},
}

// ForexDisplayOrder returns the canonical display order of currency codes.
func ForexDisplayOrder() []string {
	out := make([]string, len(forexCurrencies))
	for i, c := range forexCurrencies {
		out[i] = c.Code
	}
	return out
}

// DunamuForex fetches multi-currency KRW exchange rates from Dunamu CDN API.
type DunamuForex struct {
	client *BreakerHTTPClient
	logger *slog.Logger

	mu    sync.RWMutex
	rate  ForexRate
	rates MultiForexRates
}

// NewDunamuForex creates a new DunamuForex provider.
func NewDunamuForex(logger *slog.Logger) *DunamuForex {
	if logger == nil {
		logger = slog.Default()
	}
	return &DunamuForex{
		client: DefaultBreakerClient(5 * time.Second, "dunamu_forex", logger),
		logger: logger.With("component", "dunamu_forex"),
		rates:  MultiForexRates{Rates: make(map[string]CurrencyRate)},
	}
}

// SetTransport replaces the underlying HTTP transport (for testing).
func (d *DunamuForex) SetTransport(rt http.RoundTripper) { d.client.SetTransport(rt) }

// Rate returns the current cached USD/KRW exchange rate (backward compatible).
func (d *DunamuForex) Rate() ForexRate {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.rate
}

// Rates returns the current cached multi-currency exchange rates.
func (d *DunamuForex) Rates() MultiForexRates {
	d.mu.RLock()
	defer d.mu.RUnlock()
	// Return a shallow copy so callers can't mutate the map.
	cp := MultiForexRates{
		Rates:     make(map[string]CurrencyRate, len(d.rates.Rates)),
		UpdatedAt: d.rates.UpdatedAt,
	}
	for k, v := range d.rates.Rates {
		cp.Rates[k] = v
	}
	return cp
}

// FetchRate fetches multi-currency KRW exchange rates.
// Tries Dunamu CDN first, falls back to open.er-api.com.
func (d *DunamuForex) FetchRate(ctx context.Context) (ForexRate, error) {
	rates, err := d.fetchDunamuMulti(ctx)
	if err != nil {
		d.logger.Debug("dunamu forex failed, trying fallback", "error", err)
		rates, err = d.fetchOpenERMulti(ctx)
		if err != nil {
			return ForexRate{}, fmt.Errorf("all forex sources failed: %w", err)
		}
	}

	d.mu.Lock()
	d.rates = rates
	if usd, ok := rates.Rates["USD"]; ok {
		d.rate = ForexRate{Rate: usd.BasePrice, UpdatedAt: rates.UpdatedAt}
	}
	d.mu.Unlock()

	return d.Rate(), nil
}

func (d *DunamuForex) fetchDunamuMulti(ctx context.Context) (MultiForexRates, error) {
	codes := ""
	for i, c := range forexCurrencies {
		if i > 0 {
			codes += ","
		}
		codes += c.DunamuCode
	}
	url := "https://quotation-api-cdn.dunamu.com/v1/forex/recent?codes=" + codes

	body, err := d.doGet(ctx, url)
	if err != nil {
		return MultiForexRates{}, fmt.Errorf("dunamu request: %w", err)
	}

	var items []struct {
		Code              string  `json:"code"`
		CurrencyCode      string  `json:"currencyCode"`
		Country           string  `json:"country"`
		BasePrice         float64 `json:"basePrice"`
		CurrencyUnit      int     `json:"currencyUnit"`
		SignedChangePrice  float64 `json:"signedChangePrice"`
		SignedChangeRate   float64 `json:"signedChangeRate"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		return MultiForexRates{}, fmt.Errorf("dunamu parse: %w", err)
	}
	if len(items) == 0 {
		return MultiForexRates{}, fmt.Errorf("dunamu: empty response")
	}

	now := time.Now()
	result := MultiForexRates{
		Rates:     make(map[string]CurrencyRate, len(items)),
		UpdatedAt: now,
	}

	// Merge: keep previous cache for currencies not in this response.
	d.mu.RLock()
	for k, v := range d.rates.Rates {
		result.Rates[k] = v
	}
	d.mu.RUnlock()

	for _, item := range items {
		result.Rates[item.CurrencyCode] = CurrencyRate{
			Code:              item.Code,
			CurrencyCode:      item.CurrencyCode,
			Country:           item.Country,
			BasePrice:         item.BasePrice,
			CurrencyUnit:      item.CurrencyUnit,
			SignedChangePrice:  item.SignedChangePrice,
			SignedChangeRate:   item.SignedChangeRate,
		}
	}

	return result, nil
}

func (d *DunamuForex) fetchOpenERMulti(ctx context.Context) (MultiForexRates, error) {
	const url = "https://open.er-api.com/v6/latest/USD"

	body, err := d.doGet(ctx, url)
	if err != nil {
		return MultiForexRates{}, fmt.Errorf("open.er-api request: %w", err)
	}

	var resp struct {
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return MultiForexRates{}, fmt.Errorf("open.er-api parse: %w", err)
	}

	krw, ok := resp.Rates["KRW"]
	if !ok {
		return MultiForexRates{}, fmt.Errorf("open.er-api: KRW not found")
	}

	// open.er-api gives rates as "1 USD = X <currency>".
	// We need KRW per <currency_unit> of each currency.
	// KRW per 1 unit of currency = KRW_rate / currency_rate
	openERCodeMap := map[string]string{
		"USD": "USD", "JPY": "JPY", "CNY": "CNY", "EUR": "EUR",
		"THB": "THB", "TWD": "TWD", "HKD": "HKD", "VND": "VND",
	}

	now := time.Now()
	result := MultiForexRates{
		Rates:     make(map[string]CurrencyRate, len(forexCurrencies)),
		UpdatedAt: now,
	}

	for _, fc := range forexCurrencies {
		openERKey, exists := openERCodeMap[fc.Code]
		if !exists {
			continue
		}
		foreignRate, ok := resp.Rates[openERKey]
		if !ok || foreignRate == 0 {
			continue
		}

		// KRW per 1 unit = krw / foreignRate
		// KRW per currencyUnit = (krw / foreignRate) * currencyUnit
		basePrice := (krw / foreignRate) * float64(fc.CurrencyUnit)

		result.Rates[fc.Code] = CurrencyRate{
			Code:         fc.DunamuCode,
			CurrencyCode: fc.Code,
			Country:      "", // open.er-api doesn't provide country names
			BasePrice:    basePrice,
			CurrencyUnit: fc.CurrencyUnit,
		}
	}

	return result, nil
}

func (d *DunamuForex) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// SetRatesForTest sets cached rates directly (for testing).
func (d *DunamuForex) SetRatesForTest(rates MultiForexRates) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rates = rates
	if usd, ok := rates.Rates["USD"]; ok {
		d.rate = ForexRate{Rate: usd.BasePrice, UpdatedAt: rates.UpdatedAt}
	}
}

// StartPolling starts background polling at the given interval.
// Blocks until ctx is cancelled.
func (d *DunamuForex) StartPolling(ctx context.Context, interval time.Duration) {
	d.logger.Info("forex polling started", "interval", interval)

	// Initial fetch.
	if _, err := d.FetchRate(ctx); err != nil {
		d.logger.Warn("initial forex fetch failed", "error", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.logger.Info("forex polling stopped")
			return
		case <-ticker.C:
			if _, err := d.FetchRate(ctx); err != nil {
				d.logger.Warn("forex poll failed", "error", err)
			}
		}
	}
}
