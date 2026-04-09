package formatter

import (
	"strings"
	"testing"
)

func TestFormatCEXCoinQuoteBranches(t *testing.T) {
	t.Parallel()

	full := FormatCEXCoinQuote(CEXCoinData{
		Name:          "비트코인",
		Symbol:        "BTC",
		MarketCap:     "2,100조",
		USDPrice:      73171,
		USDChangePct:  2.25,
		USDPrevClose:  71563.55,
		USDChange:     1607.45,
		KRWPrice:      107268686,
		KRWChangePct:  2.10,
		KRWPrevClose:  105267420,
		KRWChange:     2001266,
		KimchiPremium: -2.12,
		HasKimchi:     true,
	})
	for _, token := range []string{"비트코인 | BTC", "시총: 2,100조", "USD:", "KRW:", "🌶️: -2.12%"} {
		if !strings.Contains(full, token) {
			t.Fatalf("formatted cex quote missing %q:\n%s", token, full)
		}
	}
	if !strings.Contains(full, "▲") {
		t.Fatalf("expected 상승 화살표 in cex quote:\n%s", full)
	}

	krwOnly := FormatCEXCoinQuote(CEXCoinData{
		Name:         "테스트",
		KRWPrice:     1000,
		KRWChangePct: -1.5,
		KRWPrevClose: 1020,
		KRWChange:    -20,
	})
	if strings.Contains(krwOnly, "USD:") || strings.Contains(krwOnly, "🌶️") {
		t.Fatalf("unexpected sections in KRW-only quote:\n%s", krwOnly)
	}
	if !strings.Contains(krwOnly, "▼") {
		t.Fatalf("expected 하락 화살표 in KRW-only quote:\n%s", krwOnly)
	}
}

func TestFormatDEXCoinQuoteBranches(t *testing.T) {
	t.Parallel()

	lowLiquidity := FormatDEXCoinQuote(DEXCoinData{
		Name:            "Pepe",
		Symbol:          "PEPE",
		ChainID:         "ethereum",
		DEXName:         "uniswap",
		USDPrice:        0.00000123,
		USDChangePct24h: -4.2,
		Volume24h:       25000,
		Liquidity:       9500,
		MarketCap:       "120억",
		KRWPrice:        0.0017,
		KRWChangePct:    -4.2,
	})
	for _, token := range []string{"Pepe | PEPE", "체인: Ethereum (Uniswap V3)", "24h Vol:", "유동성:", "시총: 120억", "KRW:", "유동성 주의"} {
		if !strings.Contains(lowLiquidity, token) {
			t.Fatalf("formatted dex quote missing %q:\n%s", token, lowLiquidity)
		}
	}

	normal := FormatDEXCoinQuote(DEXCoinData{
		Name:            "Token",
		Symbol:          "TKN",
		ChainID:         "unknown-chain",
		DEXName:         "unknown-dex",
		USDPrice:        1.25,
		USDChangePct24h: 0,
		Liquidity:       50000,
	})
	if strings.Contains(normal, "유동성 주의") {
		t.Fatalf("unexpected low-liquidity warning:\n%s", normal)
	}
	if !strings.Contains(normal, "⚠️ DEX 토큰") {
		t.Fatalf("expected dex warning footer:\n%s", normal)
	}
}

func TestCoinFormatterHelpers(t *testing.T) {
	t.Parallel()

	if sign, arrow := changeSymbols(1); sign != "+" || arrow != "▲" {
		t.Fatalf("changeSymbols positive = (%q,%q)", sign, arrow)
	}
	if sign, arrow := changeSymbols(-1); sign != "-" || arrow != "▼" {
		t.Fatalf("changeSymbols negative = (%q,%q)", sign, arrow)
	}
	if sign, arrow := changeSymbols(0); sign != "" || arrow != "-" {
		t.Fatalf("changeSymbols zero = (%q,%q)", sign, arrow)
	}

	if got := formatUSDPrice(10); got != "10.00" {
		t.Fatalf("formatUSDPrice >=1 = %q", got)
	}
	if got := formatUSDPrice(0.1234); got != "0.1234" {
		t.Fatalf("formatUSDPrice >=0.01 = %q", got)
	}
	if got := formatUSDPrice(0.001234); got != "0.001234" {
		t.Fatalf("formatUSDPrice >=0.0001 = %q", got)
	}
	if got := formatUSDPrice(0.00001234); got != "0.00001234" {
		t.Fatalf("formatUSDPrice small = %q", got)
	}

	if got := formatKRWPrice(1234567); got != "1,234,567" {
		t.Fatalf("formatKRWPrice >=1 = %q", got)
	}
	if got := formatKRWPrice(0.1); got != "0.1000" {
		t.Fatalf("formatKRWPrice >=0.01 = %q", got)
	}

	if got := addCommasInt(1_234_567); got != "1,234,567" {
		t.Fatalf("addCommasInt positive = %q", got)
	}
	if got := addCommasInt(-1_234_567); got != "-1,234,567" {
		t.Fatalf("addCommasInt negative = %q", got)
	}
	if got := addCommasFloat(1234.56, 2); got != "1,234.56" {
		t.Fatalf("addCommasFloat decimals = %q", got)
	}

	if got := formatCompactUSD(1_500_000_000); got != "$1.5B" {
		t.Fatalf("formatCompactUSD B = %q", got)
	}
	if got := formatCompactUSD(2_500_000); got != "$2.5M" {
		t.Fatalf("formatCompactUSD M = %q", got)
	}
	if got := formatCompactUSD(12_500); got != "$12.5K" {
		t.Fatalf("formatCompactUSD K = %q", got)
	}
	if got := formatCompactUSD(999); got != "$999" {
		t.Fatalf("formatCompactUSD plain = %q", got)
	}

	if got := formatChainName("bsc"); got != "BSC" {
		t.Fatalf("formatChainName known = %q", got)
	}
	if got := formatChainName("mychain"); got != "mychain" {
		t.Fatalf("formatChainName unknown = %q", got)
	}
	if got := formatDEXName("pancakeswap"); got != "PancakeSwap" {
		t.Fatalf("formatDEXName known = %q", got)
	}
	if got := formatDEXName("mydex"); got != "mydex" {
		t.Fatalf("formatDEXName unknown = %q", got)
	}
}

