package providers

import "testing"

func TestParseTimeframe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  Timeframe
		ok    bool
	}{
		{"1일", Timeframe1D, true},
		{"1d", Timeframe1D, true},
		{"1D", Timeframe1D, true},
		{"1주", Timeframe1W, true},
		{"1w", Timeframe1W, true},
		{"1달", Timeframe1M, true},
		{"1개월", Timeframe1M, true},
		{"1m", Timeframe1M, true},
		{"3달", Timeframe3M, true},
		{"3개월", Timeframe3M, true},
		{"6달", Timeframe6M, true},
		{"6개월", Timeframe6M, true},
		{"1년", Timeframe1Y, true},
		{"1y", Timeframe1Y, true},
		{"5년", "", false},
		{"abc", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got, ok := ParseTimeframe(tt.input)
			if ok != tt.ok {
				t.Fatalf("ParseTimeframe(%q) ok = %v, want %v", tt.input, ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("ParseTimeframe(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTimeframeDays(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tf   Timeframe
		want int
	}{
		{Timeframe1D, 5},
		{Timeframe1W, 7},
		{Timeframe1M, 30},
		{Timeframe3M, 90},
		{Timeframe6M, 180},
		{Timeframe1Y, 365},
	}

	for _, tt := range tests {
		t.Run(string(tt.tf), func(t *testing.T) {
			t.Parallel()
			got := TimeframeDays(tt.tf)
			if got != tt.want {
				t.Fatalf("TimeframeDays(%q) = %d, want %d", tt.tf, got, tt.want)
			}
		})
	}
}

func TestTimeframeLabel(t *testing.T) {
	t.Parallel()

	if got := TimeframeLabel(Timeframe3M); got != "3개월" {
		t.Fatalf("TimeframeLabel(3M) = %q, want %q", got, "3개월")
	}
}
