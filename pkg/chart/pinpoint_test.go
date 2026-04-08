package chart

import (
	"testing"
	"time"
)

func TestZigzagSwings_BasicUpDown(t *testing.T) {
	t.Parallel()
	candles := makeTestCandles([]float64{100, 110, 120, 105, 95, 110, 125, 115, 100})
	swings := zigzagSwings(candles, 10.0) // 10% deviation
	if len(swings) == 0 {
		t.Fatal("expected at least one swing point")
	}
	// Should detect swing highs and lows.
	var hasHigh, hasLow bool
	for _, s := range swings {
		if s.IsHigh {
			hasHigh = true
		} else {
			hasLow = true
		}
	}
	if !hasHigh {
		t.Error("expected at least one swing high")
	}
	if !hasLow {
		t.Error("expected at least one swing low")
	}
}

func TestZigzagSwings_NoSwingsInFlat(t *testing.T) {
	t.Parallel()
	// All prices within 1% range — no swing should be detected with 5% deviation.
	candles := makeTestCandles([]float64{100, 100.5, 99.5, 100.2, 99.8, 100.1})
	swings := zigzagSwings(candles, 5.0)
	if len(swings) != 0 {
		t.Errorf("expected no swings in flat market, got %d: %+v", len(swings), swings)
	}
}

func TestZigzagSwings_TooFewCandles(t *testing.T) {
	t.Parallel()
	candles := makeTestCandles([]float64{100})
	swings := zigzagSwings(candles, 5.0)
	if len(swings) != 0 {
		t.Errorf("expected no swings with 1 candle, got %d", len(swings))
	}
}

func TestRdpFilterToCount(t *testing.T) {
	t.Parallel()
	candles := makeTestCandles([]float64{100, 120, 90, 130, 80, 140, 85, 135, 95, 125})
	// Create artificial swings.
	swings := []Pinpoint{
		{Index: 1, Price: 120, IsHigh: true},
		{Index: 2, Price: 90, IsHigh: false},
		{Index: 3, Price: 130, IsHigh: true},
		{Index: 4, Price: 80, IsHigh: false},
		{Index: 5, Price: 140, IsHigh: true},
		{Index: 6, Price: 85, IsHigh: false},
		{Index: 7, Price: 135, IsHigh: true},
		{Index: 8, Price: 95, IsHigh: false},
	}

	filtered := rdpFilterToCount(swings, candles, 3)
	if len(filtered) > 3 {
		t.Errorf("expected at most 3 pinpoints, got %d", len(filtered))
	}
	if len(filtered) == 0 {
		t.Fatal("expected at least 1 pinpoint after RDP")
	}
}

func TestRdpFilterToCount_AlreadyUnderTarget(t *testing.T) {
	t.Parallel()
	swings := []Pinpoint{
		{Index: 1, Price: 120, IsHigh: true},
		{Index: 5, Price: 80, IsHigh: false},
	}
	filtered := rdpFilterToCount(swings, nil, 5)
	if len(filtered) != 2 {
		t.Errorf("expected 2 pinpoints (under target), got %d", len(filtered))
	}
}

func TestEnsureExtremes(t *testing.T) {
	t.Parallel()
	candles := makeTestCandles([]float64{100, 150, 80, 120, 90})
	// Global high is at index 1 (high = 152), global low is at index 2 (low = 78).
	// Pinpoints missing both extremes.
	pinpoints := []Pinpoint{
		{Index: 3, Price: 120, IsHigh: true},
	}
	result := ensureExtremes(pinpoints, candles)
	if len(result) < 2 {
		t.Fatalf("expected at least 2 pinpoints (original + extremes), got %d", len(result))
	}
	hasHigh, hasLow := false, false
	for _, p := range result {
		if p.Index == 1 {
			hasHigh = true
		}
		if p.Index == 2 {
			hasLow = true
		}
	}
	if !hasHigh {
		t.Error("expected global high at index 1 to be added")
	}
	if !hasLow {
		t.Error("expected global low at index 2 to be added")
	}
}

func TestDetectPinpoints_EndToEnd(t *testing.T) {
	t.Parallel()
	// Create a volatile price series.
	prices := []float64{
		100, 105, 115, 130, 125, 110, 95, 85,
		90, 100, 120, 140, 135, 120, 105, 95,
		100, 110, 125, 115, 105, 100,
	}
	candles := makeTestCandles(prices)
	pinpoints := DetectPinpoints(candles, "stock", "3개월")

	if len(pinpoints) == 0 {
		t.Fatal("expected pinpoints to be detected")
	}
	if len(pinpoints) > 5 {
		t.Errorf("expected at most 5 pinpoints for 3개월, got %d", len(pinpoints))
	}
}

func TestDetectPinpoints_TooFewCandles(t *testing.T) {
	t.Parallel()
	candles := makeTestCandles([]float64{100, 110})
	pinpoints := DetectPinpoints(candles, "coin", "1주")
	if pinpoints != nil {
		t.Errorf("expected nil pinpoints for 2 candles, got %d", len(pinpoints))
	}
}

