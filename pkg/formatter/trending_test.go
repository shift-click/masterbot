package formatter

import (
	"strings"
	"testing"
	"time"
)

func TestFormatTrending(t *testing.T) {
	items := []TrendItemData{
		{Rank: 1, Title: "붉은사막", Change: TrendChangeUp},
		{Rank: 2, Title: "챔피언스리그", Change: TrendChangeNone},
		{Rank: 3, Title: "웃음치료사", Change: TrendChangeDown},
		{Rank: 4, Title: "트럼프", Change: TrendChangeNew},
	}

	ts := time.Date(2026, 3, 19, 7, 43, 0, 0, time.FixedZone("KST", 9*60*60))
	result := FormatTrending(items, ts)

	if !strings.Contains(result, "🔍 실시간 검색 트렌드 Top4 (07:43)") {
		t.Errorf("missing header, got:\n%s", result)
	}
	if !strings.Contains(result, "1위 🔺 붉은사막") {
		t.Errorf("missing up indicator, got:\n%s", result)
	}
	if !strings.Contains(result, "2위 ➡️ 챔피언스리그") {
		t.Errorf("missing none indicator, got:\n%s", result)
	}
	if !strings.Contains(result, "3위 🔻 웃음치료사") {
		t.Errorf("missing down indicator, got:\n%s", result)
	}
	if !strings.Contains(result, "4위 🆕 트럼프") {
		t.Errorf("missing new indicator, got:\n%s", result)
	}
}

func TestFormatTrending_NoChange(t *testing.T) {
	items := []TrendItemData{
		{Rank: 1, Title: "테스트", Change: -1}, // unknown change
	}
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	result := FormatTrending(items, ts)

	// Unknown change should show no emoji, just padding.
	if !strings.Contains(result, "1위    테스트") {
		t.Errorf("unexpected format for unknown change, got:\n%s", result)
	}
}
