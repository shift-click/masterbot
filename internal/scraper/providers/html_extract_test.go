package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractTitle_FromTitleTag(t *testing.T) {
	t.Parallel()
	htmlDoc := `<html><head><title>My Page Title</title></head><body><p>` + longText + `</p></body></html>`
	title, body := parseAndExtract(t, htmlDoc)
	if title != "My Page Title" {
		t.Fatalf("expected title 'My Page Title', got %q", title)
	}
	if body == "" {
		t.Fatal("expected non-empty body")
	}
}

func TestExtractTitle_FromOGTitle(t *testing.T) {
	t.Parallel()
	htmlDoc := `<html><head><meta property="og:title" content="OG Title Here"></head><body><p>` + longText + `</p></body></html>`
	title, _ := parseAndExtract(t, htmlDoc)
	if title != "OG Title Here" {
		t.Fatalf("expected title 'OG Title Here', got %q", title)
	}
}

func TestExtractTitle_PrefersTitleOverOG(t *testing.T) {
	t.Parallel()
	const htmlDoc = `<html><head><title>Title Tag</title><meta property="og:title" content="OG Title"></head><body><p>text</p></body></html>`
	title, _ := parseAndExtract(t, htmlDoc)
	if title != "Title Tag" {
		t.Fatalf("expected 'Title Tag', got %q", title)
	}
}

func TestExtractBody_SemanticArticle(t *testing.T) {
	t.Parallel()
	htmlDoc := `<html><body><nav>menu items</nav><article><p>` + longText + `</p></article><footer>footer</footer></body></html>`
	_, body := parseAndExtract(t, htmlDoc)
	if !strings.Contains(body, "article content") {
		t.Fatalf("expected article content in body, got %q", body[:100])
	}
	if strings.Contains(body, "menu items") {
		t.Fatal("body should not contain nav content")
	}
	if strings.Contains(body, "footer") {
		t.Fatal("body should not contain footer content")
	}
}

func TestExtractBody_SemanticMain(t *testing.T) {
	t.Parallel()
	htmlDoc := `<html><body><aside>sidebar</aside><main><p>` + longText + `</p></main></body></html>`
	_, body := parseAndExtract(t, htmlDoc)
	if !strings.Contains(body, "article content") {
		t.Fatalf("expected main content in body, got %q", body[:100])
	}
	if strings.Contains(body, "sidebar") {
		t.Fatal("body should not contain aside content")
	}
}

func TestExtractBody_FallbackToBody(t *testing.T) {
	t.Parallel()
	htmlDoc := `<html><body><div>` + longText + `</div><script>var x = 1;</script><style>.a{}</style></body></html>`
	_, body := parseAndExtract(t, htmlDoc)
	if !strings.Contains(body, "article content") {
		t.Fatalf("expected div content in body, got %q", body[:100])
	}
	if strings.Contains(body, "var x") {
		t.Fatal("body should not contain script content")
	}
	if strings.Contains(body, ".a{}") {
		t.Fatal("body should not contain style content")
	}
}

func TestExtractBody_SkipsNavHeaderFooterAside(t *testing.T) {
	t.Parallel()
	htmlDoc := `<html><body>
		<header>header stuff</header>
		<nav>nav links</nav>
		<div><p>` + longText + `</p></div>
		<aside>side panel</aside>
		<footer>footer info</footer>
	</body></html>`
	_, body := parseAndExtract(t, htmlDoc)
	for _, skip := range []string{"header stuff", "nav links", "side panel", "footer info"} {
		if strings.Contains(body, skip) {
			t.Fatalf("body should not contain %q", skip)
		}
	}
}

func TestExtractBody_TooShort(t *testing.T) {
	t.Parallel()
	// "short" is 5 runes, well below minExtractRuneLen=50
	const htmlDoc = `<html><body><p>short</p></body></html>`
	_, body := parseAndExtract(t, htmlDoc)
	if body != "" {
		t.Fatalf("expected empty body for short content, got %q", body)
	}
}

func TestExtractBody_TooShort_Korean(t *testing.T) {
	t.Parallel()
	// 30 Korean chars = 90 bytes, exceeds old 100-byte threshold but below 50-rune threshold
	const htmlDoc = `<html><body><p>가나다라마바사아자차카타파하가나다라마바사아자차카타파하가나</p></body></html>`
	_, body := parseAndExtract(t, htmlDoc)
	if body != "" {
		t.Fatalf("expected empty body for text below rune threshold, got %q", body)
	}
}

func TestExtractBody_NaverSEContainer(t *testing.T) {
	t.Parallel()
	seContent := strings.Repeat("Naver blog body text for testing. ", 10)
	htmlDoc := `<html><body>
		<nav>nav menu items</nav>
		<div class="se-main-container">
			<div class="se-component se-text">
				<div class="se-module se-module-text">
					<p class="se-text-paragraph"><span>` + seContent + `</span></p>
				</div>
			</div>
		</div>
		<footer>footer info</footer>
	</body></html>`
	_, body := parseAndExtract(t, htmlDoc)
	if !strings.Contains(body, "Naver blog body text") {
		t.Fatalf("expected se-main-container content, got %q", body)
	}
	if strings.Contains(body, "nav menu items") {
		t.Fatal("body should not contain nav content when se-main-container is present")
	}
	if strings.Contains(body, "footer info") {
		t.Fatal("body should not contain footer content when se-main-container is present")
	}
}

