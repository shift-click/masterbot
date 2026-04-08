package providers

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type FallcentCandidate struct {
	ProductID string
	Title     string
	Path      string
}

type FallcentProductData struct {
	FallcentProductID string
	ProductID         string
	ItemID            string
	VendorItemID      string
	Name              string
	Price             int
	ImageURL          string
	LowestPrice       int
	SearchKeyword     string
}

type FallcentResolver struct {
	baseURL       string
	client *BreakerHTTPClient
	logger        *slog.Logger
	maxCandidates int
}

const (
	fallcentHeaderUserAgent  = "User-Agent"
	fallcentBrowserUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

func NewFallcentResolver(logger *slog.Logger, maxCandidates int) *FallcentResolver {
	if logger == nil {
		logger = slog.Default()
	}
	if maxCandidates <= 0 {
		maxCandidates = 3
	}
	return &FallcentResolver{
		baseURL: "https://fallcent.com",
		client: DefaultBreakerClient(15 * time.Second, "fallcent", logger),
		logger:        logger.With("component", "fallcent"),
		maxCandidates: maxCandidates,
	}
}

func (r *FallcentResolver) SearchCandidates(ctx context.Context, keyword string) ([]FallcentCandidate, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("search keyword is required")
	}

	searchURL := fmt.Sprintf("%s/product/search/?keyword=%s", r.baseURL, url.QueryEscape(keyword))
	body, err := r.fetch(ctx, searchURL)
	if err != nil {
		return nil, err
	}

	candidates := parseFallcentSearchResults(body)
	if len(candidates) == 0 {
		return nil, nil
	}
	if len(candidates) > r.maxCandidates {
		candidates = candidates[:r.maxCandidates]
	}
	return candidates, nil
}

func (r *FallcentResolver) FetchProduct(ctx context.Context, fallcentProductID string) (*FallcentProductData, error) {
	fallcentProductID = strings.TrimSpace(fallcentProductID)
	if fallcentProductID == "" {
		return nil, fmt.Errorf("fallcent product id is required")
	}

	body, err := r.fetch(ctx, fmt.Sprintf("%s/product/%s/", r.baseURL, fallcentProductID))
	if err != nil {
		return nil, err
	}

	data, err := parseFallcentDetail(body)
	if err != nil {
		return nil, err
	}
	data.FallcentProductID = fallcentProductID
	return data, nil
}

