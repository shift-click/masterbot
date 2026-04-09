package scraper

import (
	"log/slog"
	"testing"

	"github.com/shift-click/masterbot/internal/scraper/providers"
)

func TestThemeIndexMatchPrecisionRules(t *testing.T) {
	t.Parallel()

	index := &ThemeIndex{
		naverEntries: []providers.ThemeEntry{
			{No: 10, Name: "네온가스"},
			{No: 11, Name: "2차전지(소재/부품)"},
			{No: 12, Name: "로봇AI"},
			{No: 13, Name: "로봇부품"},
		},
		naverReady: true,
		logger:     slog.Default(),
	}

	if got := index.Match("네온"); len(got) != 1 || got[0].No != 10 {
		t.Fatalf("unique prefix match = %+v, want 네온가스", got)
	}
	if got := index.Match("네"); len(got) != 0 {
		t.Fatalf("short keyword should not prefix match, got %+v", got)
	}
	if got := index.Match("로봇"); len(got) != 2 {
		t.Fatalf("ambiguous prefix should return multiple matches, got %+v", got)
	}
	if got := index.Match("전지"); len(got) != 0 {
		t.Fatalf("contains-only keyword should not match, got %+v", got)
	}
}
