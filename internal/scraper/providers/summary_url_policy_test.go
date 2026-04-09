package providers

import "testing"

func TestClassifyAutoSummaryURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want SummaryURLKind
	}{
		{name: "news whitelist", raw: "https://v.daum.net/v/20260401185027234", want: SummaryURLKindNews},
		{name: "news mobile whitelist", raw: "https://m.hani.co.kr/arti/economy/economy_general/1234567.html", want: SummaryURLKindNews},
		{name: "naver blog", raw: "https://m.blog.naver.com/fontoylab/224098503131", want: SummaryURLKindNaverBlog},
		{name: "x", raw: "https://x.com/openai/status/123", want: SummaryURLKindX},
		{name: "twitter", raw: "https://twitter.com/openai/status/123", want: SummaryURLKindX},
		{name: "youtube excluded", raw: "https://youtu.be/dQw4w9WgXcQ", want: SummaryURLKindNone},
		{name: "generic blog excluded", raw: "https://example.com/post/1", want: SummaryURLKindNone},
		{name: "invalid", raw: "not-a-url", want: SummaryURLKindNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ClassifyAutoSummaryURL(tt.raw); got != tt.want {
				t.Fatalf("ClassifyAutoSummaryURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestIsAutoSummaryURL(t *testing.T) {
	t.Parallel()

	if !IsAutoSummaryURL("https://news.naver.com/main/read.naver?oid=001&aid=0012345678") {
		t.Fatal("expected whitelisted news URL to be allowed")
	}
	if IsAutoSummaryURL("https://example.com/article") {
		t.Fatal("expected non-whitelisted URL to be ignored")
	}
}
