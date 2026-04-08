package command

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/pkg/chart"
)

// --- stubs ---

type stubCoinResolver struct {
	results map[string]providers.CoinSearchResult
}

func (s *stubCoinResolver) Resolve(_ context.Context, input string) (providers.CoinSearchResult, bool) {
	r, ok := s.results[input]
	return r, ok
}

func (s *stubCoinResolver) ResolveLocalOnly(input string) (providers.CoinSearchResult, bool) {
	return s.Resolve(context.Background(), input)
}

type stubStockResolver struct {
	results map[string]providers.StockSearchResult
}

func (s *stubStockResolver) Resolve(_ context.Context, input string) (providers.StockSearchResult, error) {
	r, ok := s.results[input]
	if !ok {
		return providers.StockSearchResult{}, &resolveError{input}
	}
	return r, nil
}

func (s *stubStockResolver) ResolveLocalOnly(input string) (providers.StockSearchResult, bool) {
	r, ok := s.results[input]
	return r, ok
}

func (s *stubStockResolver) ResolveBareKRXExact(_ context.Context, input string) (providers.StockSearchResult, error) {
	return s.Resolve(context.Background(), input)
}

type resolveError struct{ query string }

func (e *resolveError) Error() string { return "not found: " + e.query }

type stubCoinOHLC struct {
	data          providers.OHLCData
	err           error
	lastTimeframe providers.Timeframe
}

func (s *stubCoinOHLC) Fetch(_ context.Context, _ string, tf providers.Timeframe) (providers.OHLCData, error) {
	s.lastTimeframe = tf
	return s.data, s.err
}

type stubStockOHLC struct {
	domestic providers.OHLCData
	world    providers.OHLCData
	err      error
	lastTF   providers.Timeframe
}

func (s *stubStockOHLC) FetchDomestic(_ context.Context, _ string, tf providers.Timeframe) (providers.OHLCData, error) {
	s.lastTF = tf
	return s.domestic, s.err
}

func (s *stubStockOHLC) FetchWorld(_ context.Context, _ string, tf providers.Timeframe) (providers.OHLCData, error) {
	s.lastTF = tf
	return s.world, s.err
}

type stubDEXOHLC struct {
	data providers.OHLCData
	err  error
}

func (s *stubDEXOHLC) Fetch(_ context.Context, _ string, _ string, _ providers.Timeframe) (providers.OHLCData, error) {
	return s.data, s.err
}

func makeOHLCData(symbol string, prices ...float64) providers.OHLCData {
	points := make([]providers.OHLCPoint, len(prices))
	for i, p := range prices {
		points[i] = providers.OHLCPoint{Close: p, Open: p, High: p, Low: p}
	}
	return providers.OHLCData{Symbol: symbol, Points: points}
}

func newTestChartHandler(
	coinResolver chartCoinResolver,
	stockResolver chartStockResolver,
	binanceOHLC chartCoinOHLC,
	upbitOHLC chartCoinOHLC,
	stockOHLC chartStockOHLC,
	dexOHLC chartDEXOHLC,
	rendererURL string,
	logger *slog.Logger,
) *ChartHandler {
	return NewChartHandler(ChartHandlerDeps{
		CoinResolver:  coinResolver,
		StockResolver: stockResolver,
		BinanceOHLC:   binanceOHLC,
		UpbitOHLC:     upbitOHLC,
		StockOHLC:     stockOHLC,
		DEXOHLC:       dexOHLC,
		RendererURL:   rendererURL,
		Logger:        logger,
	})
}

// --- tests ---

func TestChartHandler_Execute_Coin(t *testing.T) {
	t.Parallel()

	binanceOHLC := &stubCoinOHLC{data: makeOHLCData("BTC", 60000, 61000, 62000, 63000, 64000)}
	h := newTestChartHandler(
		&stubCoinResolver{results: map[string]providers.CoinSearchResult{
			"비트코인": {Symbol: "BTC", Name: "비트코인", Tier: providers.CoinTierCEX},
		}},
		&stubStockResolver{results: map[string]providers.StockSearchResult{}},
		binanceOHLC,
		&stubCoinOHLC{},
		&stubStockOHLC{},
		nil,
		"",
		nil,
	)

	var replies []bot.Reply
	cmd := bot.CommandContext{
		Message: transport.Message{Raw: transport.RawChatLog{}},
		Args:    []string{"비트코인"},
		Reply: func(_ context.Context, r bot.Reply) error {
			replies = append(replies, r)
			return nil
		},
	}

	if err := h.Execute(context.Background(), cmd); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(replies) != 1 {
		t.Fatalf("got %d replies, want 1 (image only)", len(replies))
	}
	if replies[0].Type != transport.ReplyTypeImage {
		t.Errorf("reply type = %s, want image", replies[0].Type)
	}
	if replies[0].ImageBase64 == "" {
		t.Error("image reply has empty base64")
	}
	if binanceOHLC.lastTimeframe != providers.Timeframe1W {
		t.Fatalf("default coin timeframe = %s, want %s", binanceOHLC.lastTimeframe, providers.Timeframe1W)
	}
}

