package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	maxExtractBodyBytes = 2 * 1024 * 1024 // 2 MB
	maxExtractTextLen   = 12_000
	minExtractRuneLen   = 50 // minimum rune (character) count, not bytes
	fetchTimeout        = 15 * time.Second
)

// FetchAndExtractText fetches a URL and extracts readable text content.
// Returns empty body (and no error) when the extracted text is shorter than minExtractRuneLen.
func FetchAndExtractText(ctx context.Context, rawURL string) (title, body string, err error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	rawURL = normalizeNaverBlogURL(rawURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("html extract: create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ko-KR,ko;q=0.9,en-US;q=0.8,en;q=0.7")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("html extract: fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("html extract: http %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, maxExtractBodyBytes)
	doc, err := html.Parse(limited)
	if err != nil {
		return "", "", fmt.Errorf("html extract: parse: %w", err)
	}

	title = extractTitle(doc)
	body = extractBody(doc)

	if utf8.RuneCountInString(body) < minExtractRuneLen {
		return title, "", nil
	}
	if len(body) > maxExtractTextLen {
		body = body[:maxExtractTextLen]
	}
	return title, body, nil
}

// normalizeNaverBlogURL rewrites desktop Naver blog URLs to the mobile variant
// so that FetchAndExtractText always receives SSR HTML instead of a JS redirect.
func normalizeNaverBlogURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	host := strings.ToLower(u.Hostname())
	if host == "blog.naver.com" {
		u.Host = "m.blog.naver.com"
		return u.String()
	}
	return rawURL
}

// extractTitle extracts the page title from <title> or <meta property="og:title">.
func extractTitle(doc *html.Node) string {
	var title, ogTitle string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if foundTitle, foundOGTitle := extractNodeTitles(n); title == "" && foundTitle != "" {
			title = foundTitle
		} else if ogTitle == "" && foundOGTitle != "" {
			ogTitle = foundOGTitle
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	if title != "" {
		return strings.TrimSpace(title)
	}
	return strings.TrimSpace(ogTitle)
}

func extractNodeTitles(n *html.Node) (string, string) {
	if n.Type != html.ElementNode {
		return "", ""
	}
	if n.DataAtom == atom.Title {
		return textContent(n), ""
	}
	if n.DataAtom != atom.Meta {
		return "", ""
	}
	prop, content := metaAttributePair(n)
	if prop == "og:title" {
		return "", content
	}
	return "", ""
}

func metaAttributePair(n *html.Node) (string, string) {
	var prop, content string
	for _, attr := range n.Attr {
		switch attr.Key {
		case "property":
			prop = attr.Val
		case "content":
			content = attr.Val
		}
	}
	return prop, content
}

var skipAtoms = map[atom.Atom]bool{
	atom.Script:   true,
	atom.Style:    true,
	atom.Nav:      true,
	atom.Header:   true,
	atom.Footer:   true,
	atom.Aside:    true,
	atom.Noscript: true,
}

var contentAtoms = map[atom.Atom]bool{
	atom.Article: true,
	atom.Main:    true,
}

// contentClasses are CSS class names for known content containers, checked
// when no semantic <article>/<main> element is found. Order matters.
var contentClasses = []string{
	"se-main-container", // Naver SmartEditor4
}

// extractBody extracts readable text from the HTML document.
// Prefers <article>/<main>, then known content-class containers, then <body>.
func extractBody(doc *html.Node) string {
	// First pass: look for semantic content elements.
	if node := findFirst(doc, contentAtoms); node != nil {
		return collectText(node)
	}

	// Second pass: look for known content-class containers.
	for _, cls := range contentClasses {
		if node := findFirstByClass(doc, cls); node != nil {
			return collectText(node)
		}
	}

	// Fallback: collect from <body> with skip-tags filtered.
	if body := findFirstAtom(doc, atom.Body); body != nil {
		return collectText(body)
	}

	// Last resort: entire document.
	return collectText(doc)
}

func findFirst(n *html.Node, atoms map[atom.Atom]bool) *html.Node {
	if n.Type == html.ElementNode && atoms[n.DataAtom] {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findFirst(c, atoms); found != nil {
			return found
		}
	}
	return nil
}

// findFirstByClass returns the first element whose class attribute contains cls.
func findFirstByClass(n *html.Node, cls string) *html.Node {
	if n.Type == html.ElementNode {
		for _, a := range n.Attr {
			if a.Key == "class" && containsClass(a.Val, cls) {
				return n
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findFirstByClass(c, cls); found != nil {
			return found
		}
	}
	return nil
}

// containsClass checks whether a space-separated class list contains the given class name.
func containsClass(classList, cls string) bool {
	for _, c := range strings.Fields(classList) {
		if c == cls {
			return true
		}
	}
	return false
}

func findFirstAtom(n *html.Node, a atom.Atom) *html.Node {
	if n.Type == html.ElementNode && n.DataAtom == a {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findFirstAtom(c, a); found != nil {
			return found
		}
	}
	return nil
}

func collectText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && skipAtoms[n.DataAtom] {
			return
		}
		if n.Type == html.TextNode {
			text := strings.ReplaceAll(n.Data, "\u200b", "")
			text = strings.TrimSpace(text)
			if text != "" {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(text)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}
