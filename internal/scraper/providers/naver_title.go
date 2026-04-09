package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// NaverTitleResolver extracts product titles from Naver mobile search results.
// When a user searches for a Coupang product URL on Naver, the search result
// page includes the product's title as indexed by Naver. This bypasses
// Coupang's Akamai bot protection since we never contact Coupang directly.
type NaverTitleResolver struct {
	client *BreakerHTTPClient
	logger *slog.Logger
}

func NewNaverTitleResolver(logger *slog.Logger) *NaverTitleResolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &NaverTitleResolver{
		client: DefaultBreakerClient(10 * time.Second, "naver_title", logger),
		logger: logger.With("component", "naver_title"),
	}
}

// naverJSONTitleRe matches the main result title in Naver's embedded JSON.
// The "titleVariant" key immediately follows the result title, distinguishing
// it from other "title" fields like profile.title ("쿠팡").
var naverJSONTitleRe = regexp.MustCompile(`"title":"([^"]+)","titleVariant"`)

// naverHTMLSpanRe matches <span class="sds-comps-text">TITLE</span> in Naver's
// search result HTML structure.
var naverHTMLSpanRe = regexp.MustCompile(`<span[^>]*class="sds-comps-text"[^>]*>([^<]+)</span>`)

// ResolveTitle searches Naver mobile for the given Coupang product URL
// and extracts the product title from the search results.
func (r *NaverTitleResolver) ResolveTitle(ctx context.Context, productID string) (string, error) {
	if productID == "" {
		return "", fmt.Errorf("product ID is required")
	}

	query := "coupang.com/vp/products/" + productID
	searchURL := "https://m.search.naver.com/search.naver?query=" + url.QueryEscape(query)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return "", fmt.Errorf("create naver request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.0 Mobile/15E148 Safari/604.1")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ko-KR,ko;q=0.9")

	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch naver search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("naver search returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", fmt.Errorf("read naver response: %w", err)
	}

	html := string(body)
	title := extractProductTitle(html, productID)
	if title == "" {
		return "", fmt.Errorf("no product title found in naver results for %s", productID)
	}

	r.logger.Info("resolved coupang title via naver", "product_id", productID, "title", title)
	return title, nil
}

func extractProductTitle(page string, productID string) string {
	// Strategy 1: Extract from Naver's embedded JSON data.
	// Naver embeds structured JSON in the page where each search result has
	// "href" and "title" fields. We locate the Coupang result by its href
	// and read the nearby "title","titleVariant" pair.
	if title := extractTitleFromJSON(page, productID); title != "" {
		return title
	}

	// Strategy 2: Extract from HTML link structure.
	// Look for <a href="...coupang.com/vp/products/{ID}...">
	//            <span class="sds-comps-text">TITLE</span></a>
	if title := extractTitleFromHTML(page, productID); title != "" {
		return title
	}

	return ""
}

// extractTitleFromJSON finds the Coupang product title in Naver's embedded JSON.
// The JSON structure for each search result contains:
//
//	"href":"https://www.coupang.com/vp/products/{ID}",...,
//	"profile":{"title":"쿠팡"},...,
//	"title":"PRODUCT NAME","titleVariant":"..."
func extractTitleFromJSON(page string, productID string) string {
	marker := `"href":"https://www.coupang.com/vp/products/` + productID + `"`
	idx := strings.Index(page, marker)
	if idx == -1 {
		return ""
	}

	// The title field appears within ~2500 chars after the href in the JSON.
	end := min(idx+2500, len(page))
	window := page[idx:end]

	match := naverJSONTitleRe.FindStringSubmatch(window)
	if match == nil {
		return ""
	}

	// Decode JSON string escapes (\uXXXX, \/, etc.)
	title := match[1]
	var decoded string
	if err := json.Unmarshal([]byte(`"`+title+`"`), &decoded); err == nil {
		title = decoded
	}

	return cleanProductTitle(title)
}

// extractTitleFromHTML finds the product title from Naver's HTML structure.
// It looks for <a href="...coupang.com/vp/products/{ID}..."> followed by
// <span class="sds-comps-text">TITLE</span>.
func extractTitleFromHTML(page string, productID string) string {
	hrefMarker := `coupang.com/vp/products/` + productID
	startIdx := 0
	for {
		idx := strings.Index(page[startIdx:], hrefMarker)
		if idx == -1 {
			break
		}
		idx += startIdx

		// Only consider matches inside an href attribute.
		before := page[max(0, idx-100):idx]
		if !strings.Contains(before, `href="`) {
			startIdx = idx + len(hrefMarker)
			continue
		}

		// Look for <span class="sds-comps-text">TITLE</span> within 500 chars.
		end := min(idx+500, len(page))
		window := page[idx:end]

		match := naverHTMLSpanRe.FindStringSubmatch(window)
		if match != nil {
			title := strings.TrimSpace(match[1])
			if title != "" {
				return cleanProductTitle(title)
			}
		}
		startIdx = idx + len(hrefMarker)
	}
	return ""
}

// cleanProductTitle strips the Coupang category suffix (e.g., " - 기타간장")
// from a title like "홍영의 붉은대게 백간장, 500ml, 1개 - 기타간장".
func cleanProductTitle(title string) string {
	if dashIdx := strings.LastIndex(title, " - "); dashIdx > 0 {
		title = title[:dashIdx]
	}
	return strings.TrimSpace(title)
}


