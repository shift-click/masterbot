package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// CoupangURL holds parsed identifiers from a Coupang product URL.
type CoupangURL struct {
	ProductID    string
	ItemID       string
	VendorItemID string
	OriginalURL  string
}

var (
	// Matches /vp/products/{id} or /vm/products/{id}
	reProductPath = regexp.MustCompile(`/v[pm]/products/(\d+)`)

	// Patterns to detect Coupang URLs in text.
	coupangURLPatterns = []string{
		"link.coupang.com",
		"coupang.com/vp/products/",
		"m.coupang.com/vm/products/",
	}

	// Full URL extraction regex for fallback handler.
	reCoupangURL = regexp.MustCompile(`https?://(?:link\.coupang\.com/\S+|(?:www\.|m\.)?coupang\.com/v[pm]/products/\d+\S*)`)
)

// ParseCoupangURL parses various forms of Coupang URLs and extracts product identifiers.
// Supports regular URLs (/vp/products/), mobile URLs (/vm/products/), and short URLs (link.coupang.com).
func ParseCoupangURL(ctx context.Context, rawURL string) (*CoupangURL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("empty URL")
	}

	// Ensure URL has a scheme.
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	// Short URL: resolve redirect.
	if parsed.Host == "link.coupang.com" {
		resolved, err := resolveShortURL(ctx, rawURL)
		if err != nil {
			return nil, fmt.Errorf("resolve short URL: %w", err)
		}
		parsed, err = url.Parse(resolved)
		if err != nil {
			return nil, fmt.Errorf("parse resolved URL: %w", err)
		}
	}

	// Validate domain.
	host := strings.ToLower(parsed.Host)
	if !strings.Contains(host, "coupang.com") {
		return nil, fmt.Errorf("not a Coupang URL: %s", host)
	}

	// Extract productId from path.
	matches := reProductPath.FindStringSubmatch(parsed.Path)
	if len(matches) < 2 {
		return nil, fmt.Errorf("no product ID found in path: %s", parsed.Path)
	}

	result := &CoupangURL{
		ProductID:    matches[1],
		ItemID:       parsed.Query().Get("itemId"),
		VendorItemID: parsed.Query().Get("vendorItemId"),
		OriginalURL:  rawURL,
	}

	return result, nil
}

// resolveShortURL follows a Coupang short URL redirect without downloading the body.
func resolveShortURL(ctx context.Context, shortURL string) (string, error) {
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: DefaultTransport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Don't follow redirects.
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, shortURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("no redirect location found (status %d)", resp.StatusCode)
	}

	return loc, nil
}

// IsCoupangURL checks whether the text contains a Coupang URL.
func IsCoupangURL(text string) bool {
	lower := strings.ToLower(text)
	for _, pattern := range coupangURLPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// ExtractCoupangURL extracts the first Coupang URL from text.
func ExtractCoupangURL(text string) string {
	return reCoupangURL.FindString(text)
}
