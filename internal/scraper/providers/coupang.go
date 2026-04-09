package providers

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"encoding/json"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/proxy"
)

// CoupangProduct holds scraped product data from a Coupang product page.
type CoupangProduct struct {
	ProductID    string `json:"product_id"`
	VendorItemID string `json:"vendor_item_id"`
	ItemID       string `json:"item_id"`
	Name         string `json:"name"`
	Price        int    `json:"price"` // KRW, integer
	ImageURL     string `json:"image_url"`
}

// CoupangScraper fetches product data from Coupang product pages by parsing JSON-LD.
type CoupangScraper struct {
	client  *BreakerHTTPClient
	logger  *slog.Logger
	lastReq time.Time
}

// NewCoupangScraper creates a new CoupangScraper.
// All TLS connections use a Chrome-like fingerprint via utls.
// If proxyURL is non-empty, requests are routed through that proxy (HTTP or SOCKS5).
func NewCoupangScraper(logger *slog.Logger, proxyURL string) *CoupangScraper {
	if logger == nil {
		logger = slog.Default()
	}
	jar, _ := cookiejar.New(nil)

	transport := newCoupangUTLSTransport(proxyURL, logger)

	client := &http.Client{
		Timeout: 15 * time.Second,
		Jar:     jar,
	}
	if transport != nil {
		client.Transport = transport
	}

	return &CoupangScraper{
		client: NewBreakerHTTPClient(client, "coupang_scraper", logger),
		logger: logger.With("component", "coupang_scraper"),
	}
}

// newCoupangUTLSTransport builds an http.Transport that uses utls to present a
// Chrome TLS fingerprint (HelloChrome_Auto). HTTP/2 is disabled so the Go
// standard transport handles HTTP/1.1 over the utls connection.
//
// When proxyURL is set:
//   - HTTP/HTTPS proxy: manual CONNECT tunnel, then utls handshake
//   - SOCKS5 proxy: golang.org/x/net/proxy dialer, then utls handshake
func newCoupangUTLSTransport(proxyURL string, logger *slog.Logger) *http.Transport {
	if logger == nil {
		logger = slog.Default()
	}
	var proxyDialer proxyDialFunc
	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			logger.Warn("invalid coupang scraper proxy URL, using direct connection", "proxy_url", proxyURL, "error", err)
		} else {
			proxyDialer = buildProxyDialer(parsed, logger)
		}
	}

	return &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		MaxConnsPerHost:     20,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   false,
		// Disable HTTP/2: the utls handshake may advertise h2 in ALPN (to look
		// like Chrome) but Go's HTTP/2 implementation is incompatible with utls.
		TLSNextProto: make(map[string]func(string, *tls.Conn) http.RoundTripper),
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialUTLS(ctx, network, addr, proxyDialer)
		},
	}
}

// proxyDialFunc dials the target address through a proxy, returning a raw
// TCP connection (the CONNECT/SOCKS tunnel is established but no TLS yet).
type proxyDialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// buildProxyDialer returns a proxyDialFunc for the given proxy URL.
func buildProxyDialer(parsed *url.URL, logger *slog.Logger) proxyDialFunc {
	switch parsed.Scheme {
	case "socks5", "socks5h":
		return buildSOCKS5Dialer(parsed, logger)
	default:
		return buildHTTPCONNECTDialer(parsed, logger)
	}
}

func buildSOCKS5Dialer(parsed *url.URL, logger *slog.Logger) proxyDialFunc {
	var auth *proxy.Auth
	if parsed.User != nil {
		password, _ := parsed.User.Password()
		auth = &proxy.Auth{
			User:     parsed.User.Username(),
			Password: password,
		}
	}
	dialer, err := proxy.SOCKS5("tcp", parsed.Host, auth, proxy.Direct)
	if err != nil {
		logger.Warn("failed to create SOCKS5 dialer, using direct connection", "error", err)
		return nil
	}
	logger.Info("coupang scraper using SOCKS5 proxy", "proxy", parsed.Host)
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialer.Dial(network, addr)
	}
}