func TestChartHandler_Execute_Stock(t *testing.T) {
	t.Parallel()

	stockOHLC := &stubStockOHLC{domestic: makeOHLCData("005930", 70000, 71000, 72000, 73000, 72500)}
	h := newTestChartHandler(
		&stubCoinResolver{results: map[string]providers.CoinSearchResult{}},
		&stubStockResolver{results: map[string]providers.StockSearchResult{
			"삼성전자": {Code: "005930", Name: "삼성전자", Market: "KOSPI", NationCode: "KOR"},
		}},
		&stubCoinOHLC{},
		&stubCoinOHLC{},
		stockOHLC,
		nil,
		"",
		nil,
	)

	var replies []bot.Reply
	cmd := bot.CommandContext{
		Message: transport.Message{Raw: transport.RawChatLog{}},
		Args:    []string{"삼성전자", "1달"},
		Reply: func(_ context.Context, r bot.Reply) error {
			replies = append(replies, r)
			return nil
		},
	}

	if err := h.Execute(context.Background(), cmd); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(replies) != 1 {
		t.Fatalf("got %d replies, want 1", len(replies))
	}
	if replies[0].Type != transport.ReplyTypeImage {
		t.Errorf("reply type = %s, want image", replies[0].Type)
	}
	if stockOHLC.lastTF != providers.Timeframe1M {
		t.Fatalf("explicit stock timeframe = %s, want %s", stockOHLC.lastTF, providers.Timeframe1M)
	}
}

func TestChartHandler_Execute_Stock_DefaultTimeframe(t *testing.T) {
	t.Parallel()

	stockOHLC := &stubStockOHLC{domestic: makeOHLCData("005930", 70000, 71000, 72000, 73000, 72500)}
	h := newTestChartHandler(
		&stubCoinResolver{results: map[string]providers.CoinSearchResult{}},
		&stubStockResolver{results: map[string]providers.StockSearchResult{
			"삼성전자": {Code: "005930", Name: "삼성전자", Market: "KOSPI", NationCode: "KOR"},
		}},
		&stubCoinOHLC{},
		&stubCoinOHLC{},
		stockOHLC,
		nil,
		"",
		nil,
	)

	cmd := bot.CommandContext{
		Message: transport.Message{Raw: transport.RawChatLog{}},
		Args:    []string{"삼성전자"},
		Reply: func(_ context.Context, _ bot.Reply) error {
			return nil
		},
	}

	if err := h.Execute(context.Background(), cmd); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if stockOHLC.lastTF != providers.Timeframe3M {
		t.Fatalf("default domestic stock timeframe = %s, want %s", stockOHLC.lastTF, providers.Timeframe3M)
	}
}

func TestChartHandler_Execute_WorldStock(t *testing.T) {
	t.Parallel()

	stockOHLC := &stubStockOHLC{world: makeOHLCData("TSLA.O", 250, 255, 260, 258, 262)}
	h := newTestChartHandler(
		&stubCoinResolver{results: map[string]providers.CoinSearchResult{}},
		&stubStockResolver{results: map[string]providers.StockSearchResult{
			"테슬라": {Code: "TSLA", Name: "테슬라", NationCode: "USA", ReutersCode: "TSLA.O"},
		}},
		&stubCoinOHLC{},
		&stubCoinOHLC{},
		stockOHLC,
		nil,
		"",
		nil,
	)

	var replies []bot.Reply
	cmd := bot.CommandContext{
		Message: transport.Message{Raw: transport.RawChatLog{}},
		Args:    []string{"테슬라"},
		Reply: func(_ context.Context, r bot.Reply) error {
			replies = append(replies, r)
			return nil
		},
	}

	if err := h.Execute(context.Background(), cmd); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(replies) != 1 {
		t.Fatalf("got %d replies, want 1", len(replies))
	}
	if replies[0].Type != transport.ReplyTypeImage {
		t.Errorf("reply type = %s, want image", replies[0].Type)
	}
	if stockOHLC.lastTF != providers.Timeframe3M {
		t.Fatalf("default world stock timeframe = %s, want %s", stockOHLC.lastTF, providers.Timeframe3M)
	}
}

