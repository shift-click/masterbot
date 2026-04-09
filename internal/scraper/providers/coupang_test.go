package providers

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuildCoupangProductURLIncludesVendorOnlyQuery(t *testing.T) {
	t.Parallel()

	got := buildCoupangProductURL(&CoupangURL{
		ProductID:    "6138942156",
		VendorItemID: "92085070745",
	})

	want := "https://www.coupang.com/vp/products/6138942156?vendorItemId=92085070745"
	if got != want {
		t.Fatalf("product url = %q, want %q", got, want)
	}
}

func TestParseJSONLDAndOGFallback(t *testing.T) {
	t.Parallel()

	jsonHTML := `<script type="application/ld+json">{"@type":"Product","name":"테스트 상품","image":["https://img"],"offers":{"price":"12,345"}}</script>`
	product, err := parseJSONLD(jsonHTML)
	if err != nil {
		t.Fatalf("parseJSONLD: %v", err)
	}
	if product.Name != "테스트 상품" || product.Price != 12345 {
		t.Fatalf("unexpected product: %+v", product)
	}

	fallbackHTML := `<meta property="og:title" content="OG 상품"><meta property="og:image" content="https://og-img"><span class="total-price"><strong>54,321</strong></span>`
	product, err = parseOGTags(fallbackHTML)
	if err != nil {
		t.Fatalf("parseOGTags: %v", err)
	}
	if product.Name != "OG 상품" || product.Price != 54321 {
		t.Fatalf("unexpected fallback product: %+v", product)
	}

	if _, err := extractPrice(nil); err == nil {
		t.Fatal("expected extractPrice nil error")
	}
	if got := extractImage([]interface{}{"https://a"}); got != "https://a" {
		t.Fatalf("extractImage = %q", got)
	}

	arrayHTML := `<script type="application/ld+json">{"@type":"Product","name":"배열 상품","image":"https://img","offers":[{"price":12345}]}</script>`
	arrayProduct, err := parseJSONLD(arrayHTML)
	if err != nil {
		t.Fatalf("parseJSONLD array offers: %v", err)
	}
	if arrayProduct.Price != 12345 {
		t.Fatalf("array product price = %d, want 12345", arrayProduct.Price)
	}

	if _, err := extractPrice(struct{}{}); err == nil {
		t.Fatal("expected extractPrice error for unexpected type")
	}
}

