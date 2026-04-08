package formatter

import (
	"strings"
	"testing"

	"github.com/shift-click/masterbot/internal/scraper/providers"
)

func TestFormatForexRates(t *testing.T) {
	rates := map[string]providers.CurrencyRate{
		"USD": {Country: "미국", BasePrice: 1487.10, CurrencyUnit: 1, SignedChangePrice: -2.30},
		"JPY": {Country: "일본", BasePrice: 935.11, CurrencyUnit: 100, SignedChangePrice: 1.05},
		"CNY": {Country: "중국", BasePrice: 216.05, CurrencyUnit: 1, SignedChangePrice: 0},
		"EUR": {Country: "유럽", BasePrice: 1715.67, CurrencyUnit: 1, SignedChangePrice: -3.12},
		"THB": {Country: "태국", BasePrice: 46.03, CurrencyUnit: 1, SignedChangePrice: 0.08},
		"TWD": {Country: "대만", BasePrice: 46.67, CurrencyUnit: 1, SignedChangePrice: -0.11},
		"HKD": {Country: "홍콩", BasePrice: 189.76, CurrencyUnit: 1, SignedChangePrice: 0},
		"VND": {Country: "베트남", BasePrice: 5.67, CurrencyUnit: 100, SignedChangePrice: 0},
	}
	order := []string{"USD", "JPY", "CNY", "EUR", "THB", "TWD", "HKD", "VND"}

	got := FormatForexRates(rates, order)

	// Header.
	if !strings.HasPrefix(got, "💱 환율") {
		t.Errorf("missing header, got:\n%s", got)
	}

	// Check each currency line.
	checks := []struct {
		contains string
	}{
		{"미국: 1,487.10 ▼2.30"},
		{"일본: 935.11 ▲1.05"},
		{"중국: 216.05 ―"},
		{"유럽: 1,715.67 ▼3.12"},
		{"태국: 46.03 ▲0.08"},
		{"대만: 46.67 ▼0.11"},
		{"홍콩: 189.76 ―"},
		{"베트남: 5.67 ―"},
	}
	for _, check := range checks {
		if !strings.Contains(got, check.contains) {
			t.Errorf("missing %q in output:\n%s", check.contains, got)
		}
	}
}

func TestFormatForexRatesOrder(t *testing.T) {
	rates := map[string]providers.CurrencyRate{
		"USD": {Country: "미국", BasePrice: 1487.10},
		"JPY": {Country: "일본", BasePrice: 935.11},
	}

	got := FormatForexRates(rates, []string{"JPY", "USD"})
	lines := strings.Split(got, "\n")

	// lines[0] = header, lines[1] = JPY, lines[2] = USD
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(lines))
	}
	if !strings.HasPrefix(lines[1], "일본:") {
		t.Errorf("line 1 should start with 일본, got %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "미국:") {
		t.Errorf("line 2 should start with 미국, got %q", lines[2])
	}
}

func TestFormatForexRatesEmpty(t *testing.T) {
	got := FormatForexRates(map[string]providers.CurrencyRate{}, []string{"USD"})
	if got != "💱 환율" {
		t.Errorf("expected header only, got:\n%s", got)
	}
}

func TestFormatForexChange(t *testing.T) {
	tests := []struct {
		change float64
		want   string
	}{
		{2.30, "▲2.30"},
		{-3.12, "▼3.12"},
		{0, "―"},
		{0.08, "▲0.08"},
		{-0.11, "▼0.11"},
	}
	for _, tt := range tests {
		got := formatForexChange(tt.change)
		if got != tt.want {
			t.Errorf("formatForexChange(%v) = %q, want %q", tt.change, got, tt.want)
		}
	}
}
