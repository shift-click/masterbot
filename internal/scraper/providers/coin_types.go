package providers

import (
	"strings"
	"time"
)

// CoinTier represents which data tier a coin was resolved from.
type CoinTier int

const (
	CoinTierCEX       CoinTier = iota + 1 // Tier 1: CEX (Binance+Upbit WS)
	CoinTierCoinGecko                     // Tier 2: CoinGecko registered coins
	CoinTierDEX                           // Tier 3: DEX (DexScreener)
)

type CoinQuoteVenue string

const (
	CoinQuoteVenueUnknown   CoinQuoteVenue = ""
	CoinQuoteVenueCEX       CoinQuoteVenue = "cex"
	CoinQuoteVenueCoinGecko CoinQuoteVenue = "coingecko"
	CoinQuoteVenueDEX       CoinQuoteVenue = "dex"
)

type CoinChartVenue string

const (
	CoinChartVenueUnknown CoinChartVenue = ""
	CoinChartVenueBinance CoinChartVenue = "binance"
	CoinChartVenueUpbit   CoinChartVenue = "upbit"
	CoinChartVenueDEX     CoinChartVenue = "dex"
)

// CoinSearchResult represents a resolved coin from the alias/search process.
type CoinSearchResult struct {
	Symbol              string         `json:"symbol"`                // display symbol, e.g. "BTC"
	Name                string         `json:"name"`                  // display name, e.g. "Bitcoin"
	Tier                CoinTier       `json:"tier"`                  // resolve provenance
	ContractAddress     string         `json:"contract_address"`      // DEX token contract
	PairAddress         string         `json:"pair_address"`          // DEX pool address
	ChainID             string         `json:"chain_id"`              // DEX chain id
	CoinGeckoID         string         `json:"coingecko_id"`          // CoinGecko coin id
	BinanceSymbol       string         `json:"binance_symbol"`        // symbol for Binance spot/USDT data
	UpbitMarket         string         `json:"upbit_market"`          // market code, e.g. "KRW-BTC"
	PreferredQuoteVenue CoinQuoteVenue `json:"preferred_quote_venue"` // optional quote venue preference
	PreferredChartVenue CoinChartVenue `json:"preferred_chart_venue"` // optional chart venue preference
}

func (r CoinSearchResult) EffectiveBinanceSymbol() string {
	if sym := strings.TrimSpace(r.BinanceSymbol); sym != "" {
		return strings.ToUpper(sym)
	}
	if r.Tier == CoinTierCEX {
		return strings.ToUpper(strings.TrimSpace(r.Symbol))
	}
	return ""
}

func (r CoinSearchResult) EffectiveUpbitMarket() string {
	if market := strings.TrimSpace(r.UpbitMarket); market != "" {
		return strings.ToUpper(market)
	}
	if r.Tier == CoinTierCEX {
		sym := strings.ToUpper(strings.TrimSpace(r.Symbol))
		if sym != "" {
			return "KRW-" + sym
		}
	}
	return ""
}

func (r CoinSearchResult) EffectiveUpbitSymbol() string {
	market := r.EffectiveUpbitMarket()
	if !strings.HasPrefix(market, "KRW-") {
		return ""
	}
	return strings.TrimPrefix(market, "KRW-")
}

func (r CoinSearchResult) HasCEXCapability() bool {
	return r.EffectiveBinanceSymbol() != "" || r.EffectiveUpbitMarket() != ""
}

func (r CoinSearchResult) HasCoinGeckoCapability() bool {
	return strings.TrimSpace(r.CoinGeckoID) != ""
}

func (r CoinSearchResult) HasDEXCapability() bool {
	return strings.TrimSpace(r.ContractAddress) != ""
}

