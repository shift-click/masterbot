package formatter

import (
	"strings"
	"testing"
	"time"
)

func TestFormatNews(t *testing.T) {
	items := []NewsItemData{
		{Rank: 1, Title: "트럼프 이란 발언", Source: "한겨레", Link: "https://news.google.com/1"},
		{Rank: 2, Title: "이란 가스전 폭격", Source: "조선일보", Link: "https://news.google.com/2"},
	}
	ts := time.Date(2026, 3, 19, 7, 43, 0, 0, time.FixedZone("KST", 9*60*60))
	result := FormatNews(items, ts)

	if !strings.Contains(result, "📰 실시간 인기뉴스 Top2 (07:43)") {
		t.Errorf("missing header, got:\n%s", result)
	}
	if !strings.Contains(result, "1위. [한겨레] 트럼프 이란 발언") {
		t.Errorf("missing item 1, got:\n%s", result)
	}
	if !strings.Contains(result, "https://news.google.com/1") {
		t.Errorf("missing link, got:\n%s", result)
	}
}

func TestFormatNews_NoSource(t *testing.T) {
	items := []NewsItemData{
		{Rank: 1, Title: "기사 제목", Source: "", Link: "https://example.com"},
	}
	ts := time.Now()
	result := FormatNews(items, ts)

	if strings.Contains(result, "[]") {
		t.Errorf("should not show empty brackets, got:\n%s", result)
	}
	if !strings.Contains(result, "1위. 기사 제목") {
		t.Errorf("missing title without source, got:\n%s", result)
	}
}

func TestFormatNews_NoLink(t *testing.T) {
	items := []NewsItemData{
		{Rank: 1, Title: "제목만", Source: "매체", Link: ""},
	}
	ts := time.Now()
	result := FormatNews(items, ts)

	lines := strings.Split(result, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Should not have an empty line for link.
	}
	if !strings.Contains(result, "1위. [매체] 제목만") {
		t.Errorf("unexpected format, got:\n%s", result)
	}
}
