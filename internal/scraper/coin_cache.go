package scraper

import (
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/shift-click/masterbot/internal/scraper/providers"
)

// CoinCache is the unified in-memory cache for all coin data.
// CEX data is updated via WebSocket callbacks.
// DEX data is updated via CoinHotList polling.
// Market caps and forex rates are updated via background pollers.
type CoinCache struct {
	mu sync.RWMutex

	cex        map[string]*providers.CEXQuote // uppercase symbol -> CEX quote
	dex        map[string]*providers.DEXQuote // lowercase contract address -> DEX quote
	marketCaps map[string]float64             // uppercase symbol -> market cap
	forexRate  float64                        // USD/KRW rate

	coinNames map[string]string // uppercase symbol -> Korean name mapping

	logger *slog.Logger
}

// NewCoinCache creates a new CoinCache.
func NewCoinCache(logger *slog.Logger) *CoinCache {
	if logger == nil {
		logger = slog.Default()
	}
	return &CoinCache{
		cex:        make(map[string]*providers.CEXQuote),
		dex:        make(map[string]*providers.DEXQuote),
		marketCaps: make(map[string]float64),
		coinNames:  defaultCoinNames(),
		logger:     logger.With("component", "coin_cache"),
	}
}

// --- CEX data ---

// GetCEX returns the cached CEX quote for a symbol, or nil if not found.
func (c *CoinCache) GetCEX(symbol string) *providers.CEXQuote {
	c.mu.RLock()
	defer c.mu.RUnlock()
	q := c.cex[strings.ToUpper(symbol)]
	return q
}

// OnBinanceUpdate handles a price update from Binance WebSocket.
func (c *CoinCache) OnBinanceUpdate(u providers.BinanceTickerUpdate) {
	sym := strings.ToUpper(u.Symbol)

	c.mu.Lock()
	defer c.mu.Unlock()

	q, ok := c.cex[sym]
	if !ok {
		q = &providers.CEXQuote{Symbol: sym}
		if name, exists := c.coinNames[sym]; exists {
			q.Name = name
		} else {
			q.Name = sym
		}
		c.cex[sym] = q
	}

	q.USDPrice = u.Price
	q.USDPrevClose = u.PrevClose
	q.USDChange = u.Change
	q.USDChangePct = u.ChangePct
	q.UpdatedAt = time.Now()

	// Recalculate kimchi premium if KRW price and forex rate are available.
	c.recalcKimchi(q)
}

// OnUpbitUpdate handles a price update from Upbit WebSocket.
func (c *CoinCache) OnUpbitUpdate(u providers.UpbitTickerUpdate) {
	sym := strings.ToUpper(u.Symbol)

	c.mu.Lock()
	defer c.mu.Unlock()

	q, ok := c.cex[sym]
	if !ok {
		q = &providers.CEXQuote{Symbol: sym}
		if name, exists := c.coinNames[sym]; exists {
			q.Name = name
		} else {
			q.Name = sym
		}
		c.cex[sym] = q
	}

	q.KRWPrice = u.TradePrice
	q.KRWPrevClose = u.PrevClose
	q.KRWChange = u.Change
	q.KRWChangePct = u.ChangePct * 100 // Convert 0.0225 → 2.25
	q.UpdatedAt = time.Now()

	c.recalcKimchi(q)
}

// recalcKimchi recalculates kimchi premium. Must be called with lock held.
func (c *CoinCache) recalcKimchi(q *providers.CEXQuote) {
	if q.USDPrice > 0 && q.KRWPrice > 0 && c.forexRate > 0 {
		globalKRW := q.USDPrice * c.forexRate
		q.KimchiPremium = (q.KRWPrice/globalKRW - 1) * 100
	}
}

// --- DEX data ---

// GetDEX returns the cached DEX quote for a contract address, or nil if not found.
func (c *CoinCache) GetDEX(contractAddr string) *providers.DEXQuote {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.dex[strings.ToLower(contractAddr)]
}

// SetDEX stores a DEX quote in the cache.
func (c *CoinCache) SetDEX(quote *providers.DEXQuote) {
	if quote == nil || quote.ContractAddress == "" {
		return
	}
	addr := strings.ToLower(quote.ContractAddress)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Calculate KRW price from USD × forex rate.
	if quote.USDPrice > 0 && c.forexRate > 0 {
		quote.KRWPrice = quote.USDPrice * c.forexRate
		quote.KRWChangePct24h = quote.USDChangePct24h // same percent
	}

	c.dex[addr] = quote
}

// --- Market caps ---

// UpdateMarketCap sets the market cap for a symbol.
func (c *CoinCache) UpdateMarketCap(symbol string, marketCap float64) {
	sym := strings.ToUpper(symbol)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.marketCaps[sym] = marketCap

	// Also update in CEX quote if exists.
	if q, ok := c.cex[sym]; ok {
		q.MarketCap = marketCap
	}
}

