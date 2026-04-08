package providers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestFallcentResolverSearchCandidatesRespectsFanout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/product/search/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		fmt.Fprint(w, `
			<a href="/product/fc1/" class="block"><p class="mt-1 text-sm leading-snug line-clamp-2">첫 번째 상품</p></a>
			<a href="/product/fc2/" class="block"><p class="mt-1 text-sm leading-snug line-clamp-2">두 번째 상품</p></a>
			<a href="/product/fc3/" class="block"><p class="mt-1 text-sm leading-snug line-clamp-2">세 번째 상품</p></a>
		`)
	}))
	defer server.Close()

	resolver := NewFallcentResolver(slog.Default(), 2)
	resolver.baseURL = server.URL
	resolver.client = NewBreakerHTTPClient(server.Client(), "test", nil)

	candidates, err := resolver.SearchCandidates(context.Background(), "선풍기")
	if err != nil {
		t.Fatalf("search candidates: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidate count = %d, want 2", len(candidates))
	}
	if candidates[0].ProductID != "fc1" || candidates[1].ProductID != "fc2" {
		t.Fatalf("candidate ids = %#v, want fc1/fc2", candidates)
	}
}

func TestFallcentResolverFetchProductParsesDetail(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/product/fc1/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		fmt.Fprint(w, `
			<meta property="product:price:amount" content="12,345">
			<meta property="og:image" content="https://img.example/fc1.jpg">
			<h1>검증 상품</h1>
			<span>역대 최저가</span><span>10,900원</span>
			<a href="https://link.coupang.com/a/test?pageKey=9334776688&amp;itemId=20787679097&amp;vendorItemId=99334455">구매</a>
		`)
	}))
	defer server.Close()

	resolver := NewFallcentResolver(slog.Default(), 3)
	resolver.baseURL = server.URL
	resolver.client = NewBreakerHTTPClient(server.Client(), "test", nil)

	product, err := resolver.FetchProduct(context.Background(), "fc1")
	if err != nil {
		t.Fatalf("fetch product: %v", err)
	}
	if product.FallcentProductID != "fc1" {
		t.Fatalf("fallcent product id = %q, want fc1", product.FallcentProductID)
	}
	if product.ProductID != "9334776688" {
		t.Fatalf("product id = %q, want 9334776688", product.ProductID)
	}
	if product.ItemID != "20787679097" {
		t.Fatalf("item id = %q, want 20787679097", product.ItemID)
	}
	if product.VendorItemID != "99334455" {
		t.Fatalf("vendor item id = %q, want 99334455", product.VendorItemID)
	}
	if product.Price != 12345 {
		t.Fatalf("price = %d, want 12345", product.Price)
	}
	if product.LowestPrice != 10900 {
		t.Fatalf("lowest price = %d, want 10900", product.LowestPrice)
	}
}

func TestFallcentResolverResolveProductSkipsMismatchedCandidate(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle direct lookup attempt (returns 404 to fall through to keyword search).
		if r.URL.Path == "/product/" && r.URL.Query().Get("product_id") != "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.URL.Path {
		case "/product/search/":
			fmt.Fprint(w, `
				<a href="/product/fc1/" class="block"><p class="mt-1 text-sm leading-snug line-clamp-2">후보 1</p></a>
				<a href="/product/fc2/" class="block"><p class="mt-1 text-sm leading-snug line-clamp-2">후보 2</p></a>
			`)
		case "/product/fc1/":
			fmt.Fprint(w, fallcentDetailHTML("1111111111", "999", "888", 10000))
		case "/product/fc2/":
			fmt.Fprint(w, fallcentDetailHTML("9334776688", "20787679097", "555", 9800))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	resolver := NewFallcentResolver(slog.Default(), 3)
	resolver.baseURL = server.URL
	resolver.client = NewBreakerHTTPClient(server.Client(), "test", nil)

	product, err := resolver.ResolveProduct(context.Background(), &CoupangURL{
		ProductID: "9334776688",
		ItemID:    "20787679097",
	}, []string{"후보 상품"})
	if err != nil {
		t.Fatalf("resolve product: %v", err)
	}
	if product.FallcentProductID != "fc2" {
		t.Fatalf("fallcent product id = %q, want fc2", product.FallcentProductID)
	}
	if product.SearchKeyword != "후보 상품" {
		t.Fatalf("search keyword = %q, want 후보 상품", product.SearchKeyword)
	}
}

