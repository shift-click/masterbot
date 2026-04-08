package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/shift-click/masterbot/internal/scraper"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/pkg/formatter"
)

const (
	testSectionDivider = "══════════════════════════════════════"
	testSectionTopLine = "\n" + testSectionDivider
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	coinCache := scraper.NewCoinCache(logger)
	rate := runForexTest(ctx, logger, coinCache)
	coinGecko := runMarketCapTest(ctx, logger, coinCache)
	runAliasTest()
	runDexTest(ctx, logger, rate)
	runCEXFormatTest(rate, coinGecko)

	fmt.Println(testSectionTopLine)
	fmt.Println("  All tests complete!")
	fmt.Println(testSectionDivider)
}

func printSection(title string) {
	fmt.Println(testSectionTopLine)
	fmt.Println("  Test:", title)
	fmt.Println(testSectionDivider)
}

func runForexTest(ctx context.Context, logger *slog.Logger, coinCache *scraper.CoinCache) providers.ForexRate {
	printSection("Dunamu Forex Rate")
	forex := providers.NewDunamuForex(logger)
	rate, err := forex.FetchRate(ctx)
	if err != nil {
		fmt.Printf("  ❌ Forex failed: %v\n", err)
		return providers.ForexRate{}
	}
	fmt.Printf("  ✅ USD/KRW: %.2f\n", rate.Rate)
	coinCache.UpdateForexRate(rate.Rate)
	return rate
}

func runMarketCapTest(ctx context.Context, logger *slog.Logger, coinCache *scraper.CoinCache) *providers.CoinGecko {
	printSection("CoinGecko Market Caps")
	coinGecko := providers.NewCoinGecko(logger)
	if err := coinGecko.FetchMarketCaps(ctx); err != nil {
		fmt.Printf("  ❌ Market caps failed: %v\n", err)
		return coinGecko
	}
	for _, sym := range []string{"BTC", "ETH", "SOL"} {
		if mc, ok := coinGecko.MarketCap(sym); ok {
			fmt.Printf("  ✅ %s market cap: $%.0f\n", sym, mc)
			coinCache.UpdateMarketCap(sym, mc)
		}
	}
	return coinGecko
}

func runAliasTest() {
	printSection("Coin Aliases")
	aliases := providers.NewCoinAliases()
	testAliases := []string{"비트", "비트코인", "btc", "BTC", "이더", "ㅂㅌ", "솔라나", "PEPE"}
	for _, input := range testAliases {
		if sym, ok := aliases.Lookup(input); ok {
			fmt.Printf("  ✅ %q → %s\n", input, sym)
		} else {
			fmt.Printf("  ❌ %q → not found\n", input)
		}
	}
}

func runDexTest(ctx context.Context, logger *slog.Logger, rate providers.ForexRate) {
	printSection("DexScreener Search")
	dexScreener := providers.NewDexScreener(logger)
	dexQuote, err := dexScreener.Search(ctx, "PEPE")
	if err != nil {
		fmt.Printf("  ❌ DexScreener search failed: %v\n", err)
		return
	}
	fmt.Printf("  ✅ %s (%s) on %s\n", dexQuote.Name, dexQuote.Symbol, dexQuote.ChainID)
	fmt.Printf("     Price: $%f\n", dexQuote.USDPrice)
	fmt.Printf("     Liquidity: $%.0f\n", dexQuote.Liquidity)

	if rate.Rate > 0 {
		dexQuote.KRWPrice = dexQuote.USDPrice * rate.Rate
		dexQuote.KRWChangePct24h = dexQuote.USDChangePct24h
	}
	mcap := ""
	if dexQuote.MarketCap > 0 && rate.Rate > 0 {
		mcap = scraper.FormatMarketCapKRW(dexQuote.MarketCap, rate.Rate)
	}
	text := formatter.FormatDEXCoinQuote(formatter.DEXCoinData{
		Name:            dexQuote.Name,
		Symbol:          dexQuote.Symbol,
		ChainID:         dexQuote.ChainID,
		DEXName:         dexQuote.DEXName,
		USDPrice:        dexQuote.USDPrice,
		USDChangePct24h: dexQuote.USDChangePct24h,
		Volume24h:       dexQuote.Volume24h,
		Liquidity:       dexQuote.Liquidity,
		MarketCap:       mcap,
		KRWPrice:        dexQuote.KRWPrice,
		KRWChangePct:    dexQuote.KRWChangePct24h,
	})
	fmt.Printf("\n--- DEX 포맷 출력 ---\n%s\n", text)
}

func runCEXFormatTest(rate providers.ForexRate, coinGecko *providers.CoinGecko) {
	printSection("CEX Format (simulated data)")
	btcMC := float64(0)
	if mc, ok := coinGecko.MarketCap("BTC"); ok {
		btcMC = mc
	}
	mcapStr := ""
	if btcMC > 0 && rate.Rate > 0 {
		mcapStr = scraper.FormatMarketCapKRW(btcMC, rate.Rate)
	}

	cexText := formatter.FormatCEXCoinQuote(formatter.CEXCoinData{
		Name:          "비트코인",
		Symbol:        "BTC",
		MarketCap:     mcapStr,
		USDPrice:      73171.00,
		USDChangePct:  2.25,
		USDPrevClose:  71563.55,
		USDChange:     1607.45,
		KRWPrice:      107268686,
		KRWChangePct:  2.25,
		KRWPrevClose:  105267420,
		KRWChange:     2001266,
		KimchiPremium: -2.12,
		HasKimchi:     true,
	})
	fmt.Printf("\n--- CEX 포맷 출력 ---\n%s\n", cexText)
}
