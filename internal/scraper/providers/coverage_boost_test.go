package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNaverTitleResolveAndExtractHelpers(t *testing.T) {
	t.Parallel()

	jsonPage := `"href":"https://www.coupang.com/vp/products/12345","title":"\uac00\ub098\ub2e4 - \uae30\ud0c0","titleVariant":"x"`
	if got := extractTitleFromJSON(jsonPage, "12345"); got != "가나다" {
		t.Fatalf("extractTitleFromJSON = %q", got)
	}

	htmlPage := `<a href="https://www.coupang.com/vp/products/12345"><span class="sds-comps-text">상품명 - 기타</span></a>`
	if got := extractTitleFromHTML(htmlPage, "12345"); got != "상품명" {
		t.Fatalf("extractTitleFromHTML = %q", got)
	}
	if got := cleanProductTitle("이름 - 기타카테고리"); got != "이름" {
		t.Fatalf("cleanProductTitle = %q", got)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, htmlPage)
	}))
	defer srv.Close()

	resolver := NewNaverTitleResolver(nil)
	resolver.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	resolver.client.Unwrap().Transport = rewriteTransport{base: srv.URL}

	title, err := resolver.ResolveTitle(context.Background(), "12345")
	if err != nil {
		t.Fatalf("ResolveTitle: %v", err)
	}
	if title != "상품명" {
		t.Fatalf("title = %q", title)
	}
	if _, err := resolver.ResolveTitle(context.Background(), ""); err == nil {
		t.Fatal("expected empty product id error")
	}
}

