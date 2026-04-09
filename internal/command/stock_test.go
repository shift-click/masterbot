package command

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
)

func TestStockHandlerExecuteRequiresArgs(t *testing.T) {
	h := newStockHandlerForTest(t)

	reply := runStock(t, h, bot.CommandContext{Command: "주식"})
	if !strings.Contains(reply.Text, "종목명을 입력") {
		t.Fatalf("reply = %q", reply.Text)
	}
}

func TestStockHandlerExecuteUsesHotlistSnapshot(t *testing.T) {
	h := newStockHandlerForTest(t)

	result, ok := h.naver.ResolveLocalOnly("삼전")
	if !ok {
		t.Fatal("expected local alias 삼전 to resolve")
	}
	key, isWorld := resolveStockHotlistKey(result)
	payload, err := json.Marshal(providers.StockQuote{
		Name:       "삼성전자",
		SymbolCode: "005930",
		Market:     "KOSPI",
		Price:      "70,000",
		PrevClose:  "69,000",
		Change:     "+1,000",
	})
	if err != nil {
		t.Fatalf("marshal quote: %v", err)
	}
	h.hotlist.RegisterWithMeta(key, payload, isWorld, "")

	reply := runStock(t, h, bot.CommandContext{Command: "주식", Args: []string{"삼전"}})
	if !strings.Contains(reply.Text, "삼성전자") {
		t.Fatalf("reply = %q", reply.Text)
	}
}

func TestStockHandlerMatchBareQueryLocalAlias(t *testing.T) {
	h := newStockHandlerForTest(t)

	args, ok := h.MatchBareQuery(context.Background(), "삼전")
	if !ok || len(args) != 1 {
		t.Fatalf("MatchBareQuery(삼전) = (%v, %v)", args, ok)
	}
}

func TestStockHandlerMatchBareQueryRejectsThemeShapedInput(t *testing.T) {
	h := newStockHandlerForTest(t)

	if _, ok := h.MatchBareQuery(context.Background(), "네온가스 관련주"); ok {
		t.Fatal("expected theme-shaped input to be excluded from bare stock exact matcher")
	}
}

func TestStockHandlerMatchBareQueryRejectsUnknownNumericCode(t *testing.T) {
	h := newStockHandlerForTest(t)

	if _, ok := h.MatchBareQuery(context.Background(), "123456"); ok {
		t.Fatal("expected unknown numeric code to be excluded from bare stock exact matcher")
	}
}

func TestStockHandlerMatchBareQueryAllowsBareExactKRXRemoteHit(t *testing.T) {
	h := newStockHandlerWithDeps(t, fakeStockProvider{
		bareExactResultByQuery: map[string]providers.StockSearchResult{
			"동국제약": {Code: "086450", Name: "동국제약", Market: "KOSDAQ", NationCode: "KOR"},
			"신라젠":  {Code: "215600", Name: "신라젠", Market: "KOSDAQ", NationCode: "KOR"},
			"금양":   {Code: "001570", Name: "금양", Market: "KOSPI", NationCode: "KOR"},
		},
	}, nil)

	for _, query := range []string{"동국제약", "신라젠", "금양"} {
		if _, ok := h.MatchBareQuery(context.Background(), query); !ok {
			t.Fatalf("expected bare exact remote hit for %q", query)
		}
	}
}

func TestStockHandlerMatchBareQueryRejectsRemotePrefixAndStopwords(t *testing.T) {
	h := newStockHandlerWithDeps(t, fakeStockProvider{}, nil)

	for _, query := range []string{"네", "그래", "오늘", "오키", "하이", "동국", "신라"} {
		if _, ok := h.MatchBareQuery(context.Background(), query); ok {
			t.Fatalf("expected %q to be rejected as bare remote exact stock query", query)
		}
	}
}

