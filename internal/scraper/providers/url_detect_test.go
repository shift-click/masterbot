package providers

import "testing"

func TestIsWebURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"https URL", "check https://example.com/article", true},
		{"http URL", "http://blog.example.com", true},
		{"no URL", "just plain text", false},
		{"empty", "", false},
		{"partial", "httpsomething", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsWebURL(tt.text); got != tt.want {
				t.Errorf("IsWebURL(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestExtractWebURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want string
	}{
		{"simple URL", "see https://example.com/article here", "https://example.com/article"},
		{"URL with query", "go to https://news.site.com/a?id=123&cat=tech now", "https://news.site.com/a?id=123&cat=tech"},
		{"http URL", "http://blog.example.com/post/1", "http://blog.example.com/post/1"},
		{"no URL", "no links here", ""},
		{"empty", "", ""},
		{"multiple URLs picks first", "https://first.com and https://second.com", "https://first.com"},
		{"URL with path", "https://n.news.naver.com/mnews/article/032/0003345678", "https://n.news.naver.com/mnews/article/032/0003345678"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ExtractWebURL(tt.text); got != tt.want {
				t.Errorf("ExtractWebURL(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}