func TestChartHandler_Execute_UpbitOnlyCoin(t *testing.T) {
	t.Parallel()

	h := newTestChartHandler(
		&stubCoinResolver{results: map[string]providers.CoinSearchResult{
			"monad": {
				Symbol:              "MON",
				Name:                "Monad",
				Tier:                providers.CoinTierDEX,
				ContractAddress:     "0xmon",
				PairAddress:         "monadpair",
				ChainID:             "monad",
				UpbitMarket:         "KRW-MON",
				PreferredChartVenue: providers.CoinChartVenueUpbit,
			},
		}},
		&stubStockResolver{results: map[string]providers.StockSearchResult{}},
		&stubCoinOHLC{},
		&stubCoinOHLC{data: makeOHLCData("MON", 30, 31, 32, 33, 34)},
		&stubStockOHLC{},
		&stubDEXOHLC{data: makeOHLCData("monadpair", 0.02, 0.021, 0.022, 0.024, 0.025)},
		"",
		nil,
	)

	var replies []bot.Reply
	cmd := bot.CommandContext{
		Message: transport.Message{Raw: transport.RawChatLog{}},
		Args:    []string{"monad"},
		Reply: func(_ context.Context, r bot.Reply) error {
			replies = append(replies, r)
			return nil
		},
	}

	if err := h.Execute(context.Background(), cmd); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(replies) != 1 {
		t.Fatalf("got %d replies, want 1", len(replies))
	}
	if replies[0].Type != transport.ReplyTypeImage {
		t.Errorf("reply type = %s, want image", replies[0].Type)
	}
}

func TestChartHandler_Execute_DEX(t *testing.T) {
	t.Parallel()

	h := newTestChartHandler(
		&stubCoinResolver{results: map[string]providers.CoinSearchResult{
			"monad": {Symbol: "MON", Name: "Monad", Tier: providers.CoinTierDEX, ChainID: "monad", PairAddress: "monadpair"},
		}},
		&stubStockResolver{results: map[string]providers.StockSearchResult{}},
		&stubCoinOHLC{},
		&stubCoinOHLC{},
		&stubStockOHLC{},
		&stubDEXOHLC{data: makeOHLCData("monadpair", 0.02, 0.021, 0.022, 0.024, 0.025)},
		"",
		nil,
	)

	var replies []bot.Reply
	cmd := bot.CommandContext{
		Message: transport.Message{Raw: transport.RawChatLog{}},
		Args:    []string{"monad"},
		Reply: func(_ context.Context, r bot.Reply) error {
			replies = append(replies, r)
			return nil
		},
	}

	if err := h.Execute(context.Background(), cmd); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(replies) != 1 {
		t.Fatalf("got %d replies, want 1", len(replies))
	}
	if replies[0].Type != transport.ReplyTypeImage {
		t.Errorf("reply type = %s, want image", replies[0].Type)
	}
}

func TestChartHandler_Execute_EmptyArgs(t *testing.T) {
	t.Parallel()

	h := newTestChartHandler(
		&stubCoinResolver{results: map[string]providers.CoinSearchResult{}},
		&stubStockResolver{results: map[string]providers.StockSearchResult{}},
		&stubCoinOHLC{},
		&stubCoinOHLC{},
		&stubStockOHLC{},
		nil,
		"",
		nil,
	)

	var replied bot.Reply
	cmd := bot.CommandContext{
		Message: transport.Message{Raw: transport.RawChatLog{}},
		Args:    nil,
		Reply: func(_ context.Context, r bot.Reply) error {
			replied = r
			return nil
		},
	}

	if err := h.Execute(context.Background(), cmd); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if replied.Type != transport.ReplyTypeText {
		t.Errorf("reply type = %s, want text", replied.Type)
	}
}

func TestChartHandler_Execute_Unresolvable(t *testing.T) {
	t.Parallel()

	h := newTestChartHandler(
		&stubCoinResolver{results: map[string]providers.CoinSearchResult{}},
		&stubStockResolver{results: map[string]providers.StockSearchResult{}},
		&stubCoinOHLC{},
		&stubCoinOHLC{},
		&stubStockOHLC{},
		nil,
		"",
		nil,
	)

	var replied bot.Reply
	cmd := bot.CommandContext{
		Message: transport.Message{Raw: transport.RawChatLog{}},
		Args:    []string{"존재하지않는자산"},
		Reply: func(_ context.Context, r bot.Reply) error {
			replied = r
			return nil
		},
	}

	if err := h.Execute(context.Background(), cmd); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if replied.Type != transport.ReplyTypeText {
		t.Errorf("reply type = %s, want text", replied.Type)
	}
}

func TestParseChartInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input     string
		wantQuery string
		wantTF    providers.Timeframe
	}{
		{"비트코인 차트", "비트코인", ""},
		{"차트 삼성전자", "삼성전자", ""},
		{"비트코인 차트 1달", "비트코인", providers.Timeframe1M},
		{"차트 삼전 1년", "삼전", providers.Timeframe1Y},
		{"비트코인", "", ""}, // no chart keyword
		{"", "", ""},     // empty
		{"차트", "", ""},   // chart keyword only
		{"차트도 생각해보니 이미지 아닌가", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			gotQuery, gotTF := parseChartInput(tt.input)
			if gotQuery != tt.wantQuery {
				t.Errorf("query = %q, want %q", gotQuery, tt.wantQuery)
			}
			if gotTF != tt.wantTF {
				t.Errorf("tf = %q, want %q", gotTF, tt.wantTF)
			}
		})
	}
}

func TestFormatChartPrice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		price float64
		want  string
	}{
		{name: "large integer", price: 12345, want: "12,345.00"},
		{name: "decimal", price: 12345.678, want: "12,345.68"},
		{name: "small decimal", price: 0.1234, want: "0.12"},
		{name: "negative", price: -9876.5, want: "-9,876.50"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := formatChartPrice(tt.price); got != tt.want {
				t.Fatalf("formatChartPrice(%v) = %q, want %q", tt.price, got, tt.want)
			}
		})
	}
}

func TestParseChartArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		args      []string
		wantQuery string
		wantTF    providers.Timeframe
	}{
		{[]string{"비트코인"}, "비트코인", ""},
		{[]string{"비트코인", "1주"}, "비트코인", providers.Timeframe1W},
		{[]string{"삼성전자", "3개월"}, "삼성전자", providers.Timeframe3M},
		{nil, "", ""},
	}

	for _, tt := range tests {
		name := "nil"
		if tt.args != nil {
			name = tt.wantQuery
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			gotQuery, gotTF := parseChartArgs(tt.args)
			if gotQuery != tt.wantQuery {
				t.Errorf("query = %q, want %q", gotQuery, tt.wantQuery)
			}
			if gotTF != tt.wantTF {
				t.Errorf("tf = %q, want %q", gotTF, tt.wantTF)
			}
		})
	}
}

func TestChartHandler_MatchBareQuery(t *testing.T) {
	t.Parallel()

	h := newTestChartHandler(
		&stubCoinResolver{results: map[string]providers.CoinSearchResult{
			"비트코인": {Symbol: "BTC", Name: "비트코인", Tier: providers.CoinTierCEX, BinanceSymbol: "BTCUSDT"},
		}},
		&stubStockResolver{results: map[string]providers.StockSearchResult{
			"삼전": {Code: "005930", Name: "삼성전자"},
		}},
		&stubCoinOHLC{},
		&stubCoinOHLC{},
		&stubStockOHLC{},
		nil,
		"",
		nil,
	)

	tests := []struct {
		input string
		match bool
	}{
		{"비트코인 차트", true},
		{"차트 삼전", true},
		{"비트코인 차트 1주", true},
		{"없는자산 차트", false},
		{"차트도 생각해보니 이미지 아닌가", false},
		{"비트코인", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			_, matched := h.MatchBareQuery(context.Background(), tt.input)
			if matched != tt.match {
				t.Errorf("MatchBareQuery(%q) = %v, want %v", tt.input, matched, tt.match)
			}
		})
	}
}