func TestGoldAndQuantityFormatters(t *testing.T) {
	t.Parallel()

	gold := FormatGoldQuote(GoldData{
		Metal:    "금",
		Quantity: 2,
		Unit:     "돈",
		Grams:    7.5,
		PriceKRW: 1_791_698,
	})
	if !strings.Contains(gold, "금 2.0돈 (7.50g)") || !strings.Contains(gold, "= 1,791,698원") {
		t.Fatalf("unexpected gold format:\n%s", gold)
	}

	silver := FormatGoldQuote(GoldData{
		Metal:    "은",
		Quantity: 10,
		Unit:     "g",
		AltQty:   2.67,
		PriceKRW: 13000,
	})
	if !strings.Contains(silver, "은 10.0g (2.67돈)") {
		t.Fatalf("unexpected g-unit format:\n%s", silver)
	}

	oz := FormatGoldQuote(GoldData{
		Metal:    "금",
		Quantity: 1,
		Unit:     "oz",
		Grams:    31.1,
		PriceKRW: 3200000,
	})
	if !strings.Contains(oz, "금 1.0oz (31.10g)") {
		t.Fatalf("unexpected oz-unit format:\n%s", oz)
	}

	other := FormatGoldQuote(GoldData{
		Metal:    "금",
		Quantity: 3,
		Unit:     "kg",
		PriceKRW: 100,
	})
	if !strings.Contains(other, "금 3.0kg") {
		t.Fatalf("unexpected fallback unit format:\n%s", other)
	}

	coinQty := FormatCoinQuantity(CoinQuantityData{
		Name:     "솔라나",
		Symbol:   "SOL",
		Quantity: 2.5,
		USDTotal: 188.2,
		KRWTotal: 275420,
	})
	if !strings.Contains(coinQty, "솔라나(SOL) × 2.5") || !strings.Contains(coinQty, "$188.20 (≈275,420원)") {
		t.Fatalf("unexpected coin quantity format:\n%s", coinQty)
	}

	coinKRWOnly := FormatCoinQuantity(CoinQuantityData{
		Name:     "코인",
		Quantity: 1,
		KRWTotal: 1500,
	})
	if !strings.Contains(coinKRWOnly, "1,500원") {
		t.Fatalf("unexpected krw-only coin quantity:\n%s", coinKRWOnly)
	}

	stockUSD := FormatStockQuantity(StockQuantityData{
		Name:       "애플",
		SymbolCode: "AAPL",
		Quantity:   3,
		Price:      180.5,
		Currency:   "USD",
	})
	if !strings.Contains(stockUSD, "애플(AAPL) × 3") || !strings.Contains(stockUSD, "$541.50") {
		t.Fatalf("unexpected usd stock quantity format:\n%s", stockUSD)
	}

	stockKRW := FormatStockQuantity(StockQuantityData{
		Name:     "삼성전자",
		Quantity: 10,
		Price:    56800,
		Currency: "KRW",
	})
	if !strings.Contains(stockKRW, "568,000원") {
		t.Fatalf("unexpected krw stock quantity format:\n%s", stockKRW)
	}

	if got := FormatCalcResult(12000); got != "12,000" {
		t.Fatalf("FormatCalcResult integer = %q", got)
	}
	if got := FormatCalcResult(1234.5); got != "1,234.5" {
		t.Fatalf("FormatCalcResult decimal = %q", got)
	}
	if got := formatQuantity(2); got != "2" {
		t.Fatalf("formatQuantity integer = %q", got)
	}
	if got := formatQuantity(2.5); got != "2.5" {
		t.Fatalf("formatQuantity decimal = %q", got)
	}
}
