package scraper

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/scraper/providers"
)

type stubScraperProvider struct {
	name   string
	result Result
	err    error
}

func (s stubScraperProvider) Fetch(context.Context, string) (Result, error) {
	if s.err != nil {
		return Result{}, s.err
	}
	return s.result, nil
}

func (s stubScraperProvider) Name() string { return s.name }

type stubCache struct {
	getResult     Result
	getOK         bool
	getErr        error
	staleResult   Result
	staleOK       bool
	staleErr      error
	setCalled     bool
	setKey        string
	setResultData []byte
}

func (s *stubCache) Get(context.Context, string) (Result, bool, error) {
	return s.getResult, s.getOK, s.getErr
}

func (s *stubCache) Set(_ context.Context, key string, result Result, _ time.Duration) error {
	s.setCalled = true
	s.setKey = key
	s.setResultData = append([]byte(nil), result.Data...)
	return nil
}

func (s *stubCache) GetStale(context.Context, string) (Result, bool, error) {
	return s.staleResult, s.staleOK, s.staleErr
}

func TestFallbackChainUsesCacheThenProvidersAndStale(t *testing.T) {
	t.Parallel()

	cache := &stubCache{
		getResult: Result{Data: json.RawMessage(`{"a":1}`)},
		getOK:     true,
	}
	chain := NewFallbackChain(nil, cache, time.Minute, slog.Default())

	got, err := chain.Fetch(context.Background(), "k", "q")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if !got.IsCached {
		t.Fatal("expected cache hit to set IsCached")
	}

	cache = &stubCache{
		staleResult: Result{Data: json.RawMessage(`{"stale":true}`)},
		staleOK:     true,
	}
	chain = NewFallbackChain(
		[]Scraper{stubScraperProvider{name: "p1", err: errors.New("boom")}},
		cache,
		time.Minute,
		slog.Default(),
	)
	got, err = chain.Fetch(context.Background(), "k2", "q2")
	if err != nil {
		t.Fatalf("Fetch() stale fallback error = %v", err)
	}
	if !got.IsCached {
		t.Fatal("expected stale fallback to be marked cached")
	}

	cache = &stubCache{}
	chain = NewFallbackChain(
		[]Scraper{stubScraperProvider{name: "ok", result: Result{Data: json.RawMessage(`{"x":1}`)}}},
		cache,
		time.Minute,
		slog.Default(),
	)
	got, err = chain.Fetch(context.Background(), "key-ok", "query-ok")
	if err != nil {
		t.Fatalf("Fetch() provider success error = %v", err)
	}
	if got.Source != "ok" || got.FetchedAt.IsZero() {
		t.Fatalf("unexpected provider result: %+v", got)
	}
	if !cache.setCalled || cache.setKey != "key-ok" {
		t.Fatalf("expected cache set call, got called=%v key=%q", cache.setCalled, cache.setKey)
	}

	chain = NewFallbackChain(nil, nil, time.Minute, slog.Default())
	if _, err := chain.Fetch(context.Background(), "none", "none"); err == nil {
		t.Fatal("expected error for missing providers")
	}
}

