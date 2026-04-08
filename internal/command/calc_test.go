package command

import (
	"math"
	"testing"
)

func TestEvalExpr(t *testing.T) {
	tests := []struct {
		input string
		want  float64
		err   bool
	}{
		// Basic operations
		{"100*2", 200, false},
		{"100+50", 150, false},
		{"100-30", 70, false},
		{"100/4", 25, false},
		{"10%3", 1, false},

		// Parentheses
		{"(100+50)*2", 300, false},
		{"((2+3)*4)+1", 21, false},
		{"(10)", 10, false},

		// Operator precedence
		{"2+3*4", 14, false},
		{"10-2*3", 4, false},

		// Exponentiation
		{"2^10", 1024, false},
		{"3^2", 9, false},
		{"2^3^2", 512, false}, // right-associative: 2^(3^2) = 2^9 = 512

		// Decimals
		{"3.14*2", 6.28, false},
		{"0.1+0.2", 0.3, false}, // floating point; close enough

		// Unary minus
		{"-5+3", -2, false},
		{"-(2+3)", -5, false},
		{"-5*-3", 15, false},

		// Whitespace
		{"100 * 2", 200, false},
		{" 1 + 2 ", 3, false},

		// Division by zero
		{"1/0", 0, true},
		{"10%0", 0, true},

		// Complex
		{"(1+2)*(3+4)", 21, false},
		{"2^(1+2)", 8, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := evalExpr(tt.input)
			if tt.err {
				if err == nil {
					t.Errorf("evalExpr(%q) expected error, got %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("evalExpr(%q) unexpected error: %v", tt.input, err)
				return
			}
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("evalExpr(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCalcTriggerConditions(t *testing.T) {
	tests := []struct {
		input   string
		trigger bool
	}{
		// Should trigger
		{"100*2", true},
		{"(100+50)*2", true},
		{"2^10", true},
		{"100 * 2", true},
		{"3.14*2", true},
		{"-5+3", true},
		{"10%3", true},

		// Should NOT trigger — contains letters
		{"솔*2", false},
		{"abc*2", false},
		{"100만원짜리", false},

		// Should NOT trigger — no operator
		{"42", false},
		{"3.14", false},

		// Should NOT trigger — stock code (6 digits)
		{"005930", false},

		// Should NOT trigger — empty
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			triggered := calcShouldTrigger(tt.input)
			if triggered != tt.trigger {
				t.Errorf("calcShouldTrigger(%q) = %v, want %v", tt.input, triggered, tt.trigger)
			}
		})
	}
}

// calcShouldTrigger mirrors the trigger logic from CalcHandler.HandleFallback.
func calcShouldTrigger(msg string) bool {
	if msg == "" {
		return false
	}
	for _, r := range msg {
		if 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' {
			return false
		}
		if r >= 0xAC00 && r <= 0xD7AF { // Hangul syllables
			return false
		}
		if r >= 0x3131 && r <= 0x318E { // Hangul compatibility jamo
			return false
		}
	}
	if stockCodePattern.MatchString(msg) {
		return false
	}
	if !calcExprPattern.MatchString(msg) {
		return false
	}
	if !containsOperator(msg) {
		return false
	}
	return true
}

func containsOperator(s string) bool {
	for _, r := range s {
		switch r {
		case '+', '-', '*', '/', '%', '^':
			return true
		}
	}
	return false
}
