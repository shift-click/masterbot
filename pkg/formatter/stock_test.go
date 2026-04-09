package formatter

import (
	"strings"
	"testing"
)

func TestFormatWorldStockQuote_FullData(t *testing.T) {
	t.Parallel()

	got := FormatStockQuote(StockData{
		Name:            "알파벳 Class A",
		Market:          "NASDAQ",
		Price:           "305.56",
		PrevClose:       "302.28",
		Change:          "3.28",
		ChangePercent:   "1.09",
		ChangeDirection: "RISING",
		MarketCap:       "3,034조 7,909억원",
		PER:             "27.23배",
		PBR:             "8.89배",
		Revenue:         "60066",
		EBITDA:          "23317",
		IsWorldStock:    true,
		Currency:        "USD",
		SymbolCode:      "GOOGL",
	})

	// Header should include symbol code.
	if !strings.Contains(got, "알파벳 Class A (GOOGL) | NASDAQ") {
		t.Errorf("header mismatch, got:\n%s", got)
	}

	// Market cap should strip "원" suffix.
	if !strings.Contains(got, "시총: 3,034조 7,909억") {
		t.Errorf("market cap mismatch, got:\n%s", got)
	}
	if strings.Contains(got, "억원") {
		t.Errorf("should strip 원 from market cap, got:\n%s", got)
	}

	// Price label should use currency.
	if !strings.Contains(got, "USD: 305.56 (+1.09%)") {
		t.Errorf("price line mismatch, got:\n%s", got)
	}

	// Should NOT use "현재:" label.
	if strings.Contains(got, "현재:") {
		t.Errorf("should use currency label not 현재, got:\n%s", got)
	}

	// PER/PBR
	if !strings.Contains(got, "PER: 27.23배") {
		t.Errorf("PER mismatch, got:\n%s", got)
	}

	// EBITDA should be formatted.
	if !strings.Contains(got, "EBITDA:") {
		t.Errorf("missing EBITDA, got:\n%s", got)
	}

	// Should NOT contain investor trends.
	if strings.Contains(got, "외국인") || strings.Contains(got, "기관") {
		t.Errorf("should not contain investor trends, got:\n%s", got)
	}

	// Should NOT contain 영업이익.
	if strings.Contains(got, "영업이익") {
		t.Errorf("should not contain 영업이익 for world stock, got:\n%s", got)
	}
}

func TestFormatWorldStockQuote_FallingPrice(t *testing.T) {
	t.Parallel()

	got := FormatStockQuote(StockData{
		Name:            "테슬라",
		Market:          "NASDAQ",
		Price:           "220.30",
		PrevClose:       "225.00",
		Change:          "-4.70",
		ChangePercent:   "-2.09",
		ChangeDirection: "FALLING",
		IsWorldStock:    true,
		Currency:        "USD",
		SymbolCode:      "TSLA",
	})

	if !strings.Contains(got, "USD: 220.30 (-2.09%)") {
		t.Errorf("falling price format mismatch, got:\n%s", got)
	}
	if !strings.Contains(got, "▼") {
		t.Errorf("missing falling arrow, got:\n%s", got)
	}
}

func TestFormatWorldStockQuote_DelegatesToWorldFormat(t *testing.T) {
	t.Parallel()

	// When IsWorldStock is true, FormatStockQuote should produce world format.
	world := FormatStockQuote(StockData{
		Name:         "테슬라",
		Market:       "NASDAQ",
		Price:        "220.30",
		IsWorldStock: true,
		Currency:     "USD",
		SymbolCode:   "TSLA",
	})

	// When IsWorldStock is false, FormatStockQuote should produce Korean format.
	korean := FormatStockQuote(StockData{
		Name:   "삼성전자",
		Market: "KOSPI",
		Price:  "188800",
	})

	// World format should have USD label.
	if !strings.Contains(world, "USD:") {
		t.Errorf("world format should have USD label")
	}

	// Korean format should have 현재 label.
	if !strings.Contains(korean, "현재:") {
		t.Errorf("korean format should have 현재 label")
	}
}

func TestFormatAmountBillions_WorldStockRevenue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{"60066", "6조 66억"},        // ~600조 (매출 in 억원)
		{"23317", "2조 3,317억"},      // ~233조 (EBITDA in 억원)
		{"0", "0"},
		{"", ""},
		{"-", "-"},
		{"500", "500억"},
	}

	for _, tt := range tests {
		got := FormatAmountBillions(tt.input)
		if got != tt.expected {
			t.Errorf("FormatAmountBillions(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
