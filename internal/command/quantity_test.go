package command

import (
	"testing"
)

func TestParseQuantifiedQuery(t *testing.T) {
	tests := []struct {
		input   string
		query   string
		qty     float64
		matched bool
	}{
		// With spaces
		{"솔 * 2", "솔", 2, true},
		{"삼전 * 10", "삼전", 10, true},
		{"비트 x 0.5", "비트", 0.5, true},
		{"비트 X 3", "비트", 3, true},
		{"비트 × 2", "비트", 2, true},

		// Without spaces
		{"솔*2", "솔", 2, true},
		{"삼전*10", "삼전", 10, true},

		// Mixed spaces
		{"솔 *2", "솔", 2, true},
		{"솔* 2", "솔", 2, true},

		// Decimal quantity
		{"비트*0.5", "비트", 0.5, true},
		{"삼전*1.5", "삼전", 1.5, true},

		// No quantity pattern
		{"솔", "솔", 1, false},
		{"삼전", "삼전", 1, false},
		{"비트코인", "비트코인", 1, false},

		// Empty/whitespace
		{"", "", 1, false},
		{"  ", "", 1, false},

		// Pure number (no query part — should not match because query part would be empty digits only)
		{"100*2", "100", 2, true}, // this is valid — calc handler decides separately

		// Multi-word query
		{"삼성전자 * 5", "삼성전자", 5, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			query, qty, ok := parseQuantifiedQuery(tt.input)
			if ok != tt.matched {
				t.Errorf("parseQuantifiedQuery(%q): matched=%v, want %v", tt.input, ok, tt.matched)
			}
			if ok && query != tt.query {
				t.Errorf("parseQuantifiedQuery(%q): query=%q, want %q", tt.input, query, tt.query)
			}
			if ok && qty != tt.qty {
				t.Errorf("parseQuantifiedQuery(%q): qty=%v, want %v", tt.input, qty, tt.qty)
			}
			if !ok && query != tt.query {
				t.Errorf("parseQuantifiedQuery(%q): fallback query=%q, want %q", tt.input, query, tt.query)
			}
		})
	}
}
