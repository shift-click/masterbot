package command

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
)

func TestStockHandlerNetworkPaths(t *testing.T) {
	logger := slog.Default()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/stock/005930/integration"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"stockName": "삼성전자",
				"itemCode":  "005930",
				"totalInfos": []map[string]string{
					{"code": "lastClosePrice", "value": "69,000"},
					{"code": "marketValue", "value": "410조"},
					{"code": "per", "value": "12.3"},
					{"code": "pbr", "value": "1.1"},
				},
			})
		case strings.Contains(r.URL.Path, "/api/stock/005930/basic"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"stockName":                   "삼성전자",
				"closePrice":                  "70,000",
				"compareToPreviousClosePrice": "1,000",
				"fluctuationsRatio":           "1.45",
				"compareToPreviousPrice":      map[string]string{"name": "RISING"},
				"stockExchangeName":           "KOSPI",
			})
		case strings.Contains(r.URL.Path, "/api/stock/005930/finance/annual"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"financeInfo": map[string]any{
					"trTitleList": []map[string]string{{"isConsensus": "N", "key": "2025.12.31"}},
					"rowList": []map[string]any{
						{"title": "매출액", "columns": map[string]map[string]string{"2025.12.31": {"value": "100"}}},
						{"title": "영업이익", "columns": map[string]map[string]string{"2025.12.31": {"value": "50"}}},
					},
				},
			})
		case strings.Contains(r.URL.Path, "/stock/GOOGL.O/basic"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"stockName":                   "Alphabet",
				"symbolCode":                  "GOOGL",
				"stockExchangeName":           "NASDAQ",
				"closePrice":                  "305.56",
				"compareToPreviousClosePrice": "3.28",
				"fluctuationsRatio":           "1.09",
				"compareToPreviousPrice":      map[string]string{"name": "RISING"},
				"currencyType":                map[string]string{"code": "USD"},
				"stockItemTotalInfos": []map[string]string{
					{"code": "marketValue", "valueDesc": "3,034조 7,909억원"},
					{"code": "per", "value": "27.23배"},
					{"code": "pbr", "value": "8.89배"},
				},
			})
		case strings.Contains(r.URL.Path, "/stock/GOOGL.O/finance/annual"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"trTitleList": []map[string]string{{"isConsensus": "N", "key": "2025.12.31"}},
				"rowList": []map[string]any{
					{"title": "매출액", "columns": map[string]map[string]string{"2025.12.31": {"krw": "6,006,687.60"}}},
					{"title": "EBITDA", "columns": map[string]map[string]string{"2025.12.31": {"krw": "2,331,707.63"}}},
				},
			})
		case strings.Contains(r.URL.Path, "/api/stocks/theme/12"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"groupInfo": map[string]string{"name": "AI"},
				"stocks": []map[string]any{
					{
						"itemCode":                    "005930",
						"stockName":                   "삼성전자",
						"sosok":                       "0",
						"closePrice":                  "70000",
						"compareToPreviousClosePrice": "1000",
						"compareToPreviousPrice":      map[string]string{"name": "RISING"},
						"fluctuationsRatio":           "1.45",
						"marketValue":                 "1,234",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	naver := providers.NewNaverStock(logger)
	setUnexportedField(t, naver, "client", &http.Client{
		Timeout:   5 * time.Second,
		Transport: rewriteTransport{base: server.URL},
	})
	hotlist := scraper.NewHotList(func(context.Context, string) (json.RawMessage, error) {
		return nil, nil
	}, scraper.DefaultHotListConfig(), logger)
	themeIndex := scraper.NewThemeIndex(naver, nil, logger)
	setUnexportedField(t, themeIndex, "naverEntries", []providers.ThemeEntry{{No: 12, Name: "AI"}})
	setUnexportedField(t, themeIndex, "naverReady", true)

	handler := NewStockHandler(naver, hotlist, themeIndex, logger)

	// local stock fetch path
	reply := runStock(t, handler, bot.CommandContext{Command: "주식", Args: []string{"삼전"}})
	if !strings.Contains(reply.Text, "삼성전자") {
		t.Fatalf("unexpected local stock reply: %q", reply.Text)
	}

	// world stock fetch path
	reply = runStock(t, handler, bot.CommandContext{Command: "주식", Args: []string{"구글"}})
	if !strings.Contains(strings.ToUpper(reply.Text), "GOOGL") {
		t.Fatalf("unexpected world stock reply: %q", reply.Text)
	}

	// theme branch
	reply = runStock(t, handler, bot.CommandContext{Command: "주식", Args: []string{"AI", "관련주"}})
	if !strings.Contains(reply.Text, "AI") {
		t.Fatalf("unexpected theme reply: %q", reply.Text)
	}

	// fallback + quantity path
	if ok := handler.MatchAutoQueryCandidate(context.Background(), "오늘 삼전 가격"); !ok {
		t.Fatal("expected auto candidate match")
	}
	reply = runStock(t, handler, bot.CommandContext{Command: "주식", Args: []string{"삼전", "*", "2"}})
	if !strings.Contains(reply.Text, "2") {
		t.Fatalf("unexpected quantity reply: %q", reply.Text)
	}
}

func TestCoinHandlerNetworkPaths(t *testing.T) {
	logger := slog.Default()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/v3/coins/abc-coin"):
			fmt.Fprint(w, `{"symbol":"abc","name":"AlphaBeta","market_data":{"current_price":{"usd":2.5},"price_change_percentage_24h":1.2,"price_change_24h":0.1,"market_cap":{"usd":5000000}}}`)
		case strings.Contains(r.URL.Path, "/latest/dex/tokens/0xabc"):
			fmt.Fprint(w, `{"pairs":[{"chainId":"ethereum","dexId":"uniswap","pairAddress":"pair","baseToken":{"address":"0xabc","name":"DexToken","symbol":"dxt"},"priceUsd":"1.25","volume":{"h24":1000},"liquidity":{"usd":2000},"fdv":3000,"marketCap":2500,"priceChange":{"h24":-2.3}}]}`)
		case strings.Contains(r.URL.Path, "/latest/dex/tokens/0xdef"):
			fmt.Fprint(w, `{"pairs":[{"chainId":"solana","dexId":"raydium","pairAddress":"pair2","baseToken":{"address":"0xdef","name":"DexSearch","symbol":"ds"},"priceUsd":"0.75","volume":{"h24":500},"liquidity":{"usd":800},"fdv":1200,"marketCap":1000,"priceChange":{"h24":4.3}}]}`)
		case strings.Contains(strings.ToLower(r.URL.Path), "/latest/dex/tokens/0x3bd359c1119da7da1d913d1c4d2b7c461115433a"):
			fmt.Fprint(w, `{"pairs":[{"chainId":"monad","dexId":"uniswap","pairAddress":"monadpair","baseToken":{"address":"0x3bd359C1119dA7Da1D913D1C4D2B7c461115433A","name":"Monad","symbol":"MON"},"priceUsd":"0.025","volume":{"h24":1200},"liquidity":{"usd":2200},"fdv":9900000,"marketCap":9800000,"priceChange":{"h24":5.1}}]}`)
		case strings.Contains(r.URL.Path, "/latest/dex/search"):
			fmt.Fprint(w, `{"pairs":[{"chainId":"solana","dexId":"raydium","pairAddress":"pair2","baseToken":{"address":"0xdef","name":"DexSearch","symbol":"ds"},"priceUsd":"0.75","volume":{"h24":500},"liquidity":{"usd":800},"fdv":1200,"marketCap":1000,"priceChange":{"h24":4.3}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cache := scraper.NewCoinCache(logger)
	cache.UpdateForexRate(1300)
	seedBTCQuote(cache)
	cache.OnUpbitUpdate(providers.UpbitTickerUpdate{
		Symbol:     "MON",
		TradePrice: 37.3,
		PrevClose:  37.7,
		Change:     -0.4,
		ChangePct:  -0.0106100796,
	})

	coinGecko := providers.NewCoinGecko(logger)
	setUnexportedField(t, coinGecko, "client", &http.Client{
		Timeout:   5 * time.Second,
		Transport: rewriteTransport{base: server.URL},
	})
	setUnexportedField(t, coinGecko, "idList", map[string]string{"ABC": "abc-coin"})

	dex := providers.NewDexScreener(logger)
	setUnexportedField(t, dex, "client", &http.Client{
		Timeout:   5 * time.Second,
		Transport: rewriteTransport{base: server.URL},
	})

	resolver := providers.NewCoinResolver(providers.NewCoinAliases(), coinGecko, dex, logger)
	dexHotList := scraper.NewCoinHotList(dex, scraper.DefaultCoinHotListConfig(), logger)
	handler := NewCoinHandler(resolver, cache, coinGecko, dex, dexHotList, logger)

	// coingecko quantity path
	reply := runCoinCommand(t, handler, bot.CommandContext{Command: "코인", Args: []string{"abc", "*", "2"}})
	if !strings.Contains(strings.ToUpper(reply.Text), "ABC") {
		t.Fatalf("unexpected coingecko quantity reply: %q", reply.Text)
	}

	// dex quantity path
	reply = runCoinCommand(t, handler, bot.CommandContext{Command: "코인", Args: []string{"0xabc", "*", "2"}})
	if strings.Contains(reply.Text, "⚠️") || !strings.Contains(reply.Text, "× 2") {
		t.Fatalf("unexpected dex quantity reply: %q", reply.Text)
	}

	// cex path + fallback
	reply = runCoinCommand(t, handler, bot.CommandContext{Command: "코인", Args: []string{"BTC"}})
	if !strings.Contains(strings.ToUpper(reply.Text), "BTC") {
		t.Fatalf("unexpected cex reply: %q", reply.Text)
	}

	reply = runCoinCommand(t, handler, bot.CommandContext{Command: "코인", Args: []string{"monad"}})
	if !strings.Contains(strings.ToUpper(reply.Text), "MON") || !strings.Contains(reply.Text, "Monad") {
		t.Fatalf("unexpected local dex reply: %q", reply.Text)
	}
	if strings.Contains(reply.Text, "DEX 토큰") {
		t.Fatalf("monad should prefer CEX-style reply, got: %q", reply.Text)
	}

	err := handler.HandleFallback(context.Background(), bot.CommandContext{
		Message: transport.Message{Msg: "오늘 비트 가격"},
		Reply: func(_ context.Context, _ bot.Reply) error {
			return nil
		},
	})
	if err != bot.ErrHandled {
		t.Fatalf("fallback err = %v, want ErrHandled", err)
	}

	if ok := handler.MatchAutoQueryCandidate(context.Background(), "오늘 모나드 가격"); !ok {
		t.Fatal("expected local monad auto candidate match")
	}
}

func TestNewsTrendingYoutubeValidationPaths(t *testing.T) {
	t.Parallel()

	// error branch with canceled context
	news := NewNewsHandlerReal(providers.NewGoogleNews(nil))
	newsCtx, newsCancel := context.WithCancel(context.Background())
	newsCancel()
	newsReply := runSimpleCommandWithContext(t, newsCtx, news, nil, "")
	if !strings.Contains(newsReply.Text, "인기뉴스를 가져올 수 없습니다") {
		t.Fatalf("unexpected news reply: %q", newsReply.Text)
	}

	trending := NewTrendingHandler(providers.NewGoogleTrends(nil))
	trendingCtx, trendingCancel := context.WithCancel(context.Background())
	trendingCancel()
	trendingReply := runSimpleCommandWithContext(t, trendingCtx, trending, nil, "")
	if !strings.Contains(trendingReply.Text, "실시간 검색 트렌드를 가져올 수 없습니다") {
		t.Fatalf("unexpected trending reply: %q", trendingReply.Text)
	}

	yh := NewYouTubeHandler(nil, slog.Default())
	emptyReply := runSimpleCommandWithContext(t, context.Background(), yh, nil, "")
	if !strings.Contains(emptyReply.Text, "유튜브 URL") {
		t.Fatalf("unexpected youtube empty reply: %q", emptyReply.Text)
	}
	invalidReply := runSimpleCommandWithContext(t, context.Background(), yh, []string{"not-a-url"}, "")
	if !strings.Contains(invalidReply.Text, "유효한 유튜브 URL") {
		t.Fatalf("unexpected youtube invalid reply: %q", invalidReply.Text)
	}
}

func runSimpleCommandWithContext(t *testing.T, ctx context.Context, handler bot.Handler, args []string, msg string) bot.Reply {
	t.Helper()
	var reply bot.Reply
	err := handler.Execute(ctx, bot.CommandContext{
		Args: args,
		Message: transport.Message{
			Msg: msg,
			Raw: transport.RawChatLog{ChatID: "room-1"},
		},
		Reply: func(_ context.Context, r bot.Reply) error {
			reply = r
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	return reply
}

type rewriteTransport struct {
	base string
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, _ := url.Parse(t.base)
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	return http.DefaultTransport.RoundTrip(req)
}

func setUnexportedField(t *testing.T, target any, field string, value any) {
	t.Helper()

	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		t.Fatalf("target must be non-nil pointer: %T", target)
	}
	elem := v.Elem()
	f := elem.FieldByName(field)
	if !f.IsValid() {
		t.Fatalf("field %q not found in %T", field, target)
	}

	// Auto-wrap *http.Client → *BreakerHTTPClient when the field expects it.
	if httpClient, ok := value.(*http.Client); ok {
		if f.Type() == reflect.TypeOf((*providers.BreakerHTTPClient)(nil)) {
			value = providers.NewBreakerHTTPClient(httpClient, "test", nil)
		}
	}

	val := reflect.ValueOf(value)
	if !val.Type().AssignableTo(f.Type()) {
		if val.Type().ConvertibleTo(f.Type()) {
			val = val.Convert(f.Type())
		} else {
			t.Fatalf("cannot assign %s to %s for field %q", val.Type(), f.Type(), field)
		}
	}

	ptr := unsafe.Pointer(f.UnsafeAddr())
	reflect.NewAt(f.Type(), ptr).Elem().Set(val)
}
