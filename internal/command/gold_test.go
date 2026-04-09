package command

import (
	"testing"
)

func TestParseGoldQuery(t *testing.T) {
	tests := []struct {
		input   string
		metal   string
		qty     float64
		unit    string
		matched bool
	}{
		// Basic aliases
		{"금", "gold", 1, "돈", true},
		{"골드", "gold", 1, "돈", true},
		{"gold", "gold", 1, "돈", true},
		{"금값", "gold", 1, "돈", true},
		{"금시세", "gold", 1, "돈", true},
		{"은", "silver", 1, "g", true},
		{"실버", "silver", 1, "g", true},
		{"silver", "silver", 1, "g", true},
		{"은값", "silver", 1, "g", true},

		// Unit patterns
		{"금 한돈", "gold", 1, "돈", true},
		{"금 두돈", "gold", 2, "돈", true},
		{"금 2돈", "gold", 2, "돈", true},
		{"금 10g", "gold", 10, "g", true},
		{"금 10그램", "gold", 10, "g", true},
		{"금 1oz", "gold", 1, "oz", true},
		{"금 1온스", "gold", 1, "oz", true},
		{"은 한돈", "silver", 1, "돈", true},
		{"은 5g", "silver", 5, "g", true},

		// Quantity multiplier
		{"금 * 2", "gold", 2, "돈", true},
		{"은 * 3", "silver", 3, "g", true},

		// Theme exclusions — should NOT match
		{"금 테마", "", 0, "", false},
		{"금 관련주", "", 0, "", false},
		{"금 관련", "", 0, "", false},
		{"금 주식", "", 0, "", false},
		{"금 종목", "", 0, "", false},

		// Non-gold input
		{"비트", "", 0, "", false},
		{"삼전", "", 0, "", false},
		{"", "", 0, "", false},

		// Too many words (4+)
		{"금 한돈 얼마야 지금", "", 0, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			q, ok := parseGoldQuery(tt.input)
			if ok != tt.matched {
				t.Errorf("parseGoldQuery(%q): matched=%v, want %v", tt.input, ok, tt.matched)
				return
			}
			if !ok {
				return
			}
			if q.metal != tt.metal {
				t.Errorf("parseGoldQuery(%q): metal=%q, want %q", tt.input, q.metal, tt.metal)
			}
			if q.qty != tt.qty {
				t.Errorf("parseGoldQuery(%q): qty=%v, want %v", tt.input, q.qty, tt.qty)
			}
			if q.unit != tt.unit {
				t.Errorf("parseGoldQuery(%q): unit=%q, want %q", tt.input, q.unit, tt.unit)
			}
		})
	}
}

func TestUnitToGrams(t *testing.T) {
	tests := []struct {
		qty  float64
		unit string
		want float64
	}{
		{1, "돈", 3.75},
		{2, "돈", 7.50},
		{10, "g", 10},
		{1, "oz", 31.1035},
	}

	for _, tt := range tests {
		got := unitToGrams(tt.qty, tt.unit)
		diff := got - tt.want
		if diff < 0 {
			diff = -diff
		}
		if diff > 0.001 {
			t.Errorf("unitToGrams(%v, %q) = %v, want %v", tt.qty, tt.unit, got, tt.want)
		}
	}
}
