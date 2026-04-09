package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/shift-click/masterbot/internal/scraper"
	"github.com/shift-click/masterbot/internal/scraper/providers"
)

type cointestRT func(*http.Request) (*http.Response, error)

func (f cointestRT) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCointestHelperFunctions(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	cache := scraper.NewCoinCache(logger)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	printSection("unit test")
	runAliasTest()
	rate := runForexTest(ctx, logger, cache)
	_ = runMarketCapTest(ctx, logger, cache)
	runDexTest(ctx, logger, rate)
	runCEXFormatTest(providers.ForexRate{Rate: 1300}, providers.NewCoinGecko(logger))
}

func TestMainRunsWithMockTransport(t *testing.T) {
	oldTransport := http.DefaultTransport
	http.DefaultTransport = cointestRT(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("mock transport")
	})
	defer func() { http.DefaultTransport = oldTransport }()

	main()
}

func TestCointestSuccessBranchesWithMockResponses(t *testing.T) {
	// Use a real httptest server to bypass provider-internal transports.
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/forex"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `[{"code":"FRX.KRWUSD","currencyCode":"USD","country":"미국","basePrice":1300,"currencyUnit":1,"signedChangePrice":0,"signedChangeRate":0}]`)
		case strings.Contains(r.URL.Path, "/coins/markets"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `[
				{"id":"bitcoin","symbol":"btc","market_cap":100},
				{"id":"ethereum","symbol":"eth","market_cap":80},
				{"id":"solana","symbol":"sol","market_cap":60}
			]`)
		case strings.Contains(r.URL.Path, "/latest/dex/search"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"pairs":[{"chainId":"ethereum","dexId":"uniswap","pairAddress":"0xpair","baseToken":{"address":"0xabc","name":"Pepe","symbol":"pepe"},"priceUsd":"0.00000123","volume":{"h24":1000},"liquidity":{"usd":5000},"fdv":2000,"marketCap":1500,"priceChange":{"h24":2.5}}]}`)
		default:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{}`)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cache := scraper.NewCoinCache(logger)
	ctx := context.Background()

	// Create providers pointing at the test server.
	forex := providers.NewDunamuForex(logger)
	forex.SetTransport(newRewriteTransport(srv.URL))

	coinGecko := providers.NewCoinGecko(logger)
	coinGecko.SetTransport(newRewriteTransport(srv.URL))

	printSection("Dunamu Forex Rate (mock)")
	rate, err := forex.FetchRate(ctx)
	if err != nil {
		t.Fatalf("expected forex success, got error: %v", err)
	}
	if rate.Rate <= 0 {
		t.Fatalf("expected positive rate, got %+v", rate)
	}
	cache.UpdateForexRate(rate.Rate)

	printSection("CoinGecko Market Caps (mock)")
	if err := coinGecko.FetchMarketCaps(ctx); err != nil {
		t.Fatalf("expected market caps success, got error: %v", err)
	}
	if _, ok := coinGecko.MarketCap("BTC"); !ok {
		t.Fatal("expected BTC market cap after mock fetch")
	}

	runDexTest(ctx, logger, rate)
	runCEXFormatTest(rate, coinGecko)
}

// rewriteTransport redirects all requests to a target URL (test server)
// while preserving the original path.
type rewriteTransport struct {
	targetBase string
	inner      http.RoundTripper
}

func newRewriteTransport(targetBase string) *rewriteTransport {
	return &rewriteTransport{targetBase: targetBase, inner: http.DefaultTransport}
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = "http"
	req2.URL.Host = strings.TrimPrefix(t.targetBase, "http://")
	return t.inner.RoundTrip(req2)
}