func TestCoinCacheAndFormattingPaths(t *testing.T) {
	t.Parallel()

	cache := NewCoinCache(nil)
	if cache.GetCEX("nope") != nil {
		t.Fatal("expected empty cache miss for CEX")
	}

	cache.UpdateForexRate(1000)
	cache.OnBinanceUpdate(providers.BinanceTickerUpdate{
		Symbol:    "btc",
		Price:     10,
		PrevClose: 9,
		Change:    1,
		ChangePct: 10,
	})
	cache.OnUpbitUpdate(providers.UpbitTickerUpdate{
		Symbol:    "BTC",
		TradePrice: 12000,
		PrevClose:  11000,
		Change:     1000,
		ChangePct:  0.1,
	})

	btc := cache.GetCEX("btc")
	if btc == nil {
		t.Fatal("expected BTC quote")
	}
	if btc.Name != "비트코인" {
		t.Fatalf("btc name = %q", btc.Name)
	}
	if math.Abs(btc.KimchiPremium-20) > 0.0001 {
		t.Fatalf("kimchi premium = %.4f, want 20", btc.KimchiPremium)
	}
	if math.Abs(btc.KRWChangePct-10) > 0.0001 {
		t.Fatalf("krw change pct = %.4f, want 10", btc.KRWChangePct)
	}

	cache.OnBinanceUpdate(providers.BinanceTickerUpdate{Symbol: "XYZ", Price: 3})
	if got := cache.GetCEX("xyz"); got == nil || got.Name != "XYZ" {
		t.Fatalf("unknown symbol name handling failed: %+v", got)
	}

	cache.SetDEX(nil)
	cache.SetDEX(&providers.DEXQuote{})
	dex := &providers.DEXQuote{
		ContractAddress: "0xABC",
		USDPrice:        2,
		USDChangePct24h: 3.5,
	}
	cache.SetDEX(dex)
	gotDEX := cache.GetDEX("0xabc")
	if gotDEX == nil {
		t.Fatal("expected DEX quote")
	}
	if gotDEX.KRWPrice != 2000 || gotDEX.KRWChangePct24h != 3.5 {
		t.Fatalf("unexpected DEX conversion: %+v", gotDEX)
	}
	cache.UpdateForexRate(1200)
	gotDEX = cache.GetDEX("0xabc")
	if gotDEX.KRWPrice != 2400 {
		t.Fatalf("expected DEX KRW recompute, got %.2f", gotDEX.KRWPrice)
	}

	cache.UpdateMarketCap("btc", 123)
	if cache.GetCEX("BTC").MarketCap != 123 {
		t.Fatalf("market cap not updated on quote: %+v", cache.GetCEX("BTC"))
	}
	cache.UpdateMarketCaps(map[string]float64{"btc": 456, "eth": 789})
	if cache.GetCEX("BTC").MarketCap != 456 {
		t.Fatalf("bulk market cap update failed: %+v", cache.GetCEX("BTC"))
	}

	if cache.ForexRate() != 1200 {
		t.Fatalf("forex rate = %.2f", cache.ForexRate())
	}

	if got := FormatMarketCapKRW(0, 1200); got != "" {
		t.Fatalf("FormatMarketCapKRW zero = %q, want empty", got)
	}
	if got := FormatMarketCapKRW(1_500_000_000_000, 1450); got == "" {
		t.Fatal("expected formatted market cap string")
	}
	if got := formatKoreanAmount(-12000); got != "-1조 2,000억" {
		t.Fatalf("formatKoreanAmount = %q", got)
	}
	if got := formatWithCommasCoin(1_234_567); got != "1,234,567" {
		t.Fatalf("formatWithCommasCoin = %q", got)
	}
	if got := itoa(0); got != "0" {
		t.Fatalf("itoa(0) = %q", got)
	}
}

func TestMemoryCacheAndWeatherCache(t *testing.T) {
	t.Parallel()

	mem := NewMemoryCache(50 * time.Millisecond)
	ctx := context.Background()
	original := Result{Data: []byte("hello")}
	if err := mem.Set(ctx, "k", original, 50*time.Millisecond); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	original.Data[0] = 'X'
	got, ok, err := mem.Get(ctx, "k")
	if err != nil || !ok {
		t.Fatalf("Get() = (%v,%v,%v), want ok", got, ok, err)
	}
	if string(got.Data) != "hello" || !got.IsCached {
		t.Fatalf("unexpected cached result: %+v", got)
	}

	time.Sleep(60 * time.Millisecond)
	if _, ok, _ := mem.Get(ctx, "k"); ok {
		t.Fatal("expected fresh cache miss after ttl")
	}
	if stale, ok, _ := mem.GetStale(ctx, "k"); !ok || !stale.IsCached {
		t.Fatalf("expected stale hit, got ok=%v stale=%+v", ok, stale)
	}

	w := NewWeatherCache(1*time.Millisecond, 1*time.Millisecond)
	if _, ok := w.GetForecast(37.1, 127.1); ok {
		t.Fatal("expected empty forecast cache")
	}
	w.SetForecast(37.12345, 127.54321, &providers.ForecastData{CurrentTemp: 11.2})
	fc, ok := w.GetForecast(37.12345, 127.54321)
	if !ok || fc.Data == nil || fc.Stale {
		t.Fatalf("unexpected forecast cache result: %+v ok=%v", fc, ok)
	}
	w.SetAirQuality(37.12345, 127.54321, &providers.AirQualityData{PM10: 30})
	aq, ok := w.GetAirQuality(37.12345, 127.54321)
	if !ok || aq.Data == nil || aq.Data.PM10 != 30 {
		t.Fatalf("unexpected air cache result: %+v ok=%v", aq, ok)
	}
	w.SetYesterday(37.12345, 127.54321, 8.1)
	y, ok := w.GetYesterday(37.12345, 127.54321)
	if !ok || y.Data != 8.1 {
		t.Fatalf("unexpected yesterday cache result: %+v ok=%v", y, ok)
	}

	time.Sleep(2 * time.Millisecond)
	if fc, ok := w.GetForecast(37.12345, 127.54321); !ok || !fc.Stale {
		t.Fatalf("expected stale forecast, got %+v ok=%v", fc, ok)
	}
	if aq, ok := w.GetAirQuality(37.12345, 127.54321); !ok || !aq.Stale {
		t.Fatalf("expected stale air quality, got %+v ok=%v", aq, ok)
	}
	if y, ok := w.GetYesterday(37.12345, 127.54321); !ok || !y.Stale {
		t.Fatalf("expected stale yesterday, got %+v ok=%v", y, ok)
	}

	if key := locationKey(37.123456, 127.654321); key != "37.1235,127.6543" {
		t.Fatalf("locationKey = %q", key)
	}
}

