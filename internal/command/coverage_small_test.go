package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
)

func TestDescriptorSupportDescriptor(t *testing.T) {
	t.Parallel()

	ds := newDescriptorSupport("coin")
	d := ds.Descriptor()
	if d.ID != "coin" || ds.Name() == "" || ds.Description() == "" {
		t.Fatalf("unexpected descriptor: %+v", d)
	}
	if len(ds.Aliases()) == 0 {
		t.Fatal("expected aliases")
	}
}

func TestCalcHandlerFallbackPaths(t *testing.T) {
	t.Parallel()

	h := NewCalcHandler()

	run := func(msg string) error {
		return h.HandleFallback(context.Background(), bot.CommandContext{
			Message: transport.Message{Msg: msg},
			Reply: func(context.Context, bot.Reply) error {
				return nil
			},
		})
	}

	if err := run(""); err != nil {
		t.Fatalf("empty msg err = %v", err)
	}
	if err := run("abc+1"); err != nil {
		t.Fatalf("letter msg err = %v", err)
	}
	if err := run("005930"); err != nil {
		t.Fatalf("stock code msg err = %v", err)
	}
	if err := run("1 + 2 * 3"); err != bot.ErrHandled {
		t.Fatalf("expression msg err = %v, want ErrHandled", err)
	}
}

func TestCoinAndStockSmallBranches(t *testing.T) {
	t.Parallel()

	aliases := providers.NewCoinAliases()
	resolver := providers.NewCoinResolver(aliases, providers.NewCoinGecko(nil), nil, slog.Default())
	coin := NewCoinHandler(resolver, scraper.NewCoinCache(slog.Default()), nil, nil, nil, slog.Default())
	if coin.SupportsSlashCommands() {
		t.Fatal("coin handler should not support slash commands")
	}
	if ok := coin.MatchAutoQueryCandidate(context.Background(), "비트"); !ok {
		t.Fatal("expected coin auto-query candidate match")
	}

	naver := providers.NewNaverStock(slog.Default())
	hotlist := scraper.NewHotList(func(context.Context, string) (json.RawMessage, error) {
		return nil, nil
	}, scraper.DefaultHotListConfig(), slog.Default())
	stock := NewStockHandler(naver, hotlist, nil, slog.Default())
	if stock.SupportsSlashCommands() {
		t.Fatal("stock handler should not support slash commands")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := stock.HandleFallback(ctx, bot.CommandContext{Message: transport.Message{Msg: "삼전"}})
	if !errors.Is(err, bot.ErrHandledWithFailure) {
		t.Fatalf("fallback err = %v, want ErrHandledWithFailure", err)
	}

	chart := NewChartHandler(ChartHandlerDeps{
		CoinResolver:  resolver,
		StockResolver: &stubStockResolver{},
		BinanceOHLC:   &stubCoinOHLC{},
		UpbitOHLC:     &stubCoinOHLC{},
		StockOHLC:     &stubStockOHLC{},
		RendererURL:   "",
		Logger:        slog.Default(),
	})
	if chart.SupportsSlashCommands() {
		t.Fatal("chart handler should not support slash commands")
	}
}

func TestWeatherHelpersAndBareMatch(t *testing.T) {
	t.Parallel()

	h := NewWeatherCommand(nil, scraper.NewWeatherCache(time.Hour, time.Hour), slog.Default())

	if _, ok := h.MatchBareQuery(context.Background(), "서울 날씨"); !ok {
		t.Fatal("expected weather bare match")
	}
	if _, ok := h.MatchBareQuery(context.Background(), "없는도시 날씨"); ok {
		t.Fatal("unexpected bare match for unknown city")
	}

	cities := []Location{{Lat: 37.5665, Lon: 126.9780}, {Lat: 35.1796, Lon: 129.0756}}
	lats, lons := cityCoordinates(cities)
	if len(lats) != 2 || len(lons) != 2 {
		t.Fatalf("unexpected coords len: %d %d", len(lats), len(lons))
	}

	h.cache.SetForecast(cities[0].Lat, cities[0].Lon, &providers.ForecastData{CurrentTemp: 10})
	results := []*providers.ForecastData{nil, nil}
	merged, hasAny := h.mergeStaleForecasts(cities, results)
	if !hasAny || merged[0] == nil {
		t.Fatalf("unexpected merged stale results: %+v hasAny=%v", merged, hasAny)
	}

	fetched := []*providers.ForecastData{{CurrentTemp: 11}, {CurrentTemp: 22}}
	h.applyFetchedForecasts(cities, merged, fetched)
	if merged[1] == nil || merged[1].CurrentTemp != 22 {
		t.Fatalf("unexpected fetched merge: %+v", merged)
	}
}

func TestNewsAndTrendingExecuteSuccess(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/trending/rss"):
			fmt.Fprint(w, `<?xml version="1.0"?><rss><channel><item><title>T1</title><approx_traffic>100+</approx_traffic></item><item><title>T2</title><approx_traffic>90+</approx_traffic></item></channel></rss>`)
		case strings.Contains(r.URL.Host, "news.google.com") || strings.Contains(r.URL.Path, "/rss"):
			fmt.Fprint(w, `<?xml version="1.0"?><rss><channel><item><title>N1</title><link>u1</link><source>s1</source></item><item><title>N2</title><link>u2</link><source>s2</source></item></channel></rss>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	newsProvider := providers.NewGoogleNews(nil)
	setUnexportedField(t, newsProvider, "client", srv.Client())
	newsClient := &http.Client{Timeout: 5 * time.Second, Transport: rewriteTransport{base: srv.URL}}
	setUnexportedField(t, newsProvider, "client", newsClient)

	trendingProvider := providers.NewGoogleTrends(nil)
	setUnexportedField(t, trendingProvider, "client", newsClient)

	news := NewNewsHandlerReal(newsProvider)
	trend := NewTrendingHandler(trendingProvider)

	var newsReply, trendReply bot.Reply
	if err := news.Execute(context.Background(), bot.CommandContext{Reply: func(_ context.Context, r bot.Reply) error {
		newsReply = r
		return nil
	}}); err != nil {
		t.Fatalf("news execute: %v", err)
	}
	if !strings.Contains(newsReply.Text, "N1") {
		t.Fatalf("unexpected news reply: %q", newsReply.Text)
	}

	if err := trend.Execute(context.Background(), bot.CommandContext{Reply: func(_ context.Context, r bot.Reply) error {
		trendReply = r
		return nil
	}}); err != nil {
		t.Fatalf("trend execute: %v", err)
	}
	if !strings.Contains(trendReply.Text, "T1") {
		t.Fatalf("unexpected trend reply: %q", trendReply.Text)
	}
}