func TestExtractBody_ArticlePreferredOverSEContainer(t *testing.T) {
	t.Parallel()
	articleContent := strings.Repeat("Article content for priority testing. ", 5)
	seContent := strings.Repeat("SE container content. ", 5)
	htmlDoc := `<html><body>
		<article><p>` + articleContent + `</p></article>
		<div class="se-main-container"><p>` + seContent + `</p></div>
	</body></html>`
	_, body := parseAndExtract(t, htmlDoc)
	if !strings.Contains(body, "Article content") {
		t.Fatal("expected article tag to take priority over se-main-container")
	}
}

func TestExtractBody_SEContainerWithMultipleClasses(t *testing.T) {
	t.Parallel()
	seContent := strings.Repeat("Multiple class container text. ", 5)
	htmlDoc := `<html><body>
		<div class="wrap se-main-container viewer">
			<p>` + seContent + `</p>
		</div>
	</body></html>`
	_, body := parseAndExtract(t, htmlDoc)
	if !strings.Contains(body, "Multiple class container") {
		t.Fatalf("expected content from element with multiple classes including se-main-container, got %q", body)
	}
}

func TestNormalizeNaverBlogURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "desktop blog.naver.com",
			url:  "https://blog.naver.com/fontoylab/224098503131",
			want: "https://m.blog.naver.com/fontoylab/224098503131",
		},
		{
			name: "already mobile",
			url:  "https://m.blog.naver.com/fontoylab/224098503131",
			want: "https://m.blog.naver.com/fontoylab/224098503131",
		},
		{
			name: "non-naver url unchanged",
			url:  "https://example.com/article",
			want: "https://example.com/article",
		},
		{
			name: "desktop with query params",
			url:  "https://blog.naver.com/PostView.naver?blogId=fontoylab&logNo=224098503131",
			want: "https://m.blog.naver.com/PostView.naver?blogId=fontoylab&logNo=224098503131",
		},
		{
			name: "desktop uppercase host",
			url:  "https://Blog.Naver.Com/test/123",
			want: "https://m.blog.naver.com/test/123",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeNaverBlogURL(tt.url)
			if got != tt.want {
				t.Errorf("normalizeNaverBlogURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestExtractBody_TruncatesLongText(t *testing.T) {
	t.Parallel()
	// Generate text longer than maxExtractTextLen
	long := strings.Repeat("가나다라마바사아 ", 2000)
	htmlDoc := `<html><body><article><p>` + long + `</p></article></body></html>`
	_, body := parseAndExtract(t, htmlDoc)
	if len(body) > maxExtractTextLen {
		t.Fatalf("body should be truncated to %d, got %d", maxExtractTextLen, len(body))
	}
}

func TestFetchAndExtractText_HTTPServer(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><head><title>Test Page</title></head><body><article><p>` + longText + `</p></article></body></html>`))
	}))
	t.Cleanup(srv.Close)

	title, body, err := FetchAndExtractText(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("FetchAndExtractText error: %v", err)
	}
	if title != "Test Page" {
		t.Fatalf("expected title 'Test Page', got %q", title)
	}
	if body == "" {
		t.Fatal("expected non-empty body")
	}
}

func TestFetchAndExtractText_HTTP404(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	_, _, err := FetchAndExtractText(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestFetchAndExtractText_InvalidURL(t *testing.T) {
	t.Parallel()
	_, _, err := FetchAndExtractText(context.Background(), "http://localhost:1/no-server")
	if err == nil {
		t.Fatal("expected error for unreachable URL")
	}
}

func TestExtractBody_ZeroWidthSpaceRemoved(t *testing.T) {
	t.Parallel()
	content := strings.Repeat("네이버 블로그 본문 텍스트 ", 10)
	htmlDoc := `<html><body><article><p>` + "\u200b" + content + "\u200b" + `</p></article></body></html>`
	_, body := parseAndExtract(t, htmlDoc)
	if strings.Contains(body, "\u200b") {
		t.Fatal("body should not contain zero-width space")
	}
	if !strings.Contains(body, "네이버 블로그 본문 텍스트") {
		t.Fatal("body should contain actual content")
	}
}

func TestExtractBody_ZeroWidthSpaceOnlyNode(t *testing.T) {
	t.Parallel()
	content := strings.Repeat("Real content here for testing extraction. ", 5)
	htmlDoc := `<html><body><article><p>` + "\u200b \u200b" + `</p><p>` + content + `</p></article></body></html>`
	_, body := parseAndExtract(t, htmlDoc)
	if strings.Contains(body, "\u200b") {
		t.Fatal("body should not contain zero-width space")
	}
	if !strings.Contains(body, "Real content") {
		t.Fatal("expected actual content in body")
	}
}

// longText provides >100 chars of article content for tests.
var longText = "This is article content that is repeated to ensure it exceeds the minimum text length. " +
	strings.Repeat("Article content continues with more meaningful text for extraction testing. ", 3)

// parseAndExtract is a test helper that parses HTML and extracts title+body.
func parseAndExtract(t *testing.T, htmlContent string) (title, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(htmlContent))
	}))
	t.Cleanup(srv.Close)

	title, body, err := FetchAndExtractText(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("FetchAndExtractText error: %v", err)
	}
	return title, body
}