func TestFallcentResolverResolveProductHonorsCandidateLimit(t *testing.T) {
	t.Parallel()

	var fetched []string
	var fetchedMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle direct lookup attempt (returns 404 to fall through to keyword search).
		if r.URL.Path == "/product/" && r.URL.Query().Get("product_id") != "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.URL.Path {
		case "/product/search/":
			fmt.Fprint(w, `
				<a href="/product/fc1/" class="block"><p class="mt-1 text-sm leading-snug line-clamp-2">후보 1</p></a>
				<a href="/product/fc2/" class="block"><p class="mt-1 text-sm leading-snug line-clamp-2">후보 2</p></a>
				<a href="/product/fc3/" class="block"><p class="mt-1 text-sm leading-snug line-clamp-2">후보 3</p></a>
			`)
		case "/product/fc1/", "/product/fc2/", "/product/fc3/":
			fetchedMu.Lock()
			fetched = append(fetched, strings.Trim(r.URL.Path, "/"))
			fetchedMu.Unlock()
			switch r.URL.Path {
			case "/product/fc1/":
				fmt.Fprint(w, fallcentDetailHTML("1111111111", "1", "1", 10000))
			case "/product/fc2/":
				fmt.Fprint(w, fallcentDetailHTML("2222222222", "2", "2", 10100))
			case "/product/fc3/":
				fmt.Fprint(w, fallcentDetailHTML("9334776688", "20787679097", "3", 9800))
			}
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	resolver := NewFallcentResolver(slog.Default(), 2)
	resolver.baseURL = server.URL
	resolver.client = NewBreakerHTTPClient(server.Client(), "test", nil)

	_, err := resolver.ResolveProduct(context.Background(), &CoupangURL{
		ProductID: "9334776688",
		ItemID:    "20787679097",
	}, []string{"후보 상품"})
	if err == nil {
		t.Fatal("expected resolution to fail due to fanout limit")
	}
	fetchedMu.Lock()
	defer fetchedMu.Unlock()
	if len(fetched) != 2 {
		t.Fatalf("detail fetch count = %d, want 2", len(fetched))
	}
	if strings.Contains(strings.Join(fetched, ","), "fc3") {
		t.Fatalf("unexpected fetch beyond fanout limit: %v", fetched)
	}
}

func TestFallcentResolverLookupByCoupangID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/product/" && r.URL.Query().Get("product_id") != "" {
			pid := r.URL.Query().Get("product_id")
			iid := r.URL.Query().Get("item_id")
			if pid == "7852043719" && iid == "21404187529" {
				w.Header().Set("Location", "/product/Zz0jML6cu2XPea9AnCMkF7x3DfRVTQUz/")
				w.WriteHeader(http.StatusMovedPermanently)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path == "/product/Zz0jML6cu2XPea9AnCMkF7x3DfRVTQUz/" {
			fmt.Fprint(w, fallcentDetailHTML("7852043719", "21404187529", "88460766864", 7920))
			return
		}
		t.Fatalf("unexpected path: %s", r.URL.Path)
	}))
	defer server.Close()

	resolver := NewFallcentResolver(slog.Default(), 3)
	resolver.baseURL = server.URL

	// Successful lookup.
	data, err := resolver.LookupByCoupangID(context.Background(), "7852043719", "21404187529")
	if err != nil {
		t.Fatalf("LookupByCoupangID failed: %v", err)
	}
	if data.FallcentProductID != "Zz0jML6cu2XPea9AnCMkF7x3DfRVTQUz" {
		t.Fatalf("fallcent product id = %q, want Zz0jML6cu2XPea9AnCMkF7x3DfRVTQUz", data.FallcentProductID)
	}
	if data.ProductID != "7852043719" {
		t.Fatalf("product id = %q, want 7852043719", data.ProductID)
	}
	if data.Price != 7920 {
		t.Fatalf("price = %d, want 7920", data.Price)
	}

	// Product not on Fallcent.
	_, err = resolver.LookupByCoupangID(context.Background(), "9999999999", "1111111111")
	if err == nil {
		t.Fatal("expected error for unknown product")
	}

	// Missing item_id.
	_, err = resolver.LookupByCoupangID(context.Background(), "7852043719", "")
	if err == nil {
		t.Fatal("expected error for empty item_id")
	}
}

func TestFallcentResolverResolveProductUsesDirectLookup(t *testing.T) {
	t.Parallel()

	var searchCalled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/product/" && r.URL.Query().Get("product_id") != "" {
			// Direct lookup succeeds.
			w.Header().Set("Location", "/product/directID/")
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}
		if r.URL.Path == "/product/directID/" {
			fmt.Fprint(w, fallcentDetailHTML("7852043719", "21404187529", "88460766864", 7920))
			return
		}
		if r.URL.Path == "/product/search/" {
			searchCalled.Store(true)
			w.WriteHeader(http.StatusOK)
			return
		}
		t.Fatalf("unexpected path: %s", r.URL.Path)
	}))
	defer server.Close()

	resolver := NewFallcentResolver(slog.Default(), 3)
	resolver.baseURL = server.URL

	product, err := resolver.ResolveProduct(context.Background(), &CoupangURL{
		ProductID: "7852043719",
		ItemID:    "21404187529",
	}, []string{"불닭볶음면"})
	if err != nil {
		t.Fatalf("resolve product: %v", err)
	}
	if product.FallcentProductID != "directID" {
		t.Fatalf("fallcent product id = %q, want directID", product.FallcentProductID)
	}
	if searchCalled.Load() {
		t.Fatal("keyword search should not have been called when direct lookup succeeds")
	}
}