// LookupByCoupangID resolves a Fallcent product directly using the Coupang
// product ID and item ID. Fallcent exposes the undocumented endpoint
// /product/?product_id={productId}&item_id={itemId} which returns a 301
// redirect to the canonical Fallcent product page when the product is tracked.
// This bypasses the keyword-based search entirely, avoiding the need to
// scrape or guess the product name.
func (r *FallcentResolver) LookupByCoupangID(ctx context.Context, productID, itemID string) (*FallcentProductData, error) {
	productID = strings.TrimSpace(productID)
	itemID = strings.TrimSpace(itemID)
	if productID == "" || itemID == "" {
		return nil, fmt.Errorf("both product_id and item_id are required for direct lookup")
	}

	lookupURL := fmt.Sprintf("%s/product/?product_id=%s&item_id=%s",
		r.baseURL,
		url.QueryEscape(productID),
		url.QueryEscape(itemID))

	fallcentID, err := r.resolveFallcentRedirect(ctx, lookupURL)
	if err != nil {
		return nil, err
	}

	data, err := r.FetchProduct(ctx, fallcentID)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// reFallcentProductPath extracts the Fallcent product ID from a redirect
// Location header like "/product/Zz0jML6cu2XPea9AnCMkF7x3DfRVTQUz/".
var reFallcentProductPath = regexp.MustCompile(`^/product/([A-Za-z0-9]+)/$`)

// resolveFallcentRedirect performs a GET without following redirects.
// On a 301 whose Location matches /product/{id}/, it returns the Fallcent
// product ID. Any other status (including 404) is treated as "not found".
func (r *FallcentResolver) resolveFallcentRedirect(ctx context.Context, targetURL string) (string, error) {
	noRedirect := &http.Client{
		Timeout:   r.client.Unwrap().Timeout,
		Transport: DefaultTransport(),
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", fmt.Errorf("create fallcent lookup request: %w", err)
	}
	req.Header.Set(fallcentHeaderUserAgent, fallcentBrowserUserAgent)

	resp, err := noRedirect.Do(req)
	if err != nil {
		return "", fmt.Errorf("fallcent lookup request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("product not tracked on fallcent")
	}
	if resp.StatusCode != http.StatusMovedPermanently {
		return "", fmt.Errorf("fallcent lookup returned unexpected HTTP %d", resp.StatusCode)
	}

	loc := resp.Header.Get("Location")
	match := reFallcentProductPath.FindStringSubmatch(loc)
	if len(match) < 2 {
		return "", fmt.Errorf("fallcent redirect location %q does not match expected pattern", loc)
	}
	return match[1], nil
}

func (r *FallcentResolver) ResolveProduct(ctx context.Context, cu *CoupangURL, keywords []string) (*FallcentProductData, error) {
	if cu == nil {
		return nil, fmt.Errorf("coupang url is required")
	}

	// Try direct lookup first when item ID is available.
	if cu.ItemID != "" {
		data, err := r.LookupByCoupangID(ctx, cu.ProductID, cu.ItemID)
		if err == nil && data != nil && FallcentMatchesCoupang(cu, data) {
			r.logger.Info("resolved product via fallcent direct lookup",
				"product_id", cu.ProductID, "item_id", cu.ItemID,
				"fallcent_id", data.FallcentProductID)
			return data, nil
		}
		if err != nil {
			r.logger.Debug("fallcent direct lookup failed, falling back to keyword search",
				"product_id", cu.ProductID, "item_id", cu.ItemID, "error", err)
		}
	}

	var lastErr error
	for _, keyword := range uniqueFallcentKeywords(keywords) {
		product, err := r.resolveProductByKeyword(ctx, cu, keyword)
		if err != nil {
			lastErr = err
			continue
		}
		return product, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no usable fallcent keyword")
	}
	return nil, lastErr
}

func uniqueFallcentKeywords(keywords []string) []string {
	seen := make(map[string]struct{}, len(keywords))
	unique := make([]string, 0, len(keywords))
	for _, keyword := range keywords {
		normalized := strings.TrimSpace(keyword)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		unique = append(unique, normalized)
	}
	return unique
}

func (r *FallcentResolver) resolveProductByKeyword(ctx context.Context, cu *CoupangURL, keyword string) (*FallcentProductData, error) {
	candidates, err := r.SearchCandidates(ctx, keyword)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no fallcent candidates for %q", keyword)
	}

	// Fetch all candidates in parallel for speed.
	type fetchResult struct {
		product *FallcentProductData
		err     error
		idx     int
	}
	results := make([]fetchResult, len(candidates))
	var wg sync.WaitGroup
	for i, candidate := range candidates {
		wg.Add(1)
		go func(i int, fallcentID string) {
			defer wg.Done()
			product, fetchErr := r.FetchProduct(ctx, fallcentID)
			results[i] = fetchResult{product: product, err: fetchErr, idx: i}
		}(i, candidate.ProductID)
	}
	wg.Wait()

	// Pick best match: prefer exact option match over productID-only match.
	var productIDMatch *FallcentProductData
	for i, res := range results {
		if res.err != nil {
			r.logger.Debug("fallcent candidate fetch failed", "fallcent_id", candidates[i].ProductID, "error", res.err)
			err = res.err
			continue
		}
		res.product.SearchKeyword = keyword
		if res.product.ProductID == "" || res.product.ProductID != cu.ProductID {
			err = fmt.Errorf("verification mismatch for candidate %s", candidates[i].ProductID)
			continue
		}
		if fallcentOptionMatches(cu, res.product) {
			return res.product, nil
		}
		if productIDMatch == nil {
			productIDMatch = res.product
		}
	}
	if productIDMatch != nil {
		return productIDMatch, nil
	}
	return nil, err
}

func (r *FallcentResolver) fetch(ctx context.Context, targetURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", fmt.Errorf("create fallcent request: %w", err)
	}
	req.Header.Set(fallcentHeaderUserAgent, fallcentBrowserUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ko-KR,ko;q=0.9")

	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch fallcent page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fallcent returned HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read fallcent body: %w", err)
	}
	return string(data), nil
}

// FallcentMatchesCoupang returns true when the Fallcent product refers to the
// same Coupang product. Matches on productID only — different option variants
// (e.g. "4개" vs "6개") share the same productID but have distinct item IDs.
func FallcentMatchesCoupang(cu *CoupangURL, product *FallcentProductData) bool {
	if cu == nil || product == nil {
		return false
	}
	return product.ProductID != "" && product.ProductID == cu.ProductID
}

// fallcentOptionMatches returns true when the Fallcent product matches the
// exact option variant (same itemID and vendorItemID).
func fallcentOptionMatches(cu *CoupangURL, product *FallcentProductData) bool {
	if cu.ItemID != "" && product.ItemID != "" && cu.ItemID != product.ItemID {
		return false
	}
	if cu.VendorItemID != "" && product.VendorItemID != "" && cu.VendorItemID != product.VendorItemID {
		return false
	}
	return true
}