func TestStockHandlerMatchAutoQueryCandidateUsesLocalExactOnly(t *testing.T) {
	h := newStockHandlerForTest(t)

	if ok := h.MatchAutoQueryCandidate(context.Background(), "오늘 삼전 가격"); !ok {
		t.Fatal("expected local exact candidate for 오늘 삼전 가격")
	}
	if ok := h.MatchAutoQueryCandidate(context.Background(), "오늘 네 가격"); ok {
		t.Fatal("expected non-exact conversational candidate to be rejected")
	}
}

func TestParseStockPriceToFloat(t *testing.T) {
	if got := parseStockPriceToFloat("188,800"); got != 188800 {
		t.Fatalf("parseStockPriceToFloat(188,800) = %v", got)
	}
	if got := parseStockPriceToFloat(" 305.56 "); got != 305.56 {
		t.Fatalf("parseStockPriceToFloat(305.56) = %v", got)
	}
	if got := parseStockPriceToFloat("N/A"); got != 0 {
		t.Fatalf("parseStockPriceToFloat(N/A) = %v, want 0", got)
	}
}

func TestStockHandlerTryThemeLookupDisambiguates(t *testing.T) {
	h := newStockHandlerWithDeps(t, fakeStockProvider{}, fakeStockThemeIndex{
		matches: []scraper.ThemeMatchResult{
			{Name: "네온가스", Source: scraper.ThemeSourceNaver},
			{Name: "네온가스", Source: scraper.ThemeSourceJudal},
			{Name: "네온가스 밸류체인", Source: scraper.ThemeSourceNaver},
		},
	})

	reply := captureStockReply(t, func(ctx context.Context, cmd bot.CommandContext) error {
		if !h.tryThemeLookup(ctx, cmd, "네온가스") {
			t.Fatal("expected theme lookup to handle disambiguation")
		}
		return nil
	})
	if !strings.Contains(reply.Text, "네온가스") || !strings.Contains(reply.Text, "밸류체인") {
		t.Fatalf("unexpected disambiguation reply: %q", reply.Text)
	}
}

func TestStockHandlerTryThemeLookupJudalSuccess(t *testing.T) {
	h := newStockHandlerWithDeps(t, fakeStockProvider{
		basicByCode: map[string]providers.BasicInfo{
			"005930": {
				Name:          "삼성전자",
				Market:        "KOSPI",
				Price:         "70,000",
				Change:        "+1,000",
				ChangePercent: "+1.45",
				Direction:     "RISING",
			},
		},
	}, fakeStockThemeIndex{
		matches:    []scraper.ThemeMatchResult{{No: 7, Name: "반도체", Source: scraper.ThemeSourceJudal}},
		judalCodes: []string{"005930"},
	})

	reply := captureStockReply(t, func(ctx context.Context, cmd bot.CommandContext) error {
		if !h.tryThemeLookup(ctx, cmd, "반도체") {
			t.Fatal("expected judal theme lookup to handle match")
		}
		return nil
	})
	if !strings.Contains(reply.Text, "반도체") || !strings.Contains(reply.Text, "삼성전자") {
		t.Fatalf("unexpected theme reply: %q", reply.Text)
	}
}

func TestStockHandlerReplyLookupFetchErrorBareQuery(t *testing.T) {
	h := newStockHandlerWithDeps(t, fakeStockProvider{}, nil)

	err := h.replyLookupFetchError(context.Background(), bot.CommandContext{}, "005930", errors.New("boom"))
	if !errors.Is(err, bot.ErrHandledWithFailure) {
		t.Fatalf("replyLookupFetchError() error = %v, want ErrHandledWithFailure", err)
	}
}

func TestStockHandlerExecuteExplicitInvalidNumericCodeRepliesAndClassifiesInvalidInput(t *testing.T) {
	h := newStockHandlerWithDeps(t, fakeStockProvider{
		resolveErr: providers.ErrStockNotFound,
	}, nil)

	var reply bot.Reply
	err := h.Execute(context.Background(), bot.CommandContext{
		Command: "주식",
		Args:    []string{"123456"},
		Reply: func(_ context.Context, r bot.Reply) error {
			reply = r
			return nil
		},
	})
	if !errors.Is(err, bot.ErrHandledWithFailure) {
		t.Fatalf("Execute() error = %v, want ErrHandledWithFailure", err)
	}
	if got := reply.Text; !strings.Contains(got, "존재하지 않는 종목코드") {
		t.Fatalf("reply = %q", got)
	}
}