func buildHTTPCONNECTDialer(parsed *url.URL, logger *slog.Logger) proxyDialFunc {
	proxyAddr := parsed.Host
	if !strings.Contains(proxyAddr, ":") {
		proxyAddr += ":80"
	}
	proxyAuthHeader := proxyAuthorizationHeader(parsed)
	logger.Info("coupang scraper using HTTP proxy", "proxy", parsed.Host)

	return func(ctx context.Context, _, addr string) (net.Conn, error) {
		conn, err := dialTCP(ctx, proxyAddr)
		if err != nil {
			return nil, fmt.Errorf("dial proxy %s: %w", proxyAddr, err)
		}

		if err := writeConnectRequest(conn, addr, proxyAuthHeader); err != nil {
			conn.Close()
			return nil, err
		}
		if err := validateConnectResponse(conn); err != nil {
			conn.Close()
			return nil, err
		}
		return conn, nil
	}
}

// dialUTLS dials the target (optionally through a proxy) and performs a utls
// handshake with a Chrome fingerprint.
func dialUTLS(ctx context.Context, network, addr string, proxyDial proxyDialFunc) (net.Conn, error) {
	conn, err := dialTargetConn(ctx, network, addr, proxyDial)
	if err != nil {
		return nil, err
	}

	spec, err := chromeSpecWithoutH2()
	if err != nil {
		conn.Close()
		return nil, err
	}

	uconn := utls.UClient(conn, &utls.Config{ServerName: tlsServerName(addr)}, utls.HelloCustom)
	if applyErr := uconn.ApplyPreset(&spec); applyErr != nil {
		conn.Close()
		return nil, fmt.Errorf("utls apply preset: %w", applyErr)
	}
	if err := uconn.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("utls handshake: %w", err)
	}
	return uconn, nil
}

func proxyAuthorizationHeader(parsed *url.URL) string {
	if parsed.User == nil {
		return ""
	}
	password, _ := parsed.User.Password()
	cred := parsed.User.Username() + ":" + password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(cred))
}

func dialTCP(ctx context.Context, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return dialer.DialContext(ctx, "tcp", addr)
}

func writeConnectRequest(conn net.Conn, addr, proxyAuthHeader string) error {
	if _, err := conn.Write([]byte(buildConnectRequest(addr, proxyAuthHeader))); err != nil {
		return fmt.Errorf("proxy CONNECT write: %w", err)
	}
	return nil
}

func buildConnectRequest(addr, proxyAuthHeader string) string {
	var connectReq strings.Builder
	connectReq.WriteString("CONNECT " + addr + " HTTP/1.1\r\n")
	connectReq.WriteString("Host: " + addr + "\r\n")
	if proxyAuthHeader != "" {
		connectReq.WriteString("Proxy-Authorization: " + proxyAuthHeader + "\r\n")
	}
	connectReq.WriteString("\r\n")
	return connectReq.String()
}

func validateConnectResponse(conn net.Conn) error {
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		return fmt.Errorf("proxy CONNECT response: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("proxy CONNECT status %d", resp.StatusCode)
	}
	if br.Buffered() > 0 {
		return fmt.Errorf("unexpected %d bytes after proxy CONNECT", br.Buffered())
	}
	return nil
}

