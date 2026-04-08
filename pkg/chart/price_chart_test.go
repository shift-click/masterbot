package chart

import (
	"bytes"
	"image/png"
	"testing"
)

func TestDrawPriceChart_MinimalData(t *testing.T) {
	data := PriceChartData{
		Prices:       []int{10000, 11000},
		CurrentPrice: 11000,
	}
	b, err := DrawPriceChart(data, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertValidPNG(t, b)
}

func TestDrawPriceChart_ManyPoints(t *testing.T) {
	data := PriceChartData{
		Prices:       []int{19000, 19500, 20000, 19800, 18500, 18000, 18200, 19300, 20100, 19700},
		CurrentPrice: 19700,
		LowestPrice:  18000,
		HighestPrice: 20100,
		MeanPrice:    19220,
		ProductName:  "삼성전자 충전기 25W",
		PeriodLabel:  "3개월",
	}
	b, err := DrawPriceChart(data, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertValidPNG(t, b)
}

func TestDrawPriceChart_FlatPrices(t *testing.T) {
	data := PriceChartData{
		Prices:       []int{15000, 15000, 15000, 15000},
		CurrentPrice: 15000,
		LowestPrice:  14000,
	}
	b, err := DrawPriceChart(data, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertValidPNG(t, b)
}

func TestDrawPriceChart_SpikeAndDrop(t *testing.T) {
	data := PriceChartData{
		Prices:       []int{10000, 50000, 10000, 50000, 10000},
		CurrentPrice: 10000,
		ProductName:  "극단적 변동 테스트 상품",
	}
	b, err := DrawPriceChart(data, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertValidPNG(t, b)
}

func TestDrawPriceChart_LowestEqualsCurrent(t *testing.T) {
	data := PriceChartData{
		Prices:       []int{20000, 19000, 18000, 17000},
		CurrentPrice: 17000,
		LowestPrice:  17000,
		IsAtLowest:   true,
	}
	b, err := DrawPriceChart(data, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertValidPNG(t, b)
}

func TestDrawPriceChart_NoOptionalLabels(t *testing.T) {
	data := PriceChartData{
		Prices:       []int{10000, 12000, 11000},
		CurrentPrice: 11000,
		// ProductName and PeriodLabel intentionally empty
	}
	b, err := DrawPriceChart(data, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertValidPNG(t, b)
}

func TestDrawPriceChart_WithAllLabels(t *testing.T) {
	data := PriceChartData{
		Prices:       []int{10000, 12000, 11000},
		CurrentPrice: 11000,
		LowestPrice:  9500,
		ProductName:  "아주 긴 상품명 테스트 - 쿠팡 로켓배송 삼성전자 갤럭시 충전기 25W USB-C 타입 고속충전기 화이트",
		PeriodLabel:  "6개월",
	}
	b, err := DrawPriceChart(data, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertValidPNG(t, b)
}

func TestDrawPriceChart_TooFewPoints(t *testing.T) {
	data := PriceChartData{
		Prices: []int{10000},
	}
	_, err := DrawPriceChart(data, DefaultConfig())
	if err == nil {
		t.Fatal("expected error for single data point")
	}
}

func TestDrawPriceChartBase64(t *testing.T) {
	data := PriceChartData{
		Prices:       []int{10000, 11000, 10500},
		CurrentPrice: 10500,
	}
	s, err := DrawPriceChartBase64(data, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s) == 0 {
		t.Fatal("empty base64 string")
	}
}

func TestDrawPriceChart_ZeroConfig(t *testing.T) {
	data := PriceChartData{
		Prices:       []int{10000, 11000},
		CurrentPrice: 11000,
	}
	b, err := DrawPriceChart(data, ChartConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertValidPNG(t, b)
}

func TestDrawPriceChart_CandlestickMode(t *testing.T) {
	candles := makeTestCandles([]float64{
		100, 105, 115, 130, 125, 110, 95, 85,
		90, 100, 120, 140, 135, 120, 105, 95,
		100, 110, 125, 115, 105, 100,
	})
	data := PriceChartData{
		Candles:      candles,
		ProductName:  "비트코인 (BTC)",
		PeriodLabel:  "3개월",
		Direction:    DirectionUp,
		SubTitle:     "₩130,000 (+30.00%)",
		CurrentPrice: 100,
		AssetType:    "coin",
	}
	b, err := DrawPriceChart(data, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertValidPNG(t, b)
}

func TestDrawPriceChart_CandlestickDenseData(t *testing.T) {
	// 252 candles (1 year of daily) — tests narrow candle fallback.
	prices := make([]float64, 252)
	for i := range prices {
		prices[i] = 100 + float64(i%20)*5 - float64(i%7)*3
	}
	candles := makeTestCandles(prices)
	data := PriceChartData{
		Candles:      candles,
		ProductName:  "삼성전자 (005930)",
		PeriodLabel:  "1년",
		Direction:    DirectionDown,
		SubTitle:     "₩170,300 (-2.15%)",
		CurrentPrice: 170300,
		AssetType:    "stock",
	}
	b, err := DrawPriceChart(data, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertValidPNG(t, b)
}

func TestDrawPriceChart_CandlestickMinimal(t *testing.T) {
	candles := makeTestCandles([]float64{100, 110})
	data := PriceChartData{
		Candles:   candles,
		AssetType: "stock",
	}
	b, err := DrawPriceChart(data, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertValidPNG(t, b)
}

func TestFormatPrice(t *testing.T) {
	tests := []struct {
		name  string
		price float64
		want  string
	}{
		{name: "integer", price: 19300, want: "19,300.00"},
		{name: "decimal", price: 19300.126, want: "19,300.13"},
		{name: "small", price: 0.5, want: "0.50"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatPrice(tt.price); got != tt.want {
				t.Fatalf("formatPrice(%v) = %q, want %q", tt.price, got, tt.want)
			}
		})
	}
}

func assertValidPNG(t *testing.T, data []byte) {
	t.Helper()
	if len(data) == 0 {
		t.Fatal("empty PNG data")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("invalid PNG: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		t.Fatal("PNG has zero dimensions")
	}
}
