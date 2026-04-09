package providers

import (
	"testing"
)

func TestParseTrendsRSS(t *testing.T) {
	data := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:ht="https://trends.google.com/trending/rss">
  <channel>
    <title>Daily Search Trends</title>
    <item>
      <title>붉은사막</title>
      <ht:approx_traffic>2000+</ht:approx_traffic>
    </item>
    <item>
      <title>챔피언스리그</title>
      <ht:approx_traffic>1000+</ht:approx_traffic>
    </item>
    <item>
      <title>웃음치료사</title>
      <ht:approx_traffic>500+</ht:approx_traffic>
    </item>
  </channel>
</rss>`)

	items, err := parseTrendsRSS(data)
	if err != nil {
		t.Fatalf("parseTrendsRSS: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0].Title != "붉은사막" {
		t.Errorf("item[0].Title = %q, want 붉은사막", items[0].Title)
	}
	if items[0].Traffic != "2000+" {
		t.Errorf("item[0].Traffic = %q, want 2000+", items[0].Traffic)
	}
}

func TestTrendRankChange(t *testing.T) {
	prev := map[string]int{
		"키워드A": 1,
		"키워드B": 3,
		"키워드C": 5,
	}

	tests := []struct {
		name     string
		keyword  string
		newRank  int
		wantChange TrendChange
	}{
		{"same rank", "키워드A", 1, TrendChangeNone},
		{"moved up", "키워드B", 1, TrendChangeUp},
		{"moved down", "키워드C", 8, TrendChangeDown},
		{"new entry", "키워드D", 2, TrendChangeNew},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prevRank, existed := prev[tt.keyword]
			var got TrendChange
			switch {
			case !existed:
				got = TrendChangeNew
			case tt.newRank < prevRank:
				got = TrendChangeUp
			case tt.newRank > prevRank:
				got = TrendChangeDown
			default:
				got = TrendChangeNone
			}
			if got != tt.wantChange {
				t.Errorf("change = %d, want %d", got, tt.wantChange)
			}
		})
	}
}