func dialTargetConn(ctx context.Context, network, addr string, proxyDial proxyDialFunc) (net.Conn, error) {
	if proxyDial != nil {
		return proxyDial(ctx, network, addr)
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return dialer.DialContext(ctx, network, addr)
}

func tlsServerName(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func chromeSpecWithoutH2() (utls.ClientHelloSpec, error) {
	// Use UTLSIdToSpec to get a mutable copy of the Chrome fingerprint spec,
	// then remove h2 from ALPN before applying. HelloChrome_Auto is a
	// parroted fingerprint whose Extensions are empty until
	// BuildHandshakeState is called, so the previous removeH2ALPN approach
	// was a no-op. Using UTLSIdToSpec + HelloCustom avoids this issue.
	spec, err := utls.UTLSIdToSpec(utls.HelloChrome_Auto)
	if err != nil {
		return utls.ClientHelloSpec{}, fmt.Errorf("utls spec: %w", err)
	}
	for i, ext := range spec.Extensions {
		alpn, ok := ext.(*utls.ALPNExtension)
		if !ok {
			continue
		}
		spec.Extensions[i] = &utls.ALPNExtension{AlpnProtocols: filterALPNProtocols(alpn.AlpnProtocols)}
		break
	}
	return spec, nil
}

func filterALPNProtocols(protocols []string) []string {
	filtered := make([]string, 0, len(protocols))
	for _, proto := range protocols {
		if proto != "h2" {
			filtered = append(filtered, proto)
		}
	}
	if len(filtered) == 0 {
		return []string{"http/1.1"}
	}
	return filtered
}

// Chrome User-Agent strings for rotation.
var chromeUserAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36",
}

// FetchProduct scrapes a Coupang product page and returns product data including current price.
func (s *CoupangScraper) FetchProduct(ctx context.Context, cu *CoupangURL) (*CoupangProduct, error) {
	s.applyDelay()

	productURL := buildCoupangProductURL(cu)

	body, err := s.doGet(ctx, productURL)
	if err != nil {
		return nil, fmt.Errorf("fetch product page: %w", err)
	}

	// Try JSON-LD first.
	product, err := parseJSONLD(body)
	if err != nil {
		s.logger.Debug("JSON-LD parse failed, trying OG fallback", "error", err)
		// Fallback to OG tags.
		product, err = parseOGTags(body)
		if err != nil {
			return nil, fmt.Errorf("all parse methods failed: %w", err)
		}
	}

	product.ProductID = cu.ProductID
	product.VendorItemID = cu.VendorItemID
	product.ItemID = cu.ItemID

	return product, nil
}

// FetchCurrent is the refresh path entrypoint for the latest Coupang page price.
func (s *CoupangScraper) FetchCurrent(ctx context.Context, cu *CoupangURL) (*CoupangProduct, error) {
	return s.FetchProduct(ctx, cu)
}

func buildCoupangProductURL(cu *CoupangURL) string {
	productURL := fmt.Sprintf("https://www.coupang.com/vp/products/%s", cu.ProductID)
	if cu == nil {
		return productURL
	}

	query := url.Values{}
	if cu.ItemID != "" {
		query.Set("itemId", cu.ItemID)
	}
	if cu.VendorItemID != "" {
		query.Set("vendorItemId", cu.VendorItemID)
	}
	if encoded := query.Encode(); encoded != "" {
		productURL += "?" + encoded
	}
	return productURL
}

// applyDelay enforces 2-5 second random delay between requests.
func (s *CoupangScraper) applyDelay() {
	elapsed := time.Since(s.lastReq)
	minDelay := 2 * time.Second
	if elapsed < minDelay {
		delay := minDelay + time.Duration(rand.Int63n(int64(3*time.Second)))
		time.Sleep(delay - elapsed)
	}
	s.lastReq = time.Now()
}

func (s *CoupangScraper) setHeaders(req *http.Request) {
	ua := chromeUserAgents[rand.Intn(len(chromeUserAgents))]
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ko-KR,ko;q=0.9,en-US;q=0.8,en;q=0.7")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Sec-Ch-Ua", `"Chromium";v="131", "Not_A Brand";v="24"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", `"Windows"`)
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Connection", "keep-alive")
}

func (s *CoupangScraper) doGet(ctx context.Context, targetURL string) (string, error) {
	// Attempt 1: direct request.
	body, status, err := s.rawGet(ctx, targetURL)
	if err != nil {
		return "", err
	}
	if status == http.StatusOK {
		return body, nil
	}

	// On 403: warm up cookie jar by visiting the homepage first, then retry.
	if status == http.StatusForbidden {
		s.logger.Debug("403 received, warming up cookies via homepage")
		_, _, _ = s.rawGet(ctx, "https://www.coupang.com/")
		time.Sleep(1 * time.Second)

		body, status, err = s.rawGet(ctx, targetURL)
		if err != nil {
			return "", err
		}
		if status == http.StatusOK {
			return body, nil
		}
	}

	return "", fmt.Errorf("HTTP %d from %s", status, targetURL)
}

func (s *CoupangScraper) rawGet(ctx context.Context, url string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", 0, err
	}
	s.setHeaders(req)
	if !strings.Contains(url, "/vp/products/") {
		req.Header.Set("Referer", "https://www.google.com/")
	} else {
		req.Header.Set("Referer", "https://www.coupang.com/")
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, err
	}

	return string(data), resp.StatusCode, nil
}

// JSON-LD parsing.

var reJSONLD = regexp.MustCompile(`<script[^>]+type="application/ld\+json"[^>]*>([\s\S]*?)</script>`)

type jsonLDProduct struct {
	Type   string      `json:"@type"`
	Name   string      `json:"name"`
	Image  interface{} `json:"image"`  // can be string or []string
	Offers interface{} `json:"offers"` // can be object or array
}

type jsonLDOffer struct {
	Price         interface{} `json:"price"` // can be string or number
	PriceCurrency string      `json:"priceCurrency"`
}

func parseJSONLD(html string) (*CoupangProduct, error) {
	matches := reJSONLD.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("no JSON-LD script found")
	}

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		raw := strings.TrimSpace(match[1])

		var product jsonLDProduct
		if err := json.Unmarshal([]byte(raw), &product); err != nil {
			continue
		}

		if product.Type != "Product" && product.Type != "IndividualProduct" {
			continue
		}

		price, err := extractPrice(product.Offers)
		if err != nil {
			continue
		}

		imageURL := extractImage(product.Image)

		return &CoupangProduct{
			Name:     product.Name,
			Price:    price,
			ImageURL: imageURL,
		}, nil
	}

	return nil, fmt.Errorf("no Product JSON-LD with valid price found")
}

func extractPrice(offers interface{}) (int, error) {
	if offers == nil {
		return 0, fmt.Errorf("no offers")
	}

	var offer jsonLDOffer

	switch v := offers.(type) {
	case map[string]interface{}:
		data, _ := json.Marshal(v)
		if err := json.Unmarshal(data, &offer); err != nil {
			return 0, err
		}
	case []interface{}:
		if len(v) == 0 {
			return 0, fmt.Errorf("empty offers array")
		}
		data, _ := json.Marshal(v[0])
		if err := json.Unmarshal(data, &offer); err != nil {
			return 0, err
		}
	default:
		return 0, fmt.Errorf("unexpected offers type")
	}

	switch p := offer.Price.(type) {
	case string:
		price, err := strconv.Atoi(strings.ReplaceAll(p, ",", ""))
		if err != nil {
			return 0, fmt.Errorf("parse price string %q: %w", p, err)
		}
		return price, nil
	case float64:
		return int(p), nil
	default:
		return 0, fmt.Errorf("unexpected price type")
	}
}

func extractImage(img interface{}) string {
	switch v := img.(type) {
	case string:
		return v
	case []interface{}:
		if len(v) > 0 {
			if s, ok := v[0].(string); ok {
				return s
			}
		}
	}
	return ""
}

// OG tag fallback parsing.

var (
	reOGTitle = regexp.MustCompile(`<meta\s+property="og:title"\s+content="([^"]*)"`)
	reOGImage = regexp.MustCompile(`<meta\s+property="og:image"\s+content="([^"]*)"`)
)

func parseOGTags(html string) (*CoupangProduct, error) {
	titleMatch := reOGTitle.FindStringSubmatch(html)
	if len(titleMatch) < 2 {
		return nil, fmt.Errorf("no og:title found")
	}

	product := &CoupangProduct{
		Name: titleMatch[1],
	}

	imageMatch := reOGImage.FindStringSubmatch(html)
	if len(imageMatch) >= 2 {
		product.ImageURL = imageMatch[1]
	}

	// Try to find price from HTML (last resort).
	rePriceHTML := regexp.MustCompile(`class="total-price"\s*>\s*<strong>([0-9,]+)</strong>`)
	priceMatch := rePriceHTML.FindStringSubmatch(html)
	if len(priceMatch) >= 2 {
		price, err := strconv.Atoi(strings.ReplaceAll(priceMatch[1], ",", ""))
		if err == nil {
			product.Price = price
		}
	}

	if product.Price == 0 {
		return nil, fmt.Errorf("could not extract price from OG/HTML fallback")
	}

	return product, nil
}