func TestGoogleNewsTopNewsCaching(t *testing.T) {
	t.Parallel()

	var statusCode atomic.Int32
	statusCode.Store(http.StatusOK)
	body := `<?xml version="1.0" encoding="UTF-8"?><rss><channel><item><title>A</title><link>u1</link><source>s1</source></item><item><title>B</title><link>u2</link><source>s2</source></item></channel></rss>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := int(statusCode.Load())
		w.WriteHeader(code)
		if code == http.StatusOK {
			fmt.Fprint(w, body)
		}
	}))
	defer srv.Close()

	g := NewGoogleNews(nil)
	g.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	g.client.Unwrap().Transport = rewriteTransport{base: srv.URL}
	g.cacheTTL = 10 * time.Minute

	items, err := g.TopNews(context.Background(), 2)
	if err != nil {
		t.Fatalf("TopNews first fetch: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items len = %d", len(items))
	}

	statusCode.Store(http.StatusInternalServerError)
	items, err = g.TopNews(context.Background(), 2)
	if err != nil {
		t.Fatalf("TopNews cached fetch should not fail: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("cached items len = %d", len(items))
	}

	g.mu.Lock()
	g.updatedAt = time.Now().Add(-2 * g.cacheTTL)
	g.mu.Unlock()
	items, err = g.TopNews(context.Background(), 2)
	if err != nil {
		t.Fatalf("TopNews stale fallback should not fail: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("stale items len = %d", len(items))
	}
}

func TestGoogleTrendsRankChanges(t *testing.T) {
	t.Parallel()

	var call atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		currentCall := call.Add(1)
		if currentCall == 1 {
			fmt.Fprint(w, `<?xml version="1.0"?><rss><channel><item><title>A</title><approx_traffic>100+</approx_traffic></item><item><title>B</title><approx_traffic>90+</approx_traffic></item></channel></rss>`)
			return
		}
		fmt.Fprint(w, `<?xml version="1.0"?><rss><channel><item><title>B</title><approx_traffic>100+</approx_traffic></item><item><title>C</title><approx_traffic>90+</approx_traffic></item><item><title>A</title><approx_traffic>80+</approx_traffic></item></channel></rss>`)
	}))
	defer srv.Close()

	g := NewGoogleTrends(nil)
	g.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	g.client.Unwrap().Transport = rewriteTransport{base: srv.URL}
	g.cacheTTL = time.Hour

	first, err := g.Trends(context.Background())
	if err != nil {
		t.Fatalf("first Trends: %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("first len = %d", len(first))
	}

	g.mu.Lock()
	g.updatedAt = time.Now().Add(-2 * g.cacheTTL)
	g.mu.Unlock()
	second, err := g.Trends(context.Background())
	if err != nil {
		t.Fatalf("second Trends: %v", err)
	}
	if len(second) != 3 {
		t.Fatalf("second len = %d", len(second))
	}
	if second[0].Change != TrendChangeUp {
		t.Fatalf("expected first item to be up: %+v", second[0])
	}
	if second[1].Change != TrendChangeNew {
		t.Fatalf("expected second item to be new: %+v", second[1])
	}
	if second[2].Change != TrendChangeDown {
		t.Fatalf("expected third item to be down: %+v", second[2])
	}
}

func TestCoinAliasesAndResolver(t *testing.T) {
	t.Parallel()

	aliases := NewCoinAliases()
	if sym, ok := aliases.Lookup("비트코인"); !ok || sym != "BTC" {
		t.Fatalf("lookup 비트코인 = %q,%v", sym, ok)
	}
	if sym, ok := aliases.Lookup("빗코"); !ok || sym != "BTC" {
		t.Fatalf("lookup 빗코 = %q,%v", sym, ok)
	}
	if sym, ok := aliases.Lookup("wormhole"); !ok || sym != "W" {
		t.Fatalf("lookup wormhole = %q,%v", sym, ok)
	}
	if _, ok := aliases.Lookup("마나"); ok {
		t.Fatal("guarded generated alias 마나 should not resolve by default")
	}
	if !aliases.IsCoinTicker("btc") {
		t.Fatal("expected btc to be recognized")
	}

	resolver := NewCoinResolver(aliases, nil, nil, nil)
	if result, ok := resolver.ResolveLocalOnly("0x1234567890"); !ok || result.Tier != CoinTierDEX {
		t.Fatalf("contract local resolve = %+v,%v", result, ok)
	}
	if result, ok := resolver.ResolveLocalOnly("이더리움"); !ok || result.Symbol != "ETH" {
		t.Fatalf("alias local resolve = %+v,%v", result, ok)
	}
	if result, ok := resolver.ResolveLocalOnly("빗코"); !ok || result.Symbol != "BTC" {
		t.Fatalf("generated alias local resolve = %+v,%v", result, ok)
	}
	if result, ok := resolver.ResolveLocalOnly("모나드"); !ok || result.Tier != CoinTierDEX || result.Symbol != "MON" {
		t.Fatalf("korean dex local resolve = %+v,%v", result, ok)
	}
	if result, ok := resolver.ResolveLocalOnly("MON"); !ok || result.Tier != CoinTierDEX || result.ContractAddress == "" {
		t.Fatalf("curated conflicting ticker resolve = %+v,%v", result, ok)
	}

	cg := NewCoinGecko(nil)
	cg.idList["ABC"] = "abc-coin"
	resolver = NewCoinResolver(aliases, cg, nil, nil)
	if result, ok := resolver.Resolve(context.Background(), "abc"); !ok || result.CoinGeckoID != "abc-coin" {
		t.Fatalf("coingecko resolve = %+v,%v", result, ok)
	}
}

func TestCoinResolverRefreshesLocalDEXMetadata(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(strings.ToLower(r.URL.Path), "/latest/dex/tokens/0xmon") {
			fmt.Fprint(w, `{"pairs":[{"chainId":"monad","dexId":"uniswap","pairAddress":"pair-refresh","baseToken":{"address":"0xmon","name":"Monad","symbol":"mon"},"priceUsd":"0.02","volume":{"h24":100},"liquidity":{"usd":200},"fdv":300,"marketCap":250,"priceChange":{"h24":1.5}}]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	aliases := &CoinAliases{
		aliases: map[string]string{"MON": "monad"},
		localResults: map[string]CoinSearchResult{
			"monad": {
				Symbol:          "MON",
				Name:            "Monad",
				Tier:            CoinTierDEX,
				ContractAddress: "0xmon",
			},
		},
	}

	d := NewDexScreener(nil)
	d.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	d.client.Unwrap().Transport = rewriteTransport{base: srv.URL}

	resolver := NewCoinResolver(aliases, nil, d, nil)
	result, ok := resolver.Resolve(context.Background(), "MON")
	if !ok {
		t.Fatal("expected MON to resolve")
	}
	if result.PairAddress != "pair-refresh" {
		t.Fatalf("pair address = %q, want pair-refresh", result.PairAddress)
	}
	if result.ChainID != "monad" {
		t.Fatalf("chain id = %q, want monad", result.ChainID)
	}
}

func TestDexScreenerSearchFetchAndBatch(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/latest/dex/search"):
			fmt.Fprint(w, `{"pairs":[{"chainId":"solana","dexId":"raydium","pairAddress":"pair1","baseToken":{"address":"addr1","name":"Token One","symbol":"one"},"priceUsd":"1.23","volume":{"h24":100},"liquidity":{"usd":200},"fdv":300,"marketCap":250,"priceChange":{"h24":4.5}}]}`)
		case strings.Contains(r.URL.Path, "/latest/dex/tokens/"):
			fmt.Fprint(w, `{"pairs":[{"chainId":"ethereum","dexId":"uniswap","pairAddress":"pair2","baseToken":{"address":"0xabc","name":"Token Two","symbol":"two"},"priceUsd":"2.34","volume":{"h24":111},"liquidity":{"usd":222},"fdv":333,"marketCap":222,"priceChange":{"h24":-1.5}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	d := NewDexScreener(nil)
	d.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	d.client.Unwrap().Transport = rewriteTransport{base: srv.URL}

	q, err := d.Search(context.Background(), "one")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if q.Symbol != "ONE" || q.DEXName == "" {
		t.Fatalf("unexpected search quote: %+v", q)
	}

	q, err = d.FetchByAddress(context.Background(), "0xabc")
	if err != nil {
		t.Fatalf("FetchByAddress: %v", err)
	}
	if q.Symbol != "TWO" {
		t.Fatalf("unexpected address quote: %+v", q)
	}

	batch, err := d.FetchBatch(context.Background(), []string{"0xabc"})
	if err != nil {
		t.Fatalf("FetchBatch: %v", err)
	}
	if len(batch) != 1 {
		t.Fatalf("batch len = %d", len(batch))
	}
}

func TestCoinGeckoFetchesAndPolling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/v3/coins/list"):
			fmt.Fprint(w, `[{"id":"bitcoin","symbol":"btc"},{"id":"ethereum","symbol":"eth"}]`)
		case strings.Contains(r.URL.Path, "/api/v3/coins/markets"):
			fmt.Fprint(w, `[{"id":"bitcoin","symbol":"btc","market_cap":1000}]`)
		case strings.Contains(r.URL.Path, "/api/v3/coins/bitcoin"):
			fmt.Fprint(w, `{"symbol":"btc","name":"Bitcoin","market_data":{"current_price":{"usd":10},"price_change_percentage_24h":1.5,"price_change_24h":0.5,"market_cap":{"usd":1000}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cg := NewCoinGecko(nil)
	cg.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	cg.client.Unwrap().Transport = rewriteTransport{base: srv.URL}

	if err := cg.FetchIDList(context.Background()); err != nil {
		t.Fatalf("FetchIDList: %v", err)
	}
	if id, ok := cg.LookupID("BTC"); !ok || id != "bitcoin" {
		t.Fatalf("LookupID BTC = %q,%v", id, ok)
	}

	if err := cg.FetchMarketCaps(context.Background()); err != nil {
		t.Fatalf("FetchMarketCaps: %v", err)
	}
	if cap, ok := cg.MarketCap("btc"); !ok || cap != 1000 {
		t.Fatalf("MarketCap btc = %v,%v", cap, ok)
	}

	quote, err := cg.FetchPrice(context.Background(), "bitcoin")
	if err != nil {
		t.Fatalf("FetchPrice: %v", err)
	}
	if quote.Symbol != "BTC" || quote.USDPrice != 10 {
		t.Fatalf("unexpected quote: %+v", quote)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		cg.StartPolling(ctx, 10*time.Millisecond, 10*time.Millisecond)
		close(done)
	}()
	time.Sleep(25 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("StartPolling did not stop after cancel")
	}
}

func TestAPIFootballFetchAndHelpers(t *testing.T) {
	t.Parallel()

	extra := 3
	if got := formatMinute(90, &extra); got != "90'+3'" {
		t.Fatalf("formatMinute = %q", got)
	}
	if eventType, ok := mapAPIFootballEventType("Goal", "Normal Goal"); !ok || eventType != EventGoal {
		t.Fatalf("mapAPIFootballEventType = %v,%v", eventType, ok)
	}
	if !TeamNameMatch("Jeonbuk Hyundai Motors", "Jeonbuk FC") {
		t.Fatal("expected team name match")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/fixtures/events"):
			fmt.Fprint(w, `{"response":[{"time":{"elapsed":12},"team":{"name":"Home"},"player":{"name":"P"},"assist":{"name":"A"},"type":"Goal","detail":"Normal Goal"}]}`)
		case strings.Contains(r.URL.Path, "/fixtures"):
			fmt.Fprint(w, `{"response":[{"fixture":{"id":123},"teams":{"home":{"name":"Jeonbuk Hyundai Motors"},"away":{"name":"Ulsan HD"}}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	af := NewAPIFootball("key", nil)
	af.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	af.client.Unwrap().Transport = rewriteTransport{base: srv.URL}

	fixtures, err := af.FetchFixtures(context.Background(), "2026-03-19", 292, 2026)
	if err != nil {
		t.Fatalf("FetchFixtures: %v", err)
	}
	if len(fixtures) != 1 || fixtures[0].ID != 123 {
		t.Fatalf("unexpected fixtures: %+v", fixtures)
	}

	events, err := af.FetchEvents(context.Background(), 123)
	if err != nil {
		t.Fatalf("FetchEvents: %v", err)
	}
	if len(events) != 1 || events[0].Player != "P" {
		t.Fatalf("unexpected events: %+v", events)
	}

	events, err = af.FetchEventsForMatch(context.Background(), "2026-03-19", 292, 2026, "Jeonbuk FC", "Ulsan")
	if err != nil {
		t.Fatalf("FetchEventsForMatch: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("unexpected matched events: %+v", events)
	}
}

func TestLoLEsportsParsingAndStandings(t *testing.T) {
	t.Parallel()

	l := NewLoLEsports(nil)
	matches := l.parseEvents([]lolesportsEvent{
		{
			StartTime: "2026-03-19T12:00:00Z",
			State:     "completed",
			BlockName: "1주차",
			Match: struct {
				Teams    []lolesportsTeam `json:"teams"`
				Strategy struct {
					Type  string `json:"type"`
					Count int    `json:"count"`
				} `json:"strategy"`
			}{
				Teams: []lolesportsTeam{
					{Name: "T1", Code: "T1", Result: struct {
						Outcome  string `json:"outcome"`
						GameWins int    `json:"gameWins"`
					}{GameWins: 2}},
					{Name: "GEN", Code: "GEN", Result: struct {
						Outcome  string `json:"outcome"`
						GameWins int    `json:"gameWins"`
					}{GameWins: 1}},
				},
				Strategy: struct {
					Type  string `json:"type"`
					Count int    `json:"count"`
				}{Type: "bestOf", Count: 3},
			},
		},
	})
	if len(matches) != 1 || matches[0].Status != MatchFinished {
		t.Fatalf("unexpected matches: %+v", matches)
	}

	standings := collectLoLEsportsStandings(lolesportsStandingsResponse{
		Data: struct {
			Standings []struct {
				Stages []struct {
					Sections []struct {
						Rankings []lolesportsRanking `json:"rankings"`
					} `json:"sections"`
				} `json:"stages"`
			} `json:"standings"`
		}{
			Standings: []struct {
				Stages []struct {
					Sections []struct {
						Rankings []lolesportsRanking `json:"rankings"`
					} `json:"sections"`
				} `json:"stages"`
			}{
				{
					Stages: []struct {
						Sections []struct {
							Rankings []lolesportsRanking `json:"rankings"`
						} `json:"sections"`
					}{
						{
							Sections: []struct {
								Rankings []lolesportsRanking `json:"rankings"`
							}{
								{
									Rankings: []lolesportsRanking{
										{
											Ordinal: 1,
											Teams: []struct {
												Name   string `json:"name"`
												Code   string `json:"code"`
												Record struct {
													Wins   int `json:"wins"`
													Losses int `json:"losses"`
												} `json:"record"`
											}{
												{
													Name: "T1",
													Code: "T1",
													Record: struct {
														Wins   int `json:"wins"`
														Losses int `json:"losses"`
													}{Wins: 10, Losses: 2},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	})
	if len(standings) != 1 || standings[0].TeamCode != "T1" {
		t.Fatalf("unexpected standings: %+v", standings)
	}
}

func TestBaseballRegistryAndNames(t *testing.T) {
	t.Parallel()

	leagues := BaseballLeagues()
	if len(leagues) == 0 {
		t.Fatal("expected baseball leagues")
	}
	if _, ok := LookupBaseballLeague("kbo"); !ok {
		t.Fatal("expected kbo lookup")
	}
	if got := TranslateBaseballTeamName("Los Angeles Dodgers"); got == "" {
		t.Fatal("expected translated team name")
	}
}

func TestProviderRewriteTransportUtility(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	req, _ := http.NewRequest(http.MethodGet, "https://example.com/path", nil)
	req.URL.Scheme = "https"
	req.URL.Host = "example.com"
	rt := rewriteTransport{base: srv.URL}
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer resp.Body.Close()
	if req.URL.Scheme != u.Scheme || req.URL.Host != u.Host {
		t.Fatalf("request was not rewritten: %s://%s", req.URL.Scheme, req.URL.Host)
	}
}
