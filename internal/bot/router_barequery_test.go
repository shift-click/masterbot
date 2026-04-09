package bot

import "testing"

func TestIsJamoOnly(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Jamo-only — should be true
		{"ㅎㅇ", true},
		{"ㅈ", true},
		{"ㅋㅋㅋ", true},
		{"ㅎ ㅇ", true},
		{"ㅏㅏ", true},
		{"ㅎㅎㅎㅎㅎ", true},
		{"ㄷㄷ", true},
		{"ㅇㅎ", true},
		{"ㅋ", true},

		// Not jamo-only — should be false
		{"", false},
		{" ", false},
		{"삼전", false},
		{"환율", false},
		{"BTC", false},
		{"005930", false},
		{"ㅎ안", false},
		{"비트", false},
		{"hello", false},
		{"ㅋ2", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isJamoOnly(tt.input)
			if got != tt.want {
				t.Errorf("isJamoOnly(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestLooksLikeExplicitBareQuery(t *testing.T) {
	prefix := "/"
	tests := []struct {
		input string
		want  bool
	}{
		// Standard bare queries (existing behavior)
		{"비트", true},
		{"삼전", true},
		{"삼성전자", true},
		{"금", true},
		{"은", true},

		// Two-word queries
		{"금 한돈", true},
		{"삼전 10", true},

		// Quantity patterns — must pass
		{"솔*2", true},
		{"솔 * 2", true},
		{"삼전*10", true},
		{"삼전 * 10", true},
		{"비트x0.5", true},
		{"비트 × 2", true},

		// Should reject
		{"/도움", false},     // slash command
		{"", false},        // empty
		{"비트 어때?", false},  // question mark
		{"이건 세 단어", false}, // 3 fields without quantity
		{"https://example.com", false},
		{"한줄\n두줄", false},

		// Arithmetic — passes filter as 1 field, handled by calc deterministic fallback
		{"100*2", true},      // passes filter, won't match any handler, falls to calc
		{"(100+50)*2", true}, // same — 1 field, passes filter, calc handles it
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := looksLikeExplicitBareQuery(tt.input, prefix)
			if got != tt.want {
				t.Errorf("looksLikeExplicitBareQuery(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestContainsHTTPURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  bool
	}{
		{"삼전", false},
		{"http://example.com", true},
		{"prefix https://example.com suffix", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := containsHTTPURL(tt.input); got != tt.want {
				t.Fatalf("containsHTTPURL(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestLooksLikeThemeShapedBareQueryRejectsURL(t *testing.T) {
	t.Parallel()

	if looksLikeThemeShapedBareQuery("https://example.com 관련주", "/") {
		t.Fatal("theme-shaped bare query with URL should be rejected")
	}
}

func TestLooksLikeLocalAutoCandidateRejectsURL(t *testing.T) {
	t.Parallel()

	if looksLikeLocalAutoCandidate("삼전 https://example.com") {
		t.Fatal("local auto candidate with URL should be rejected")
	}
}
