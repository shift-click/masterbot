package main

import (
	"fmt"
	"os"
	"time"

	"github.com/shift-click/masterbot/pkg/chart"
)

func main() {
	// Simulate volatile BTC 3-month data with clear swing highs/lows
	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	prices := []float64{
		95000, 98000, 102000, 108000, 115000, 120000, 118000, 112000,
		105000, 98000, 92000, 88000, 85000, 88000, 93000, 100000,
		108000, 115000, 122000, 128000, 135000, 140000, 138000, 132000,
		125000, 118000, 110000, 105000, 100000, 103000, 108000, 115000,
		122000, 128000, 130000, 126000, 120000, 115000, 110000, 108000,
		112000, 118000, 125000, 132000, 138000, 145000, 148000, 144000,
		138000, 130000, 122000, 118000, 115000, 118000, 122000, 128000,
		135000, 140000, 142000, 138000, 132000, 125000, 120000, 118000,
		122000, 128000, 135000, 142000, 148000, 150000, 146000, 140000,
		135000, 130000, 128000, 132000, 138000, 142000, 145000, 140000,
		135000, 130000, 128000, 132000, 136000, 140000, 138000, 134000,
		130000, 128000,
	}

	candles := make([]chart.CandlePoint, len(prices))
	for i, close := range prices {
		open := close
		if i > 0 {
			open = prices[i-1]
		}
		high := close
		low := close
		if open > high {
			high = open
		}
		if open < low {
			low = open
		}
		high += float64(1000 + (i%5)*500)
		low -= float64(1000 + (i%3)*500)

		candles[i] = chart.CandlePoint{
			Time:   baseTime.AddDate(0, 0, i),
			Open:   open,
			High:   high,
			Low:    low,
			Close:  close,
			Volume: float64(50000 + (i%10)*10000),
		}
	}

	currentPrice := int(prices[len(prices)-1])
	firstPrice := int(prices[0])
	dir := chart.DirectionUp
	if currentPrice < firstPrice {
		dir = chart.DirectionDown
	}

	changePct := float64(currentPrice-firstPrice) / float64(firstPrice) * 100

	data := chart.PriceChartData{
		Candles:      candles,
		ProductName:  "비트코인 (BTC)",
		PeriodLabel:  "3개월",
		Direction:    dir,
		SubTitle:     fmt.Sprintf("₩%d (+%.2f%%)", currentPrice, changePct),
		CurrentPrice: currentPrice,
		AssetType:    "coin",
	}

	pngBytes, err := chart.DrawPriceChart(data, chart.DefaultConfig())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile("/tmp/chart_debug.png", pngBytes, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote /tmp/chart_debug.png (%d bytes)\n", len(pngBytes))
}