func TestAdaptiveDeviation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		assetType string
		period    string
		wantMin   float64
		wantMax   float64
	}{
		{"coin", "1일", 3.0, 3.0},    // clamped to min 3%
		{"coin", "1주", 3.0, 3.0},    // ln(7)/ln(30)≈0.57 → 2.86 → clamp 3.0
		{"coin", "1개월", 4.9, 5.1},  // ln(30)/ln(30)=1.0 → 5.0
		{"coin", "1년", 8.0, 9.0},    // ln(365)/ln(30)≈1.73 → 8.68
		{"stock", "1일", 2.0, 2.0},   // clamped to min 2%
		{"stock", "3개월", 3.5, 4.5}, // ln(90)/ln(30)≈1.32 → 3.97
		{"stock", "1년", 5.0, 5.5},   // ln(365)/ln(30)≈1.73 → 5.21
	}
	for _, tt := range tests {
		dev := adaptiveDeviation(tt.assetType, tt.period)
		if dev < tt.wantMin || dev > tt.wantMax {
			t.Errorf("adaptiveDeviation(%s, %s) = %.2f, want [%.1f, %.1f]",
				tt.assetType, tt.period, dev, tt.wantMin, tt.wantMax)
		}
	}

	// Verify 6-month and 1-year produce distinct deviations.
	dev6m := adaptiveDeviation("coin", "6개월")
	dev1y := adaptiveDeviation("coin", "1년")
	if dev6m == dev1y {
		t.Errorf("6개월 and 1년 should have distinct deviations, both = %.2f", dev6m)
	}
	if dev6m >= dev1y {
		t.Errorf("6개월 (%.2f) should be less than 1년 (%.2f)", dev6m, dev1y)
	}
}

func TestMaxPinpointsForPeriod(t *testing.T) {
	t.Parallel()
	tests := []struct {
		period string
		want   int
	}{
		{"1일", 3},
		{"1주", 4},
		{"1개월", 4},
		{"3개월", 5},
		{"6개월", 5},
		{"1년", 5},
	}
	for _, tt := range tests {
		got := maxPinpointsForPeriod(tt.period)
		if got != tt.want {
			t.Errorf("maxPinpointsForPeriod(%s) = %d, want %d", tt.period, got, tt.want)
		}
	}
}

func TestCapPreservingExtremes(t *testing.T) {
	t.Parallel()
	// Global high is at index 1 (high=152), global low at index 4 (low=78).
	candles := makeTestCandles([]float64{100, 150, 120, 110, 80, 105, 130})
	pinpoints := []Pinpoint{
		{Index: 0, Price: 100, IsHigh: false},
		{Index: 1, Price: 152, IsHigh: true},  // global high
		{Index: 2, Price: 120, IsHigh: true},
		{Index: 4, Price: 78, IsHigh: false},   // global low
		{Index: 6, Price: 132, IsHigh: true},
	}
	result := capPreservingExtremes(pinpoints, candles, 3)
	if len(result) != 3 {
		t.Fatalf("expected 3 pinpoints, got %d", len(result))
	}
	hasGlobalHigh, hasGlobalLow := false, false
	for _, p := range result {
		if p.Index == 1 {
			hasGlobalHigh = true
		}
		if p.Index == 4 {
			hasGlobalLow = true
		}
	}
	if !hasGlobalHigh {
		t.Error("global high at index 1 should be preserved")
	}
	if !hasGlobalLow {
		t.Error("global low at index 4 should be preserved")
	}
}

func TestDynamicMarkerSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		spacing float64
		wantMin float64
		wantMax float64
	}{
		{2.0, 4.0, 4.0},    // very dense → clamp to min
		{10.0, 6.0, 6.0},   // moderate
		{50.0, 12.0, 12.0}, // very sparse → clamp to max
	}
	for _, tt := range tests {
		got := dynamicMarkerSize(tt.spacing)
		if got < tt.wantMin || got > tt.wantMax {
			t.Errorf("dynamicMarkerSize(%.0f) = %.1f, want [%.1f, %.1f]",
				tt.spacing, got, tt.wantMin, tt.wantMax)
		}
	}
}

// makeTestCandles creates candles from close prices with synthetic OHLC.
func makeTestCandles(closes []float64) []CandlePoint {
	candles := make([]CandlePoint, len(closes))
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, c := range closes {
		open := c
		if i > 0 {
			open = closes[i-1]
		}
		high := c
		low := c
		if open > high {
			high = open
		}
		if open < low {
			low = open
		}
		// Add some wick.
		high += 2
		low -= 2
		if low < 0 {
			low = 0
		}
		candles[i] = CandlePoint{
			Time:   base.AddDate(0, 0, i),
			Open:   open,
			High:   high,
			Low:    low,
			Close:  c,
			Volume: float64(1000 + i*100),
		}
	}
	return candles
}