func TestStockHandlerExplicitNumericCodeBypassesLocalExactAndUsesRemoteResolve(t *testing.T) {
	h := newStockHandlerWithDeps(t, fakeStockProvider{
		localOnly: map[string]providers.StockSearchResult{
			"005930": {Code: "005930", Name: "삼성전자", Market: "KOSPI"},
		},
		resolveResult: providers.StockSearchResult{
			Code:   "000660",
			Name:   "SK하이닉스",
			Market: "KOSPI",
		},
		quoteByCode: map[string]providers.StockQuote{
			"000660": {
				Name:   "SK하이닉스",
				Market: "KOSPI",
				Price:  "200,000",
			},
		},
	}, nil)

	reply := runStock(t, h, bot.CommandContext{Command: "주식", Args: []string{"005930"}})
	if !strings.Contains(reply.Text, "SK하이닉스") {
		t.Fatalf("reply = %q, want remote resolve result to win", reply.Text)
	}
}

func TestStockHandlerExecuteQuantityUsesWorldQuoteCurrency(t *testing.T) {
	h := newStockHandlerWithDeps(t, fakeStockProvider{
		localOnly: map[string]providers.StockSearchResult{
			"구글": {Name: "알파벳", NationCode: "USA", ReutersCode: "GOOGL.O"},
		},
		worldQuoteByReuters: map[string]providers.StockQuote{
			"GOOGL.O": {
				Name:       "알파벳",
				SymbolCode: "GOOGL",
				Market:     "NASDAQ",
				Price:      "305.56",
				Currency:   "USD",
			},
		},
	}, nil)

	reply := runStock(t, h, bot.CommandContext{Command: "주식", Args: []string{"구글", "*", "2"}})
	if !strings.Contains(reply.Text, "GOOGL") || !strings.Contains(reply.Text, "611.12") {
		t.Fatalf("unexpected quantity reply: %q", reply.Text)
	}
}

func TestStockHandlerHandleFallbackIgnoresUnmatchedQuery(t *testing.T) {
	h := newStockHandlerWithDeps(t, fakeStockProvider{}, nil)

	err := h.HandleFallback(context.Background(), bot.CommandContext{
		Message: transport.Message{Msg: "오늘 이상한종목"},
	})
	if err != nil {
		t.Fatalf("HandleFallback() error = %v, want nil", err)
	}
}

func TestStockHandlerHandleFallbackKeepsSentenceQueryLocalOnly(t *testing.T) {
	h := newStockHandlerWithDeps(t, fakeStockProvider{
		bareExactResultByQuery: map[string]providers.StockSearchResult{
			"동국제약": {Code: "086450", Name: "동국제약", Market: "KOSDAQ", NationCode: "KOR"},
		},
	}, nil)

	err := h.HandleFallback(context.Background(), bot.CommandContext{
		Message: transport.Message{Msg: "오늘 동국제약 왜 이럼"},
	})
	if err != nil {
		t.Fatalf("HandleFallback() error = %v, want nil", err)
	}
}

func newStockHandlerForTest(t *testing.T) *StockHandler {
	t.Helper()

	logger := slog.Default()
	naver := providers.NewNaverStock(logger)
	hotlist := scraper.NewHotList(func(context.Context, string) (json.RawMessage, error) {
		return nil, nil
	}, scraper.DefaultHotListConfig(), logger)
	themeIndex := scraper.NewThemeIndex(naver, nil, logger)
	return NewStockHandler(naver, hotlist, themeIndex, logger)
}