var (
	reFallcentSearchCard = regexp.MustCompile(`<a href="(/product/([A-Za-z0-9]+)/)" class="block"[\s\S]*?<p class="mt-1 text-sm leading-snug line-clamp-2">([\s\S]*?)</p>`)
	reFallcentMetaPrice  = regexp.MustCompile(`<meta property="product:price:amount" content="([0-9,]+)">`)
	reFallcentOgImage    = regexp.MustCompile(`<meta property="og:image" content="([^"]+)"`)
	reFallcentHeading    = regexp.MustCompile(`<h1[^>]*>([\s\S]*?)</h1>`)
	reFallcentLowest     = regexp.MustCompile(`역대 최저가</span>\s*<span[^>]*>([0-9,]+)원</span>`)
	reFallcentPurchase   = regexp.MustCompile(`https://[^"]*(?:link\.coupang\.com|rco\.mjbiz\.co\.kr/redirect\?url=https%3A%2F%2Flink\.coupang\.com|api\.adjoin\.co\.kr/cou/land\.php\?[^"]*link\.coupang\.com)[^"]+`)
)

func parseFallcentSearchResults(body string) []FallcentCandidate {
	var candidates []FallcentCandidate
	seen := make(map[string]struct{})
	for _, match := range reFallcentSearchCard.FindAllStringSubmatch(body, -1) {
		if len(match) < 4 {
			continue
		}
		productID := strings.TrimSpace(match[2])
		if productID == "" {
			continue
		}
		if _, exists := seen[productID]; exists {
			continue
		}
		seen[productID] = struct{}{}
		title := cleanFallcentText(match[3])
		candidates = append(candidates, FallcentCandidate{
			ProductID: productID,
			Title:     title,
			Path:      match[1],
		})
	}
	return candidates
}

func parseFallcentDetail(body string) (*FallcentProductData, error) {
	price := parseFallcentPrice(reFallcentMetaPrice.FindStringSubmatch(body))
	name := cleanFallcentText(firstSubmatch(reFallcentHeading, body))
	imageURL := html.UnescapeString(strings.TrimSpace(firstSubmatch(reFallcentOgImage, body)))
	lowest := parseFallcentPrice(reFallcentLowest.FindStringSubmatch(body))

	purchaseRaw := reFallcentPurchase.FindString(body)
	purchase := html.UnescapeString(strings.TrimSpace(purchaseRaw))
	purchase = decodeFallcentRedirect(purchase)

	productID := queryOrPathValue(purchase, "pageKey")
	// Debug: log purchase link parsing for diagnosis
	if productID == "" && purchaseRaw != "" {
		slog.Debug("fallcent purchase link parse debug", "raw_len", len(purchaseRaw), "raw_prefix", purchaseRaw[:min(len(purchaseRaw), 120)], "decoded_prefix", purchase[:min(len(purchase), 120)])
	}
	itemID := queryOrPathValue(purchase, "itemId")
	vendorItemID := queryOrPathValue(purchase, "vendorItemId")

	if price <= 0 {
		return nil, fmt.Errorf("fallcent current price not found")
	}
	if name == "" {
		return nil, fmt.Errorf("fallcent product name not found")
	}
	if productID == "" {
		return nil, fmt.Errorf("fallcent purchase link is missing pageKey")
	}

	return &FallcentProductData{
		ProductID:    productID,
		ItemID:       itemID,
		VendorItemID: vendorItemID,
		Name:         name,
		Price:        price,
		ImageURL:     imageURL,
		LowestPrice:  lowest,
	}, nil
}

func cleanFallcentText(value string) string {
	value = html.UnescapeString(value)
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\t", " ")
	value = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(value, "")
	return strings.Join(strings.Fields(value), " ")
}

func firstSubmatch(re *regexp.Regexp, body string) string {
	match := re.FindStringSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func parseFallcentPrice(match []string) int {
	if len(match) < 2 {
		return 0
	}
	price, _ := strconv.Atoi(strings.ReplaceAll(match[1], ",", ""))
	return price
}

func decodeFallcentRedirect(raw string) string {
	// Handle api.adjoin.co.kr wrapper first, before any global URL decoding.
	// The "land" query parameter contains a percent-encoded coupang URL;
	// decoding the entire URL first would merge the inner query parameters
	// with the outer ones, breaking url.Parse().Query().Get("land").
	if strings.Contains(raw, "adjoin.co.kr") {
		if parsed, err := url.Parse(raw); err == nil {
			if land := parsed.Query().Get("land"); land != "" {
				return land
			}
		}
	}

	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		decoded = raw
	}
	// Handle rco.mjbiz.co.kr redirect wrapper.
	if parts := strings.SplitN(decoded, "redirect?url=", 2); len(parts) == 2 {
		inner, err := url.QueryUnescape(parts[1])
		if err == nil {
			return inner
		}
		return parts[1]
	}
	return decoded
}

