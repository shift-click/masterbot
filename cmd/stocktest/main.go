package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/pkg/formatter"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	naver := providers.NewNaverStock(logger)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	testCases := []string{"삼전", "삼성전자", "카카오", "현대차", "NAVER"}

	for _, query := range testCases {
		fmt.Printf("\n══════════════════════════════════════\n")
		fmt.Printf("  Query: %q\n", query)
		fmt.Printf("══════════════════════════════════════\n")

		result, err := naver.Resolve(ctx, query)
		if err != nil {
			fmt.Printf("  ❌ Resolve failed: %v\n", err)
			continue
		}
		fmt.Printf("  ✅ Resolved: %s (%s) [%s]\n", result.Name, result.Code, result.Market)

		quote, err := naver.FetchQuote(ctx, result.Code)
		if err != nil {
			fmt.Printf("  ❌ FetchQuote failed: %v\n", err)
			continue
		}
		fmt.Printf("  ✅ Fetched: price=%s, change=%s%%\n", quote.Price, quote.ChangePercent)

		text := formatter.FormatStockQuote(formatter.StockData{
			Name:            quote.Name,
			Market:          quote.Market,
			Price:           quote.Price,
			PrevClose:       quote.PrevClose,
			Change:          quote.Change,
			ChangePercent:   quote.ChangePercent,
			ChangeDirection: quote.ChangeDirection,
			MarketCap:       quote.MarketCap,
			PER:             quote.PER,
			PBR:             quote.PBR,
			Revenue:         quote.Revenue,
			OperatingProfit: quote.OperatingProfit,
			ForeignNet:      quote.ForeignNet,
			InstitutionNet:  quote.InstitutionNet,
			IndividualNet:   quote.IndividualNet,
			TrendDate:       quote.TrendDate,
		})

		fmt.Printf("\n--- 포맷 출력 ---\n%s\n", text)
	}
}