func newStockHandlerWithDeps(t *testing.T, naver stockProvider, themeIndex stockThemeIndex) *StockHandler {
	t.Helper()

	logger := slog.Default()
	hotlist := scraper.NewHotList(func(context.Context, string) (json.RawMessage, error) {
		return nil, nil
	}, scraper.DefaultHotListConfig(), logger)
	return NewStockHandler(naver, hotlist, themeIndex, logger)
}

func runStock(t *testing.T, h *StockHandler, cmd bot.CommandContext) bot.Reply {
	t.Helper()

	var reply bot.Reply
	cmd.Reply = func(_ context.Context, r bot.Reply) error {
		reply = r
		return nil
	}
	if cmd.Message.Msg == "" {
		cmd.Message.Msg = strings.Join(cmd.Args, " ")
	}

	if err := h.Execute(context.Background(), cmd); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	return reply
}

func captureStockReply(t *testing.T, fn func(context.Context, bot.CommandContext) error) bot.Reply {
	t.Helper()

	var reply bot.Reply
	cmd := bot.CommandContext{
		Reply: func(_ context.Context, r bot.Reply) error {
			reply = r
			return nil
		},
	}
	if err := fn(context.Background(), cmd); err != nil {
		t.Fatalf("captureStockReply() error = %v", err)
	}
	return reply
}

type fakeStockProvider struct {
	localOnly              map[string]providers.StockSearchResult
	bareExactResultByQuery map[string]providers.StockSearchResult
	resolveResult          providers.StockSearchResult
	resolveErr             error
	quoteByCode            map[string]providers.StockQuote
	worldQuoteByReuters    map[string]providers.StockQuote
	basicByCode            map[string]providers.BasicInfo
}

func (f fakeStockProvider) ResolveLocalOnly(query string) (providers.StockSearchResult, bool) {
	result, ok := f.localOnly[query]
	return result, ok
}

func (f fakeStockProvider) ResolveBareKRXExact(_ context.Context, query string) (providers.StockSearchResult, error) {
	result, ok := f.bareExactResultByQuery[query]
	if !ok {
		return providers.StockSearchResult{}, providers.ErrStockNotFound
	}
	return result, nil
}

func (f fakeStockProvider) Resolve(context.Context, string) (providers.StockSearchResult, error) {
	if f.resolveErr != nil {
		return providers.StockSearchResult{}, f.resolveErr
	}
	if f.resolveResult == (providers.StockSearchResult{}) {
		return providers.StockSearchResult{}, errors.New("no remote result")
	}
	return f.resolveResult, nil
}

func (f fakeStockProvider) FetchWorldQuote(_ context.Context, reutersCode string) (providers.StockQuote, error) {
	quote, ok := f.worldQuoteByReuters[reutersCode]
	if !ok {
		return providers.StockQuote{}, errors.New("world quote not found")
	}
	return quote, nil
}

func (f fakeStockProvider) FetchQuote(_ context.Context, code string) (providers.StockQuote, error) {
	quote, ok := f.quoteByCode[code]
	if !ok {
		return providers.StockQuote{}, errors.New("quote not found")
	}
	return quote, nil
}

func (f fakeStockProvider) FetchBasicPublic(_ context.Context, code string) (providers.BasicInfo, error) {
	info, ok := f.basicByCode[code]
	if !ok {
		return providers.BasicInfo{}, errors.New("basic info not found")
	}
	return info, nil
}

type fakeStockThemeIndex struct {
	matches    []scraper.ThemeMatchResult
	judalCodes []string
	detail     providers.ThemeDetail
}

func (f fakeStockThemeIndex) Match(string) []scraper.ThemeMatchResult {
	return f.matches
}

func (f fakeStockThemeIndex) FetchJudalStockCodes(context.Context, int) ([]string, error) {
	return f.judalCodes, nil
}

func (f fakeStockThemeIndex) FetchDetail(context.Context, int) (providers.ThemeDetail, error) {
	if f.detail.Name == "" && len(f.detail.Stocks) == 0 {
		return providers.ThemeDetail{}, errors.New("theme detail not found")
	}
	return f.detail, nil
}
