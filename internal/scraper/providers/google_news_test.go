package providers

import (
	"testing"
)

func TestParseNewsRSS(t *testing.T) {
	data := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>주요 뉴스 - Google 뉴스</title>
    <item>
      <title>트럼프, 이란에 강경 발언</title>
      <link>https://news.google.com/rss/articles/CBMiAA</link>
      <source>한겨레</source>
    </item>
    <item>
      <title>이란 가스전 폭격 여파</title>
      <link>https://news.google.com/rss/articles/CBMiBB</link>
      <source>조선일보</source>
    </item>
    <item>
      <title>원유 수급 비상</title>
      <link>https://news.google.com/rss/articles/CBMiCC</link>
      <source>한국경제</source>
    </item>
  </channel>
</rss>`)

	items, err := parseNewsRSS(data)
	if err != nil {
		t.Fatalf("parseNewsRSS: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0].Title != "트럼프, 이란에 강경 발언" {
		t.Errorf("item[0].Title = %q", items[0].Title)
	}
	if items[0].Source != "한겨레" {
		t.Errorf("item[0].Source = %q, want 한겨레", items[0].Source)
	}
	if items[1].Link != "https://news.google.com/rss/articles/CBMiBB" {
		t.Errorf("item[1].Link = %q", items[1].Link)
	}
}

func TestParseNewsRSS_NoSource(t *testing.T) {
	data := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <item>
      <title>기사 제목</title>
      <link>https://example.com</link>
    </item>
  </channel>
</rss>`)

	items, err := parseNewsRSS(data)
	if err != nil {
		t.Fatalf("parseNewsRSS: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Source != "" {
		t.Errorf("item[0].Source = %q, want empty", items[0].Source)
	}
}