func TestThemeIndexMatchAndCacheHits(t *testing.T) {
	t.Parallel()

	index := &ThemeIndex{
		naverEntries: []providers.ThemeEntry{
			{No: 10, Name: "반도체"},
			{No: 11, Name: "자동차"},
		},
		naverReady: true,
		judalEntries: []providers.JudalThemeEntry{
			{Idx: 1, Name: "금(Gold)"},
			{Idx: 2, Name: "리튬"},
		},
		judalReady: true,
		judalStockCache: map[int]judalStockCacheEntry{
			7: {codes: []string{"005930"}, fetchedAt: time.Now()},
		},
		judalStockTTL: time.Hour,
		detailCache: map[int]themeDetailCacheEntry{
			10: {detail: providers.ThemeDetail{Name: "반도체"}, fetchedAt: time.Now()},
		},
		detailTTL: time.Hour,
		logger:    slog.Default(),
	}

	if got := index.Match("   "); got != nil {
		t.Fatalf("blank keyword should return nil, got %+v", got)
	}
	if got := index.Match("금"); len(got) != 1 || got[0].Source != ThemeSourceJudal || got[0].No != 1 {
		t.Fatalf("judal exact match failed: %+v", got)
	}
	if got := index.Match("리튬"); len(got) != 1 || got[0].No != 2 {
		t.Fatalf("judal exact second match failed: %+v", got)
	}
	if got := index.Match("반도체"); len(got) != 1 || got[0].Source != ThemeSourceNaver || got[0].No != 10 {
		t.Fatalf("naver fallback match failed: %+v", got)
	}

	codes, err := index.FetchJudalStockCodes(context.Background(), 7)
	if err != nil || len(codes) != 1 || codes[0] != "005930" {
		t.Fatalf("FetchJudalStockCodes cache hit = (%v,%v)", codes, err)
	}
	detail, err := index.FetchDetail(context.Background(), 10)
	if err != nil || detail.Name != "반도체" {
		t.Fatalf("FetchDetail cache hit = (%+v,%v)", detail, err)
	}
}