func TestCoupangScraperFetchProductAndRetry(t *testing.T) {
	t.Parallel()

	var productAttempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/":
			fmt.Fprint(w, "<html>home</html>")
		case strings.Contains(r.URL.Path, "/vp/products/"):
			if productAttempts.Add(1) == 1 {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, "<html>blocked</html>")
				return
			}
			fmt.Fprint(w, `<script type="application/ld+json">{"@type":"Product","name":"쿠팡 상품","image":"https://img","offers":{"price":11111}}</script>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	scraper := NewCoupangScraper(nil, "")
	scraper.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	scraper.client.Unwrap().Transport = rewriteTransport{base: srv.URL}

	product, err := scraper.FetchProduct(context.Background(), &CoupangURL{
		ProductID:    "6138942156",
		VendorItemID: "92085070745",
	})
	if err != nil {
		t.Fatalf("FetchProduct: %v", err)
	}
	if product.Name != "쿠팡 상품" || product.Price != 11111 {
		t.Fatalf("unexpected fetched product: %+v", product)
	}
	if productAttempts.Load() < 2 {
		t.Fatalf("expected retry after 403, attempts=%d", productAttempts.Load())
	}

	scraper.lastReq = time.Time{}
	current, err := scraper.FetchCurrent(context.Background(), &CoupangURL{ProductID: "6138942156"})
	if err != nil {
		t.Fatalf("FetchCurrent: %v", err)
	}
	if current.ProductID != "6138942156" {
		t.Fatalf("unexpected current product: %+v", current)
	}
}

func TestNewCoupangUTLSTransportDirect(t *testing.T) {
	t.Parallel()

	transport := newCoupangUTLSTransport("", nil)
	if transport == nil {
		t.Fatal("expected non-nil transport without proxy")
	}
	if transport.ForceAttemptHTTP2 {
		t.Fatal("expected ForceAttemptHTTP2 = false")
	}
	if transport.TLSNextProto == nil {
		t.Fatal("expected non-nil TLSNextProto (HTTP/2 disabled)")
	}
	if transport.DialTLSContext == nil {
		t.Fatal("expected DialTLSContext to be set")
	}
}

func TestNewCoupangUTLSTransportWithProxy(t *testing.T) {
	t.Parallel()

	transport := newCoupangUTLSTransport("http://user:pass@proxy.example.com:8080", nil)
	if transport == nil {
		t.Fatal("expected non-nil transport with proxy")
	}
	if transport.DialTLSContext == nil {
		t.Fatal("expected DialTLSContext to be set with proxy")
	}
}

func TestNewCoupangUTLSTransportSOCKS5(t *testing.T) {
	t.Parallel()

	transport := newCoupangUTLSTransport("socks5://user:pass@socks.example.com:1080", nil)
	if transport == nil {
		t.Fatal("expected non-nil transport with SOCKS5 proxy")
	}
	if transport.DialTLSContext == nil {
		t.Fatal("expected DialTLSContext to be set with SOCKS5 proxy")
	}
}

func TestNewCoupangUTLSTransportInvalidProxy(t *testing.T) {
	t.Parallel()

	transport := newCoupangUTLSTransport("://bad-url", nil)
	if transport == nil {
		t.Fatal("expected non-nil transport even with invalid proxy")
	}
}

func TestBuildConnectRequestIncludesProxyAuthorization(t *testing.T) {
	t.Parallel()

	got := buildConnectRequest("proxy.example.com:443", "Basic abc123")
	if !strings.Contains(got, "CONNECT proxy.example.com:443 HTTP/1.1\r\n") {
		t.Fatalf("missing CONNECT line: %q", got)
	}
	if !strings.Contains(got, "Host: proxy.example.com:443\r\n") {
		t.Fatalf("missing Host header: %q", got)
	}
	if !strings.Contains(got, "Proxy-Authorization: Basic abc123\r\n") {
		t.Fatalf("missing Proxy-Authorization header: %q", got)
	}
	if !strings.HasSuffix(got, "\r\n\r\n") {
		t.Fatalf("request should end with CRLF CRLF: %q", got)
	}
}

func TestWriteConnectRequestAndValidateConnectResponse(t *testing.T) {
	t.Parallel()

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	errCh := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(serverConn)
		req, err := http.ReadRequest(reader)
		if err != nil {
			errCh <- err
			return
		}
		if req.Method != http.MethodConnect || req.Host != "proxy.example.com:443" {
			errCh <- fmt.Errorf("unexpected request: method=%s host=%s", req.Method, req.Host)
			return
		}
		if got := req.Header.Get("Proxy-Authorization"); got != "Basic abc123" {
			errCh <- fmt.Errorf("proxy auth header = %q", got)
			return
		}
		_, err = io.WriteString(serverConn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		errCh <- err
	}()

	if err := writeConnectRequest(clientConn, "proxy.example.com:443", "Basic abc123"); err != nil {
		t.Fatalf("writeConnectRequest: %v", err)
	}
	if err := validateConnectResponse(clientConn); err != nil {
		t.Fatalf("validateConnectResponse: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("server side error: %v", err)
	}
}

func TestValidateConnectResponseRejectsTrailingBytes(t *testing.T) {
	t.Parallel()

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		_, _ = io.WriteString(serverConn, "HTTP/1.1 200 Connection Established\r\n\r\nx")
	}()

	if err := validateConnectResponse(clientConn); err == nil {
		t.Fatal("expected validateConnectResponse to reject trailing bytes")
	}
}

func TestDoGetReturnsErrorForNonForbiddenStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, "bad gateway")
	}))
	defer srv.Close()

	scraper := NewCoupangScraper(nil, "")
	scraper.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	scraper.client.Unwrap().Transport = rewriteTransport{base: srv.URL}

	if _, err := scraper.doGet(context.Background(), "https://www.coupang.com/vp/products/6138942156"); err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("doGet() error = %v, want HTTP 502", err)
	}
}

func TestRawGetSetsRefererByPath(t *testing.T) {
	t.Parallel()

	var referers []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		referers = append(referers, r.Header.Get("Referer"))
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	scraper := NewCoupangScraper(nil, "")
	scraper.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	scraper.client.Unwrap().Transport = rewriteTransport{base: srv.URL}

	if _, _, err := scraper.rawGet(context.Background(), "https://www.coupang.com/vp/products/6138942156"); err != nil {
		t.Fatalf("rawGet product path: %v", err)
	}
	if _, _, err := scraper.rawGet(context.Background(), "https://www.coupang.com/"); err != nil {
		t.Fatalf("rawGet home path: %v", err)
	}
	if len(referers) != 2 {
		t.Fatalf("referer count = %d, want 2", len(referers))
	}
	if referers[0] != "https://www.coupang.com/" {
		t.Fatalf("product referer = %q", referers[0])
	}
	if referers[1] != "https://www.google.com/" {
		t.Fatalf("non-product referer = %q", referers[1])
	}
}

func TestProxyAuthorizationHeaderAndTLSServerName(t *testing.T) {
	t.Parallel()

	parsed, err := url.Parse("http://user:pass@proxy.example.com:8080")
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}
	if got := proxyAuthorizationHeader(parsed); !strings.HasPrefix(got, "Basic ") {
		t.Fatalf("proxyAuthorizationHeader = %q", got)
	}
	if got := tlsServerName("proxy.example.com:443"); got != "proxy.example.com" {
		t.Fatalf("tlsServerName(host:port) = %q", got)
	}
	if got := tlsServerName("proxy.example.com"); got != "proxy.example.com" {
		t.Fatalf("tlsServerName(host) = %q", got)
	}
}

func TestFilterALPNProtocolsDropsH2AndFallsBack(t *testing.T) {
	t.Parallel()

	filtered := filterALPNProtocols([]string{"h2", "http/1.1"})
	if len(filtered) != 1 || filtered[0] != "http/1.1" {
		t.Fatalf("filtered = %v", filtered)
	}

	fallback := filterALPNProtocols([]string{"h2"})
	if len(fallback) != 1 || fallback[0] != "http/1.1" {
		t.Fatalf("fallback = %v", fallback)
	}
}