// UpdateMarketCaps bulk-updates market caps.
func (c *CoinCache) UpdateMarketCaps(caps map[string]float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for sym, cap := range caps {
		sym = strings.ToUpper(sym)
		c.marketCaps[sym] = cap
		if q, ok := c.cex[sym]; ok {
			q.MarketCap = cap
		}
	}
}

// --- Forex rate ---

// UpdateForexRate sets the USD/KRW exchange rate.
func (c *CoinCache) UpdateForexRate(rate float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.forexRate = rate

	// Recalculate kimchi premium for all CEX quotes.
	for _, q := range c.cex {
		c.recalcKimchi(q)
	}

	// Recalculate KRW prices for all DEX quotes.
	for _, q := range c.dex {
		if q.USDPrice > 0 {
			q.KRWPrice = q.USDPrice * rate
		}
	}
}

// ForexRate returns the current USD/KRW rate.
func (c *CoinCache) ForexRate() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.forexRate
}

// --- Formatting helpers ---

// FormatMarketCapKRW formats a USD market cap to Korean-style KRW string.
// e.g. 1500000000000 USD × 1450 rate → "2,175조 억" style
func FormatMarketCapKRW(usdMarketCap, rate float64) string {
	if usdMarketCap <= 0 || rate <= 0 {
		return ""
	}
	krw := usdMarketCap * rate
	// Convert to 억원 unit.
	eok := krw / 100_000_000
	return formatKoreanAmount(eok)
}

// formatKoreanAmount formats a number in 억 units to Korean style.
func formatKoreanAmount(eok float64) string {
	absEok := math.Abs(eok)
	cho := int64(absEok / 10000)
	remainEok := int64(math.Mod(absEok, 10000))

	prefix := ""
	if eok < 0 {
		prefix = "-"
	}

	if cho > 0 && remainEok > 0 {
		return prefix + formatWithCommasCoin(cho) + "조 " + formatWithCommasCoin(remainEok) + "억"
	}
	if cho > 0 {
		return prefix + formatWithCommasCoin(cho) + "조"
	}
	return prefix + formatWithCommasCoin(remainEok) + "억"
}

func formatWithCommasCoin(n int64) string {
	if n < 0 {
		n = -n
	}
	s := ""
	for n > 0 {
		if s != "" {
			s = "," + s
		}
		rem := n % 1000
		n = n / 1000
		if n > 0 {
			// Pad with zeros.
			tmp := ""
			if rem < 10 {
				tmp = "00"
			} else if rem < 100 {
				tmp = "0"
			}
			s = tmp + itoa(rem) + s
		} else {
			s = itoa(rem) + s
		}
	}
	if s == "" {
		return "0"
	}
	return s
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

// defaultCoinNames returns Korean names for popular coins.
func defaultCoinNames() map[string]string {
	names := map[string]string{
		"BTC":   "비트코인",
		"ETH":   "이더리움",
		"XRP":   "리플",
		"SOL":   "솔라나",
		"ADA":   "에이다",
		"DOGE":  "도지코인",
		"DOT":   "폴카닷",
		"AVAX":  "아발란체",
		"MATIC": "폴리곤",
		"LINK":  "체인링크",
		"UNI":   "유니스왑",
		"ATOM":  "코스모스",
		"ETC":   "이더리움클래식",
		"XLM":   "스텔라루멘",
		"ALGO":  "알고랜드",
		"NEAR":  "니어프로토콜",
		"FTM":   "팬텀",
		"SAND":  "샌드박스",
		"MANA":  "디센트럴랜드",
		"AXS":   "엑시인피니티",
		"AAVE":  "에이브",
		"EOS":   "이오스",
		"TRX":   "트론",
		"SHIB":  "시바이누",
		"LTC":   "라이트코인",
		"BCH":   "비트코인캐시",
		"ARB":   "아비트럼",
		"OP":    "옵티미즘",
		"APT":   "앱토스",
		"SUI":   "수이",
		"SEI":   "세이",
		"PEPE":  "페페",
		"BONK":  "봉크",
		"WIF":   "위프",
		"FLOKI": "플로키",
	}
	for _, result := range providers.NewCoinAliases().BinanceSymbols() {
		if _, ok := names[result]; !ok {
			names[result] = result
		}
	}
	for _, result := range providers.NewCoinAliases().UpbitSymbols() {
		if _, ok := names[result]; !ok {
			names[result] = result
		}
	}
	for _, result := range providers.NewCoinAliases().LocalResults() {
		symbol := strings.ToUpper(strings.TrimSpace(result.Symbol))
		name := strings.TrimSpace(result.Name)
		if symbol == "" || name == "" {
			continue
		}
		if existing, ok := names[symbol]; !ok || existing == symbol {
			names[symbol] = name
		}
	}
	return names
}