func TestThemeIndexRefreshAndStaleFallbackPaths(t *testing.T) {
	t.Parallel()

	naver := providers.NewNaverStock(nil)
	judal := providers.NewJudalScraper(nil)
	index := NewThemeIndex(naver, judal, nil)
	if index.logger == nil || index.judalStockTTL <= 0 || index.detailTTL <= 0 {
		t.Fatalf("unexpected NewThemeIndex defaults: %+v", index)
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	// Canceled context forces fetch failures without waiting for network,
	// exercising refresh error paths.
	index.refreshNaver(canceledCtx)
	index.refreshJudal(canceledCtx)
	index.Start(canceledCtx)

	// When refresh fails and stale cache exists, stale data should be returned.
	index.judalStockTTL = time.Millisecond
	index.judalStockCache[101] = judalStockCacheEntry{
		codes:     []string{"000660"},
		fetchedAt: time.Now().Add(-time.Hour),
	}
	codes, err := index.FetchJudalStockCodes(canceledCtx, 101)
	if err != nil || len(codes) != 1 || codes[0] != "000660" {
		t.Fatalf("FetchJudalStockCodes stale fallback = (%v,%v)", codes, err)
	}

	index.detailTTL = time.Millisecond
	index.detailCache[202] = themeDetailCacheEntry{
		detail: providers.ThemeDetail{
			Name: "테스트",
			Stocks: []providers.ThemeStock{
				{Name: "a", MarketValue: 10},
			},
		},
		fetchedAt: time.Now().Add(-time.Hour),
	}
	detail, err := index.FetchDetail(canceledCtx, 202)
	if err != nil || detail.Name != "테스트" {
		t.Fatalf("FetchDetail stale fallback = (%+v,%v)", detail, err)
	}
}

func TestCoinHotListAndPollerIntervals(t *testing.T) {
	t.Parallel()

	cfg := DefaultCoinHotListConfig()
	if cfg.MaxRatePerMin == 0 || cfg.BatchSize == 0 || cfg.MinInterval == 0 {
		t.Fatalf("unexpected default hotlist config: %+v", cfg)
	}

	hot := NewCoinHotList(nil, cfg, nil)
	if got := hot.Get("missing"); got != nil {
		t.Fatalf("expected missing hotlist quote, got %+v", got)
	}
	hot.Register(nil)
	hot.Register(&providers.DEXQuote{})
	hot.Register(&providers.DEXQuote{ContractAddress: "0xABC", Symbol: "AAA", KRWPrice: 10})
	if got := hot.Get("0xabc"); got == nil || got.Symbol != "AAA" {
		t.Fatalf("hotlist get failed: %+v", got)
	}
	if addrs := hot.addresses(); len(addrs) != 1 || addrs[0] != "0xabc" {
		t.Fatalf("addresses = %v", addrs)
	}

	hot.mu.Lock()
	hot.entries["0xabc"].lastAccess = time.Now().Add(-cfg.IdleTimeout - time.Second)
	hot.mu.Unlock()
	hot.evictIdle()
	if got := hot.Get("0xabc"); got != nil {
		t.Fatalf("expected evicted entry, got %+v", got)
	}
	if got := hot.calcPollInterval(1); got < cfg.MinInterval {
		t.Fatalf("poll interval should not be below min interval, got %v", got)
	}
	if got := hot.calcPollInterval(10_000); got < cfg.MinInterval {
		t.Fatalf("poll interval should respect min interval, got %v", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	hot.Start(ctx)

	footballCache := NewFootballCache(time.Hour)
	esportsCache := NewEsportsCache()
	baseballCache := NewBaseballCache()
	today := "20260319"
	now := time.Now()

	footballCfg := SportsPollerConfig{
		LiveInterval:     time.Second,
		MatchDayInterval: time.Minute,
		IdleDayInterval:  time.Hour,
		PreMatchLeadTime: 30 * time.Minute,
	}
	footballPoller := &FootballPoller{
		config:  footballCfg,
		cache:   footballCache,
		leagues: []providers.FootballLeague{{ID: "epl"}},
	}
	if got := footballPoller.currentInterval(today); got != footballCfg.IdleDayInterval {
		t.Fatalf("football idle interval = %v", got)
	}
	footballCache.SetMatches("epl", today, []providers.FootballMatch{
		{ID: "m1", Status: providers.MatchScheduled, StartTime: now.Add(20 * time.Minute)},
	})
	if got := footballPoller.currentInterval(today); got != footballCfg.LiveInterval {
		t.Fatalf("football pre-match live interval = %v", got)
	}
	footballCache.SetMatches("epl", today, []providers.FootballMatch{
		{ID: "m1", Status: providers.MatchScheduled, StartTime: now.Add(2 * time.Hour)},
	})
	if got := footballPoller.currentInterval(today); got != footballCfg.MatchDayInterval {
		t.Fatalf("football match-day interval = %v", got)
	}
	footballCache.SetMatches("epl", today, []providers.FootballMatch{
		{ID: "m1", Status: providers.MatchLive},
	})
	if got := footballPoller.currentInterval(today); got != footballCfg.LiveInterval {
		t.Fatalf("football live interval = %v", got)
	}

	esportsCfg := SportsPollerConfig{
		LiveInterval:     2 * time.Second,
		MatchDayInterval: 2 * time.Minute,
		IdleDayInterval:  2 * time.Hour,
	}
	esportsPoller := &EsportsPoller{
		config:  esportsCfg,
		cache:   esportsCache,
		leagues: []providers.EsportsLeague{{ID: "lck"}},
	}
	if got := esportsPoller.currentInterval(today); got != esportsCfg.IdleDayInterval {
		t.Fatalf("esports idle interval = %v", got)
	}
	esportsCache.SetMatches("lck", today, []providers.EsportsMatch{{ID: "e1", Status: providers.MatchScheduled}})
	if got := esportsPoller.currentInterval(today); got != esportsCfg.MatchDayInterval {
		t.Fatalf("esports match-day interval = %v", got)
	}
	esportsCache.SetMatches("lck", today, []providers.EsportsMatch{{ID: "e1", Status: providers.MatchLive}})
	if got := esportsPoller.currentInterval(today); got != esportsCfg.LiveInterval {
		t.Fatalf("esports live interval = %v", got)
	}

	baseballCfg := SportsPollerConfig{
		LiveInterval:     3 * time.Second,
		MatchDayInterval: 3 * time.Minute,
		IdleDayInterval:  3 * time.Hour,
		PreMatchLeadTime: 45 * time.Minute,
	}
	baseballPoller := &BaseballPoller{
		config:  baseballCfg,
		cache:   baseballCache,
		leagues: []providers.BaseballLeague{{ID: "kbo"}},
	}
	if got := baseballPoller.currentInterval(today); got != baseballCfg.IdleDayInterval {
		t.Fatalf("baseball idle interval = %v", got)
	}
	baseballCache.SetMatches("kbo", today, []providers.BaseballMatch{
		{ID: "b1", Status: providers.BaseballScheduled, StartTime: now.Add(30 * time.Minute)},
	})
	if got := baseballPoller.currentInterval(today); got != baseballCfg.LiveInterval {
		t.Fatalf("baseball pre-match interval = %v", got)
	}
	baseballCache.SetMatches("kbo", today, []providers.BaseballMatch{
		{ID: "b1", Status: providers.BaseballScheduled, StartTime: now.Add(3 * time.Hour)},
	})
	if got := baseballPoller.currentInterval(today); got != baseballCfg.MatchDayInterval {
		t.Fatalf("baseball match-day interval = %v", got)
	}
	baseballCache.SetMatches("kbo", today, []providers.BaseballMatch{
		{ID: "b1", Status: providers.BaseballLive},
	})
	if got := baseballPoller.currentInterval(today); got != baseballCfg.LiveInterval {
		t.Fatalf("baseball live interval = %v", got)
	}

	if today := todayDateStr(); len(today) != 8 {
		t.Fatalf("todayDateStr() = %q", today)
	}
}

func TestSportsPollerStartAndConstructors(t *testing.T) {
	t.Parallel()

	cfg := SportsPollerConfig{
		LiveInterval:     5 * time.Millisecond,
		MatchDayInterval: 5 * time.Millisecond,
		IdleDayInterval:  5 * time.Millisecond,
		PreMatchLeadTime: 10 * time.Minute,
	}

	footballCache := NewFootballCache(time.Hour)
	footballCalls := 0
	footballPoller := NewFootballPoller(cfg, footballCache, func(_ context.Context, league providers.FootballLeague, _ string) error {
		if league.ID == "epl" {
			footballCalls++
		}
		return nil
	}, []providers.FootballLeague{{ID: "epl"}}, nil)
	footballCtx, footballCancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer footballCancel()
	footballPoller.Start(footballCtx)
	if footballCalls == 0 {
		t.Fatal("expected football poller to invoke pollFn at least once")
	}

	esportsCache := NewEsportsCache()
	esportsCalls := 0
	esportsPoller := NewEsportsPoller(cfg, esportsCache, func(_ context.Context, league providers.EsportsLeague, _ string) error {
		if league.ID == "lck" {
			esportsCalls++
		}
		return nil
	}, []providers.EsportsLeague{{ID: "lck"}}, nil)
	esportsCtx, esportsCancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer esportsCancel()
	esportsPoller.Start(esportsCtx)
	if esportsCalls == 0 {
		t.Fatal("expected esports poller to invoke pollFn at least once")
	}

	baseballCache := NewBaseballCache()
	baseballCalls := 0
	baseballPoller := NewBaseballPoller(cfg, baseballCache, func(_ context.Context, league providers.BaseballLeague, _ string) error {
		if league.ID == "kbo" {
			baseballCalls++
		}
		return nil
	}, []providers.BaseballLeague{{ID: "kbo"}}, nil)
	baseballCtx, baseballCancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer baseballCancel()
	baseballPoller.Start(baseballCtx)
	if baseballCalls == 0 {
		t.Fatal("expected baseball poller to invoke pollFn at least once")
	}
}
