package formatter

import (
	"strings"
	"testing"
)

func TestFormatForexConvert_single(t *testing.T) {
	results := []ForexConvertResult{
		{Code: "USD", Amount: 100, KRW: 148580, RatePerUnit: 1485.80},
	}
	got := FormatForexConvert(results)

	if !strings.Contains(got, "💵") {
		t.Errorf("should contain USD emoji, got: %s", got)
	}
	if !strings.Contains(got, "$100") {
		t.Errorf("should contain $100, got: %s", got)
	}
	if !strings.Contains(got, "148,580원") {
		t.Errorf("should contain 148,580원, got: %s", got)
	}
	if !strings.Contains(got, "1,485.80원") {
		t.Errorf("should contain rate 1,485.80원, got: %s", got)
	}
}

func TestFormatForexConvert_multiple(t *testing.T) {
	results := []ForexConvertResult{
		{Code: "USD", Amount: 100, KRW: 148580, RatePerUnit: 1485.80},
		{Code: "CNY", Amount: 20, KRW: 4319, RatePerUnit: 215.93},
	}
	got := FormatForexConvert(results)

	// Should have two blocks separated by blank line.
	if !strings.Contains(got, "\n\n") {
		t.Errorf("multiple results should be separated by blank line, got: %s", got)
	}
	if !strings.Contains(got, "💵") || !strings.Contains(got, "💴") {
		t.Errorf("should contain both emojis, got: %s", got)
	}
}

func TestFormatForexConvert_VND(t *testing.T) {
	results := []ForexConvertResult{
		{Code: "VND", Amount: 100000, KRW: 5650, RatePerUnit: 0.0565},
	}
	got := FormatForexConvert(results)

	if !strings.Contains(got, "🇻🇳") {
		t.Errorf("should contain VND emoji, got: %s", got)
	}
	if !strings.Contains(got, "₫100,000") {
		t.Errorf("should contain ₫100,000, got: %s", got)
	}
	if !strings.Contains(got, "5,650원") {
		t.Errorf("should contain 5,650원, got: %s", got)
	}
	if !strings.Contains(got, "0.0565원") {
		t.Errorf("should contain rate 0.0565원, got: %s", got)
	}
}

func TestFormatForexConvert_decimalAmount(t *testing.T) {
	results := []ForexConvertResult{
		{Code: "USD", Amount: 99.5, KRW: 147837, RatePerUnit: 1485.80},
	}
	got := FormatForexConvert(results)

	if !strings.Contains(got, "$99.50") {
		t.Errorf("decimal amount should show 2 decimals, got: %s", got)
	}
}

func TestFormatConvertRate(t *testing.T) {
	tests := []struct {
		rate float64
		want string
	}{
		{1485.80, "1,485.80"},
		{215.93, "215.93"},
		{46.03, "46.03"},
		{5.65, "5.65"},
		{0.0565, "0.0565"},
	}
	for _, tt := range tests {
		got := formatConvertRate(tt.rate)
		if got != tt.want {
			t.Errorf("formatConvertRate(%f) = %s, want %s", tt.rate, got, tt.want)
		}
	}
}