// CEXQuote holds real-time CEX coin data (from Binance + Upbit WebSocket).
type CEXQuote struct {
	Symbol        string    `json:"symbol"`         // e.g. "BTC"
	Name          string    `json:"name"`           // e.g. "비트코인"
	USDPrice      float64   `json:"usd_price"`      // Binance USDT price
	USDChange     float64   `json:"usd_change"`     // 24h change amount USD
	USDChangePct  float64   `json:"usd_change_pct"` // 24h change percent
	USDPrevClose  float64   `json:"usd_prev_close"` // previous close USD
	KRWPrice      float64   `json:"krw_price"`      // Upbit KRW price
	KRWChange     float64   `json:"krw_change"`     // KRW change from prev close
	KRWChangePct  float64   `json:"krw_change_pct"` // KRW change percent
	KRWPrevClose  float64   `json:"krw_prev_close"` // Upbit prev_closing_price
	MarketCap     float64   `json:"market_cap"`     // from CoinGecko
	KimchiPremium float64   `json:"kimchi_premium"` // (KRW / (USD*rate) - 1) * 100
	UpdatedAt     time.Time `json:"updated_at"`
}

// DEXQuote holds DEX token data (from DexScreener).
type DEXQuote struct {
	Symbol          string    `json:"symbol"`
	Name            string    `json:"name"`
	ChainID         string    `json:"chain_id"` // e.g. "ethereum"
	DEXName         string    `json:"dex_name"` // e.g. "Uniswap V3"
	ContractAddress string    `json:"contract_address"`
	PairAddress     string    `json:"pair_address"`
	USDPrice        float64   `json:"usd_price"`
	USDChangePct24h float64   `json:"usd_change_pct_24h"`
	Volume24h       float64   `json:"volume_24h"`
	Liquidity       float64   `json:"liquidity"`
	MarketCap       float64   `json:"market_cap"`
	FDV             float64   `json:"fdv"`
	KRWPrice        float64   `json:"krw_price"` // calculated: usd * exchange rate
	KRWChangePct24h float64   `json:"krw_change_pct_24h"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ForexRate holds USD/KRW exchange rate data.
type ForexRate struct {
	Rate      float64   `json:"rate"` // e.g. 1450.0
	UpdatedAt time.Time `json:"updated_at"`
}

// CurrencyRate holds a single currency's KRW exchange rate from Dunamu.
type CurrencyRate struct {
	Code              string  `json:"code"`                // e.g. "FRX.KRWUSD"
	CurrencyCode      string  `json:"currency_code"`       // e.g. "USD"
	Country           string  `json:"country"`             // e.g. "미국"
	BasePrice         float64 `json:"base_price"`          // e.g. 1487.10
	CurrencyUnit      int     `json:"currency_unit"`       // 1 or 100
	SignedChangePrice float64 `json:"signed_change_price"` // e.g. -9.50
	SignedChangeRate  float64 `json:"signed_change_rate"`  // e.g. -0.0069
}

// MultiForexRates holds exchange rates for multiple currencies, keyed by currency code (e.g. "USD").
type MultiForexRates struct {
	Rates     map[string]CurrencyRate `json:"rates"`
	UpdatedAt time.Time               `json:"updated_at"`
}

// MarketCapEntry holds market cap data for a single coin.
type MarketCapEntry struct {
	CoinGeckoID string  `json:"coingecko_id"`
	Symbol      string  `json:"symbol"`
	MarketCap   float64 `json:"market_cap"`
}

// BinanceTickerUpdate represents a price update from Binance WS.
type BinanceTickerUpdate struct {
	Symbol    string // e.g. "BTC" (stripped from "BTCUSDT")
	Price     float64
	PrevClose float64
	Change    float64
	ChangePct float64
}

// UpbitTickerUpdate represents a price update from Upbit WS.
type UpbitTickerUpdate struct {
	Symbol     string  // e.g. "BTC" (stripped from "KRW-BTC")
	TradePrice float64 // current price
	PrevClose  float64 // prev_closing_price (00:00 KST)
	Change     float64 // signed_change_price
	ChangePct  float64 // signed_change_rate (0.0225 = 2.25%)
}