func fallcentDetailHTML(productID, itemID, vendorItemID string, price int) string {
	return fmt.Sprintf(`
		<meta property="product:price:amount" content="%d">
		<meta property="og:image" content="https://img.example/%s.jpg">
		<h1>상세 상품</h1>
		<span>역대 최저가</span><span>9,500원</span>
		<a href="https://link.coupang.com/a/test?pageKey=%s&amp;itemId=%s&amp;vendorItemId=%s">구매</a>
	`, price, productID, productID, itemID, vendorItemID)
}

func TestFallcentResolverFetchProductAdjoinRedirect(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/product/adj1/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		// Simulate the adjoin.co.kr wrapped purchase link as seen on real Fallcent pages.
		fmt.Fprint(w, `
			<meta property="product:price:amount" content="10,350">
			<meta property="og:image" content="https://img.example/adj1.jpg">
			<h1>Adjoin 래핑 상품</h1>
			<span>역대 최저가</span><span>9,800원</span>
			<a href="https://api.adjoin.co.kr/cou/land.php?code=fallcentsa1&amp;land=https%3A%2F%2Flink.coupang.com%2Fre%2FAFFSDP%3Flptag%3DAF1177509%26subid%3Dfallcentsa1%26pageKey%3D9304272091%26itemId%3D13261824081%26vendorItemId%3D80519507502%26traceid%3DV0-153%26token%3D31850C">구매</a>
		`)
	}))
	defer server.Close()

	resolver := NewFallcentResolver(slog.Default(), 3)
	resolver.baseURL = server.URL
	resolver.client = NewBreakerHTTPClient(server.Client(), "test", nil)

	product, err := resolver.FetchProduct(context.Background(), "adj1")
	if err != nil {
		t.Fatalf("fetch product: %v", err)
	}
	if product.FallcentProductID != "adj1" {
		t.Fatalf("fallcent product id = %q, want adj1", product.FallcentProductID)
	}
	if product.ProductID != "9304272091" {
		t.Fatalf("product id = %q, want 9304272091", product.ProductID)
	}
	if product.ItemID != "13261824081" {
		t.Fatalf("item id = %q, want 13261824081", product.ItemID)
	}
	if product.VendorItemID != "80519507502" {
		t.Fatalf("vendor item id = %q, want 80519507502", product.VendorItemID)
	}
	if product.Price != 10350 {
		t.Fatalf("price = %d, want 10350", product.Price)
	}
	if product.LowestPrice != 9800 {
		t.Fatalf("lowest price = %d, want 9800", product.LowestPrice)
	}
}

func TestFallcentResolverFetchChart(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/product/fc1/":
			http.SetCookie(w, &http.Cookie{Name: "sessionid", Value: "sess-1"})
			fmt.Fprint(w, `<div data-chart-seed="12345"></div>`)
		case strings.HasPrefix(r.URL.Path, "/api/v1/products/chart/fc1/"):
			if r.URL.Query().Get("challenge") == "" {
				t.Fatalf("missing challenge query in %s", r.URL.String())
			}
			if _, err := r.Cookie("sessionid"); err != nil {
				t.Fatalf("missing sessionid cookie: %v", err)
			}
			fmt.Fprint(w, `{"data":{"date_list":["2026-03-18","2026-03-19"],"lowest_price_list":[0,10100,9900],"highest_price_list":[12000,13000]}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	resolver := NewFallcentResolver(slog.Default(), 3)
	resolver.baseURL = server.URL
	resolver.client = NewBreakerHTTPClient(server.Client(), "test", nil)

	prices, err := resolver.FetchChart(context.Background(), "fc1")
	if err != nil {
		t.Fatalf("FetchChart: %v", err)
	}
	if len(prices) != 2 || prices[0] != 10100 || prices[1] != 9900 {
		t.Fatalf("prices = %+v", prices)
	}
}

func TestFallcentChartHelpers(t *testing.T) {
	t.Parallel()

	seed, err := extractChartSeed(`<div data-chart-seed="777"></div>`)
	if err != nil || seed != 777 {
		t.Fatalf("extractChartSeed = %d, %v", seed, err)
	}
	if _, err := extractChartSeed(`<div></div>`); err == nil {
		t.Fatal("expected extractChartSeed error")
	}

	if got := fnv1aChallenge(1, 2); len(got) != 8 {
		t.Fatalf("fnv1aChallenge length = %d (%q)", len(got), got)
	}
	if got := queryOrPathValue("https://x.test/a?itemId=123&vendorItemId=9", "itemId"); got != "123" {
		t.Fatalf("queryOrPathValue = %q", got)
	}
	if prices := collectValidChartPrices([]int{0, -1, 1, 2}); len(prices) != 2 {
		t.Fatalf("collectValidChartPrices = %+v", prices)
	}
}