func TestChartHandler_HandleFallback_UnresolvedImplicitSkips(t *testing.T) {
	t.Parallel()

	h := newTestChartHandler(
		&stubCoinResolver{results: map[string]providers.CoinSearchResult{}},
		&stubStockResolver{results: map[string]providers.StockSearchResult{}},
		&stubCoinOHLC{},
		&stubCoinOHLC{},
		&stubStockOHLC{},
		nil,
		"",
		nil,
	)

	err := h.HandleFallback(context.Background(), bot.CommandContext{
		Message: transport.Message{Msg: "없는자산 차트"},
		Reply: func(_ context.Context, _ bot.Reply) error {
			t.Fatal("unresolved implicit chart should not reply")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("HandleFallback() = %v, want nil", err)
	}
}

func TestChartHandler_ImageCaching(t *testing.T) {
	t.Parallel()

	callCount := 0
	coinOHLC := &stubCoinOHLC{data: makeOHLCData("BTC", 60000, 61000, 62000, 63000, 64000)}

	h := newTestChartHandler(
		&stubCoinResolver{results: map[string]providers.CoinSearchResult{
			"비트코인": {Symbol: "BTC", Name: "비트코인", Tier: providers.CoinTierCEX},
		}},
		&stubStockResolver{results: map[string]providers.StockSearchResult{}},
		coinOHLC,
		&stubCoinOHLC{},
		&stubStockOHLC{},
		nil,
		"",
		nil,
	)

	makeCmd := func() bot.CommandContext {
		return bot.CommandContext{
			Message: transport.Message{Raw: transport.RawChatLog{}},
			Args:    []string{"비트코인"},
			Reply: func(_ context.Context, _ bot.Reply) error {
				callCount++
				return nil
			},
		}
	}

	// First call — should generate image
	if err := h.Execute(context.Background(), makeCmd()); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	firstCallCount := callCount

	// Second call — should use cached image
	if err := h.Execute(context.Background(), makeCmd()); err != nil {
		t.Fatalf("second Execute: %v", err)
	}

	// Both calls should produce 1 reply each (image only)
	if firstCallCount != 1 {
		t.Errorf("first call replies = %d, want 1", firstCallCount)
	}
	if callCount != 2 {
		t.Errorf("total replies = %d, want 2", callCount)
	}
}

func TestNewChartHandler_DefaultDeps(t *testing.T) {
	t.Parallel()

	handler := NewChartHandler(ChartHandlerDeps{})
	if handler.logger == nil {
		t.Fatal("logger should be initialized")
	}
	if handler.httpClient == nil {
		t.Fatal("httpClient should be initialized")
	}
	if handler.imgCache == nil {
		t.Fatal("imgCache should be initialized")
	}
}

func TestChartHelperFunctions(t *testing.T) {
	t.Parallel()

	data := makeOHLCData("BTC", 100, 110, 90)
	data.Points[0].Time = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	data.Points[1].Time = time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC)
	data.Points[2].Time = time.Date(2026, 4, 3, 0, 0, 0, 0, time.UTC)
	data.Points[0].Volume = 10
	data.Points[1].Volume = 0
	data.Points[2].Volume = 12

	prices, timestamps, volumes, candles, lowest, highest, hasVolume := buildChartSeries(data)
	if len(prices) != 3 || len(timestamps) != 3 || len(volumes) != 3 || len(candles) != 3 {
		t.Fatalf("unexpected series lengths: %d %d %d %d", len(prices), len(timestamps), len(volumes), len(candles))
	}
	if lowest != 90 || highest != 110 || !hasVolume {
		t.Fatalf("unexpected extrema/volume: lowest=%d highest=%d hasVolume=%v", lowest, highest, hasVolume)
	}
	if got := buildChartHeader("비트코인", "BTC"); got != "비트코인 (BTC)" {
		t.Fatalf("buildChartHeader = %q", got)
	}
	if got := buildChartHeader("BTC", "BTC"); got != "BTC" {
		t.Fatalf("buildChartHeader same symbol = %q", got)
	}
	if got := resolveChartDirection(110, 100); got != chart.DirectionUp {
		t.Fatalf("resolveChartDirection up = %q", got)
	}
	if got := resolveChartDirection(90, 100); got != chart.DirectionDown {
		t.Fatalf("resolveChartDirection down = %q", got)
	}
	if got := resolveChartDirection(100, 100); got != "" {
		t.Fatalf("resolveChartDirection flat = %q", got)
	}
	if got := chartAssetTypeLabel(assetCoin); got != "coin" {
		t.Fatalf("chartAssetTypeLabel coin = %q", got)
	}
	if got := chartAssetTypeLabel(assetDomestic); got != "stock" {
		t.Fatalf("chartAssetTypeLabel stock = %q", got)
	}
	if got := buildChartSubtitle(110, 100); got != "110.00 (+10.00%)" {
		t.Fatalf("buildChartSubtitle gain = %q", got)
	}
	if got := buildChartSubtitle(90, 100); got != "90.00 (-10.00%)" {
		t.Fatalf("buildChartSubtitle loss = %q", got)
	}
}

func TestChartParseHelpers(t *testing.T) {
	t.Parallel()

	query, tf, shape, ok := parseChartPrefixedInput([]string{"차트", "비트코인", "1주"})
	if !ok || query != "비트코인" || tf != providers.Timeframe1W || shape != chartCandidateShapePrefix {
		t.Fatalf("parseChartPrefixedInput = %q %q %q %v", query, tf, shape, ok)
	}
	query, tf, shape, ok = parseChartSuffixedInput([]string{"비트코인", "차트", "1일"})
	if !ok || query != "비트코인" || tf != providers.Timeframe1D || shape != chartCandidateShapeSuffix {
		t.Fatalf("parseChartSuffixedInput = %q %q %q %v", query, tf, shape, ok)
	}
	query, tf, shape = joinChartQuery([]string{"삼전"}, providers.Timeframe1M, chartCandidateShapeSuffix)
	if query != "삼전" || tf != providers.Timeframe1M || shape != chartCandidateShapeSuffix {
		t.Fatalf("joinChartQuery = %q %q %q", query, tf, shape)
	}
	if query, tf, shape = joinChartQuery(nil, providers.Timeframe1M, chartCandidateShapeSuffix); query != "" || tf != "" || shape != chartCandidateShapeKeyword {
		t.Fatalf("joinChartQuery empty = %q %q %q", query, tf, shape)
	}
	if !hasChartKeywordSubstring([]string{"차트도", "아닌가"}) {
		t.Fatal("expected substring detection")
	}
	if hasChartKeywordSubstring([]string{"비트코인", "가격"}) {
		t.Fatal("unexpected substring detection")
	}
}

func TestSidecarHelpers(t *testing.T) {
	t.Parallel()

	points := make([]providers.OHLCPoint, 20)
	for i := range points {
		points[i] = providers.OHLCPoint{
			Time:   time.Date(2026, 4, i+1, 0, 0, 0, 0, time.UTC),
			Open:   float64(100 + i),
			High:   float64(110 + i),
			Low:    float64(90 + i),
			Close:  float64(101 + i),
			Volume: float64(1000 + i),
		}
	}
	ohlc := providers.OHLCData{Symbol: "BTC", Points: points}

	candles, volumes := buildSidecarSeries(ohlc)
	if len(candles) != len(points) || len(volumes) != len(points) {
		t.Fatalf("unexpected sidecar lengths: %d %d", len(candles), len(volumes))
	}
	if candles[0].Time != "2026-04-01" {
		t.Fatalf("unexpected candle time: %q", candles[0].Time)
	}
	if volumes[0].Color != "#26A69A80" {
		t.Fatalf("unexpected volume color: %q", volumes[0].Color)
	}
	bearish := providers.OHLCPoint{Open: 10, Close: 9}
	if got := renderVolumeColor(bearish); got != "#EF535080" {
		t.Fatalf("renderVolumeColor bearish = %q", got)
	}

	sma := buildSidecarSMA(ohlc)
	if len(sma) != 1 {
		t.Fatalf("unexpected sma len: %d", len(sma))
	}
	if sma[0].Time != "2026-04-20" {
		t.Fatalf("unexpected sma time: %q", sma[0].Time)
	}

	subtitle, color := buildSidecarSubtitle(ohlc)
	if subtitle == "" || color != "#26A69A" {
		t.Fatalf("unexpected subtitle/color: %q %q", subtitle, color)
	}

	points[19].Close = 80
	ohlc.Points = points
	_, color = buildSidecarSubtitle(ohlc)
	if color != "#EF5350" {
		t.Fatalf("unexpected bearish subtitle color: %q", color)
	}

	chartCandles := toChartCandles(points)
	if len(chartCandles) != len(points) {
		t.Fatalf("unexpected converted candles len: %d", len(chartCandles))
	}

	markers := buildSidecarMarkers(ohlc, providers.Timeframe3M, assetCoin)
	for _, marker := range markers {
		if marker.Position != "aboveBar" && marker.Position != "belowBar" {
			t.Fatalf("unexpected marker position: %q", marker.Position)
		}
	}
	if got := resolveMarkerPosition(chart.Pinpoint{IsHigh: true}); got != "aboveBar" {
		t.Fatalf("resolveMarkerPosition high = %q", got)
	}
	if got := resolveMarkerPosition(chart.Pinpoint{}); got != "belowBar" {
		t.Fatalf("resolveMarkerPosition low = %q", got)
	}
}