// FetchChart fetches historical price data from the Fallcent chart API.
// It visits the product page to obtain a chart seed and session cookie,
// then calls the chart API with an FNV-1a challenge. Returns the
// lowest_price_list as daily price points suitable for seeding local history.
func (r *FallcentResolver) FetchChart(ctx context.Context, fallcentProductID string) ([]int, error) {
	if fallcentProductID == "" {
		return nil, fmt.Errorf("fallcent product id is required")
	}

	page, err := r.fetchFallcentChartPage(ctx, fallcentProductID)
	if err != nil {
		return nil, err
	}
	chartData, err := r.fetchFallcentChartData(ctx, fallcentProductID, page)
	if err != nil {
		return nil, err
	}
	prices := collectValidChartPrices(chartData.Data.LowestPriceList)
	if len(prices) == 0 {
		return nil, fmt.Errorf("no valid prices in chart data")
	}

	return prices, nil
}

type fallcentChartPage struct {
	productURL string
	seed       int64
	sessionID  string
}

func (r *FallcentResolver) fetchFallcentChartPage(ctx context.Context, fallcentProductID string) (*fallcentChartPage, error) {
	productURL := fmt.Sprintf("%s/product/%s/", r.baseURL, fallcentProductID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, productURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create fallcent page request: %w", err)
	}
	req.Header.Set(fallcentHeaderUserAgent, fallcentBrowserUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ko-KR,ko;q=0.9")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch fallcent page: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fallcent page returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read fallcent page body: %w", err)
	}
	seed, err := extractChartSeed(string(body))
	if err != nil {
		return nil, err
	}
	sessionID := fallcentSessionID(resp.Cookies())
	if sessionID == "" {
		return nil, fmt.Errorf("no sessionid cookie in fallcent response")
	}
	return &fallcentChartPage{
		productURL: productURL,
		seed:       seed,
		sessionID:  sessionID,
	}, nil
}

func fallcentSessionID(cookies []*http.Cookie) string {
	for _, c := range cookies {
		if c.Name == "sessionid" {
			return c.Value
		}
	}
	return ""
}

func (r *FallcentResolver) fetchFallcentChartData(ctx context.Context, fallcentProductID string, page *fallcentChartPage) (*fallcentChartResponse, error) {
	ts := time.Now().Unix()
	challenge := fnv1aChallenge(page.seed, ts)
	chartURL := fmt.Sprintf("%s/api/v1/products/chart/%s/?challenge=%s&ts=%d&fp=x", r.baseURL, fallcentProductID, challenge, ts)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, chartURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create chart request: %w", err)
	}
	req.Header.Set(fallcentHeaderUserAgent, fallcentBrowserUserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", page.productURL)
	req.AddCookie(&http.Cookie{Name: "sessionid", Value: page.sessionID})

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch fallcent chart: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fallcent chart API returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read chart response: %w", err)
	}
	var chartData fallcentChartResponse
	if err := json.Unmarshal(body, &chartData); err != nil {
		return nil, fmt.Errorf("parse chart JSON: %w", err)
	}
	return &chartData, nil
}

func collectValidChartPrices(values []int) []int {
	prices := make([]int, 0, len(values))
	for _, p := range values {
		if p > 0 {
			prices = append(prices, p)
		}
	}
	return prices
}

type fallcentChartResponse struct {
	Data struct {
		DateList         []string `json:"date_list"`
		LowestPriceList  []int    `json:"lowest_price_list"`
		HighestPriceList []int    `json:"highest_price_list"`
	} `json:"data"`
}

var reChartSeed = regexp.MustCompile(`data-chart-seed="(\d+)"`)

func extractChartSeed(body string) (int64, error) {
	match := reChartSeed.FindStringSubmatch(body)
	if len(match) < 2 {
		return 0, fmt.Errorf("data-chart-seed not found in page")
	}
	seed, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse chart seed: %w", err)
	}
	return seed, nil
}

// fnv1aChallenge generates the FNV-1a challenge expected by the Fallcent chart API.
// It hashes seed (4 bytes big-endian) + timestamp (4 bytes big-endian) using FNV-1a 32-bit.
func fnv1aChallenge(seed, timestamp int64) string {
	data := make([]byte, 8)
	binary.BigEndian.PutUint32(data[0:4], uint32(seed))
	binary.BigEndian.PutUint32(data[4:8], uint32(timestamp))

	h := fnv.New32a()
	h.Write(data)
	return fmt.Sprintf("%08x", h.Sum32())
}

func queryOrPathValue(rawURL, key string) string {
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Query().Get(key)
}
