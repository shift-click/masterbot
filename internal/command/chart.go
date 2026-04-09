package command

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/pkg/chart"
	"github.com/shift-click/masterbot/pkg/formatter"
)

// chartCoinResolver is the subset of CoinResolver needed by ChartHandler.
type chartCoinResolver interface {
	Resolve(ctx context.Context, input string) (providers.CoinSearchResult, bool)
	ResolveLocalOnly(input string) (providers.CoinSearchResult, bool)
}

// chartStockResolver is the subset of NaverStock needed by ChartHandler.
type chartStockResolver interface {
	Resolve(ctx context.Context, input string) (providers.StockSearchResult, error)
	ResolveLocalOnly(input string) (providers.StockSearchResult, bool)
	ResolveBareKRXExact(ctx context.Context, input string) (providers.StockSearchResult, error)
}

// chartCoinOHLC fetches OHLC data for coins.
type chartCoinOHLC interface {
	Fetch(ctx context.Context, symbol string, tf providers.Timeframe) (providers.OHLCData, error)
}

// chartStockOHLC fetches OHLC data for stocks.
type chartStockOHLC interface {
	FetchDomestic(ctx context.Context, code string, tf providers.Timeframe) (providers.OHLCData, error)
	FetchWorld(ctx context.Context, reutersCode string, tf providers.Timeframe) (providers.OHLCData, error)
}

// chartDEXOHLC fetches OHLC data for DEX tokens.
type chartDEXOHLC interface {
	Fetch(ctx context.Context, chainID, poolAddress string, tf providers.Timeframe) (providers.OHLCData, error)
}

// chartImageCache caches rendered chart images.
type chartImageCache interface {
	Get(ctx context.Context, key string) (string, bool)
	Set(ctx context.Context, key string, value string, ttl time.Duration)
}

// simpleImageCache is a thread-safe in-memory cache for chart images
// with probabilistic eviction and maximum size enforcement.
type simpleImageCache struct {
	mu       sync.RWMutex
	items    map[string]cacheEntry
	maxItems int
	evictN   int // eviction counter for probabilistic cleanup
}

type cacheEntry struct {
	value     string
	expiresAt time.Time
}

const defaultMaxCacheItems = 1000

func newSimpleImageCache() *simpleImageCache {
	return &simpleImageCache{
		items:    make(map[string]cacheEntry),
		maxItems: defaultMaxCacheItems,
	}
}

func (c *simpleImageCache) Get(_ context.Context, key string) (string, bool) {
	c.mu.RLock()
	entry, ok := c.items[key]
	c.mu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return "", false
	}
	return entry.value, true
}

func (c *simpleImageCache) Set(_ context.Context, key, value string, ttl time.Duration) {
	c.mu.Lock()
	c.items[key] = cacheEntry{value: value, expiresAt: time.Now().Add(ttl)}
	c.evictN++

	// Probabilistic eviction: every 10 writes, clean expired entries.
	if c.evictN%10 == 0 {
		now := time.Now()
		for k, v := range c.items {
			if now.After(v.expiresAt) {
				delete(c.items, k)
			}
		}
	}

	// Enforce max size: remove oldest-expiring entries if over limit.
	for len(c.items) > c.maxItems {
		var oldestKey string
		var oldestExp time.Time
		for k, v := range c.items {
			if oldestKey == "" || v.expiresAt.Before(oldestExp) {
				oldestKey = k
				oldestExp = v.expiresAt
			}
		}
		if oldestKey != "" {
			delete(c.items, oldestKey)
		}
	}
	c.mu.Unlock()
}

// ChartHandler handles chart commands.
type ChartHandler struct {
	descriptorSupport
	coinResolver  chartCoinResolver
	stockResolver chartStockResolver
	binanceOHLC   chartCoinOHLC
	upbitOHLC     chartCoinOHLC
	stockOHLC     chartStockOHLC
	dexOHLC       chartDEXOHLC
	imgCache      chartImageCache
	rendererURL   string // TradingView chart renderer sidecar URL (empty = disabled)
	httpClient    *http.Client
	logger        *slog.Logger
}

// ChartHandlerDeps groups constructor dependencies to keep call sites stable.
type ChartHandlerDeps struct {
	CoinResolver  chartCoinResolver
	StockResolver chartStockResolver
	BinanceOHLC   chartCoinOHLC
	UpbitOHLC     chartCoinOHLC
	StockOHLC     chartStockOHLC
	DEXOHLC       chartDEXOHLC
	ImageCache    chartImageCache
	RendererURL   string
	HTTPClient    *http.Client
	Logger        *slog.Logger
}

// NewChartHandler creates a new ChartHandler.
func NewChartHandler(deps ChartHandlerDeps) *ChartHandler {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.ImageCache == nil {
		deps.ImageCache = newSimpleImageCache()
	}
	if deps.HTTPClient == nil {
		deps.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &ChartHandler{
		descriptorSupport: newDescriptorSupport("chart"),
		coinResolver:      deps.CoinResolver,
		stockResolver:     deps.StockResolver,
		binanceOHLC:       deps.BinanceOHLC,
		upbitOHLC:         deps.UpbitOHLC,
		stockOHLC:         deps.StockOHLC,
		dexOHLC:           deps.DEXOHLC,
		imgCache:          deps.ImageCache,
		rendererURL:       deps.RendererURL,
		httpClient:        deps.HTTPClient,
		logger:            deps.Logger.With("component", "chart_handler"),
	}
}

// chartKeyword is the keyword that triggers chart matching.
const chartKeyword = "차트"

type chartCandidateShape string

const (
	chartCandidateShapeNone      chartCandidateShape = "none"
	chartCandidateShapePrefix    chartCandidateShape = "prefix"
	chartCandidateShapeSuffix    chartCandidateShape = "suffix"
	chartCandidateShapeSubstring chartCandidateShape = "substring_only"
	chartCandidateShapeKeyword   chartCandidateShape = "keyword_only"
)

// timeframeAliases maps Korean/English timeframe strings.
var timeframeAliases = []string{"1일", "1주", "1달", "1개월", "3달", "3개월", "6달", "6개월", "1년", "1d", "1w", "1m", "3m", "6m", "1y"}

const chartDateLayout = "2006-01-02"

func (h *ChartHandler) SupportsSlashCommands() bool { return false }

// Execute handles explicit chart execution after bare-query routing.
func (h *ChartHandler) Execute(ctx context.Context, cmd bot.CommandContext) error {
	query, tf := parseChartArgs(cmd.Args)
	if query == "" {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "차트를 조회할 자산명을 입력해주세요.\n예: 차트 비트코인, 차트 삼전 1달",
		})
	}
	return h.renderAndReply(ctx, cmd, query, tf)
}

// MatchBareQuery checks if input contains a chart-shaped standalone "차트" token.
func (h *ChartHandler) MatchBareQuery(ctx context.Context, content string) ([]string, bool) {
	query, tf, matched := h.matchImplicitChart(ctx, content)
	if !matched {
		return nil, false
	}
	args := []string{query}
	if tf != "" {
		args = append(args, string(tf))
	}
	return args, true
}

// HandleFallback handles deterministic fallback — checks for "차트" keyword in message.
func (h *ChartHandler) HandleFallback(ctx context.Context, cmd bot.CommandContext) error {
	var query string
	var tf providers.Timeframe

	if len(cmd.Args) > 0 {
		query = cmd.Args[0]
		if len(cmd.Args) > 1 {
			tf, _ = providers.ParseTimeframe(cmd.Args[1])
		}
	} else {
		var matched bool
		query, tf, matched = h.matchImplicitChart(ctx, cmd.Message.Msg)
		if !matched {
			return nil
		}
	}

	if query == "" {
		return nil // no "차트" keyword — skip, let other handlers try
	}

	if err := h.renderAndReply(ctx, cmd, query, tf); err != nil {
		return err
	}
	return bot.ErrHandled
}

func (h *ChartHandler) matchImplicitChart(ctx context.Context, content string) (string, providers.Timeframe, bool) {
	query, tf, shape := parseChartInputDetailed(content)
	if shape != chartCandidateShapeNone {
		resolveResult := "skipped"
		if query != "" {
			if _, err := h.resolveImplicitAsset(ctx, query, tf); err != nil {
				resolveResult = "unresolved"
				h.logger.Debug("chart implicit candidate",
					"chart_candidate_shape", shape,
					"chart_resolve_result", resolveResult,
				)
				return "", "", false
			}
			resolveResult = "matched"
		}
		h.logger.Debug("chart implicit candidate",
			"chart_candidate_shape", shape,
			"chart_resolve_result", resolveResult,
		)
	}
	if query == "" {
		return "", "", false
	}
	return query, tf, true
}

func (h *ChartHandler) renderAndReply(ctx context.Context, cmd bot.CommandContext, query string, tf providers.Timeframe) error {
	// Resolve asset
	resolved, err := h.resolveAsset(ctx, query, tf)
	if err != nil {
		if errors.Is(err, providers.ErrWorldStockChartUnavailable) {
			return cmd.Reply(ctx, bot.Reply{
				Type: transport.ReplyTypeText,
				Text: fmt.Sprintf("%s 차트는 현재 공급자에서 지원하지 않습니다.", query),
			})
		}
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: fmt.Sprintf("'%s'에 해당하는 자산을 찾을 수 없습니다.", query),
		})
	}

	cacheKey := fmt.Sprintf("chart:img:%s:%s:%s:%s", resolved.kind, resolved.coinVenue, resolved.symbol, resolved.tf)
	ttl := chartCacheTTL(resolved.tf)

	// Check image cache
	if cached, ok := h.imgCache.Get(ctx, cacheKey); ok {
		h.logger.Debug("chart cache hit", "key", cacheKey)
		return cmd.Reply(ctx, bot.Reply{Type: transport.ReplyTypeImage, ImageBase64: cached})
	}

	// Fetch OHLC data (needed for both sidecar and native rendering)
	ohlc, err := h.fetchOHLC(ctx, resolved)
	if err != nil {
		h.logger.Error("chart ohlc fetch failed", "symbol", resolved.symbol, "error", err)
		replyErr := cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: fmt.Sprintf("%s 차트 데이터를 가져올 수 없습니다.", resolved.name),
		})
		if replyErr != nil {
			return replyErr
		}
		return bot.ErrHandledWithFailure
	}

	if len(ohlc.Points) < 2 {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: fmt.Sprintf("%s 차트 데이터가 부족합니다.", resolved.name),
		})
	}

	// Try lightweight-charts sidecar (all assets supported — uses our OHLC data)
	if h.rendererURL != "" {
		b64, err := h.renderFromSidecar(ctx, ohlc, resolved.name, resolved.symbol, resolved.tf, resolved.kind)
		if err == nil {
			h.imgCache.Set(ctx, cacheKey, b64, ttl)
			return cmd.Reply(ctx, bot.Reply{Type: transport.ReplyTypeImage, ImageBase64: b64})
		}
		h.logger.Warn("chart sidecar render failed, falling back to native", "symbol", resolved.symbol, "error", err)
	}

	// Fallback: Go native candlestick chart
	chartData := h.buildChartData(ohlc, resolved.name, resolved.symbol, resolved.tf, resolved.kind)
	b64, err := chart.DrawPriceChartBase64(chartData, chart.DefaultConfig())
	if err != nil {
		h.logger.Error("chart render failed", "symbol", resolved.symbol, "error", err)
		replyErr := cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: fmt.Sprintf("%s 차트 렌더링에 실패했습니다.", resolved.name),
		})
		if replyErr != nil {
			return replyErr
		}
		return bot.ErrHandledWithFailure
	}

	h.imgCache.Set(ctx, cacheKey, b64, ttl)
	return cmd.Reply(ctx, bot.Reply{
		Type:        transport.ReplyTypeImage,
		ImageBase64: b64,
	})
}

type assetType string

const (
	assetCoin     assetType = "coin"
	assetDomestic assetType = "domestic"
	assetWorld    assetType = "world"
	assetDEX      assetType = "dex"
)

// dexResolveInfo holds extra info needed for DEX OHLC fetching.
type dexResolveInfo struct {
	chainID     string
	pairAddress string
}

type resolvedChartAsset struct {
	kind      assetType
	symbol    string
	name      string
	tf        providers.Timeframe
	dex       *dexResolveInfo
	coinVenue providers.CoinChartVenue
}

func (h *ChartHandler) resolveAsset(ctx context.Context, query string, tf providers.Timeframe) (resolvedChartAsset, error) {
	// Try coin first
	if coinResult, ok := h.coinResolver.Resolve(ctx, query); ok {
		return h.resolveCoinChartAsset(coinResult, tf)
	}

	// Try stock
	stockResult, err := h.stockResolver.Resolve(ctx, query)
	if err == nil && stockResult.Code != "" {
		if tf == "" {
			tf = providers.Timeframe3M
		}
		if stockResult.NationCode != "" && stockResult.NationCode != "KOR" {
			if stockResult.ReutersCode == "" || strings.HasPrefix(stockResult.ReutersCode, "UNSUPPORTED:") {
				return resolvedChartAsset{}, providers.ErrWorldStockChartUnavailable
			}
			return resolvedChartAsset{kind: assetWorld, symbol: stockResult.ReutersCode, name: stockResult.Name, tf: tf}, nil
		}
		return resolvedChartAsset{kind: assetDomestic, symbol: stockResult.Code, name: stockResult.Name, tf: tf}, nil
	}

	return resolvedChartAsset{}, fmt.Errorf("unresolved: %s", query)
}

func (h *ChartHandler) resolveImplicitAsset(ctx context.Context, query string, tf providers.Timeframe) (resolvedChartAsset, error) {
	if _, ok := h.coinResolver.ResolveLocalOnly(query); ok {
		if coinResult, ok := h.coinResolver.Resolve(ctx, query); ok {
			return h.resolveCoinChartAsset(coinResult, tf)
		}
	}

	if stockResult, ok := h.stockResolver.ResolveLocalOnly(query); ok {
		if stockResult.NationCode != "" && stockResult.NationCode != "KOR" &&
			(stockResult.ReutersCode == "" || strings.HasPrefix(stockResult.ReutersCode, "UNSUPPORTED:")) {
			return resolvedChartAsset{}, providers.ErrWorldStockChartUnavailable
		}
		return resolveStockChartAsset(stockResult, tf), nil
	}
	if stockResult, err := h.stockResolver.ResolveBareKRXExact(ctx, query); err == nil && stockResult.Code != "" {
		return resolveStockChartAsset(stockResult, tf), nil
	}

	return resolvedChartAsset{}, fmt.Errorf("unresolved implicit asset: %s", query)
}

func resolveStockChartAsset(stockResult providers.StockSearchResult, tf providers.Timeframe) resolvedChartAsset {
	if tf == "" {
		tf = providers.Timeframe3M
	}
	if stockResult.NationCode != "" && stockResult.NationCode != "KOR" {
		return resolvedChartAsset{kind: assetWorld, symbol: stockResult.ReutersCode, name: stockResult.Name, tf: tf}
	}
	return resolvedChartAsset{kind: assetDomestic, symbol: stockResult.Code, name: stockResult.Name, tf: tf}
}

func (h *ChartHandler) resolveCoinChartAsset(result providers.CoinSearchResult, tf providers.Timeframe) (resolvedChartAsset, error) {
	if tf == "" {
		tf = providers.Timeframe1W
	}
	name := result.Name
	if name == "" {
		name = result.Symbol
	}
	venue := h.selectCoinChartVenue(result)
	switch venue {
	case providers.CoinChartVenueUpbit:
		if symbol := result.EffectiveUpbitSymbol(); symbol != "" {
			return resolvedChartAsset{
				kind:      assetCoin,
				symbol:    symbol,
				name:      name,
				tf:        tf,
				coinVenue: venue,
			}, nil
		}
	case providers.CoinChartVenueBinance:
		if symbol := result.EffectiveBinanceSymbol(); symbol != "" {
			return resolvedChartAsset{
				kind:      assetCoin,
				symbol:    symbol,
				name:      name,
				tf:        tf,
				coinVenue: venue,
			}, nil
		}
	case providers.CoinChartVenueDEX:
		if result.PairAddress != "" {
			return resolvedChartAsset{
				kind:      assetDEX,
				symbol:    result.Symbol,
				name:      name,
				tf:        tf,
				coinVenue: venue,
				dex:       &dexResolveInfo{chainID: result.ChainID, pairAddress: result.PairAddress},
			}, nil
		}
	}
	return resolvedChartAsset{}, fmt.Errorf("coin chart venue unavailable: %s", result.Symbol)
}

func (h *ChartHandler) selectCoinChartVenue(result providers.CoinSearchResult) providers.CoinChartVenue {
	try := func(venue providers.CoinChartVenue) bool {
		switch venue {
		case providers.CoinChartVenueUpbit:
			return h.upbitOHLC != nil && result.EffectiveUpbitSymbol() != ""
		case providers.CoinChartVenueBinance:
			return h.binanceOHLC != nil && result.EffectiveBinanceSymbol() != ""
		case providers.CoinChartVenueDEX:
			return h.dexOHLC != nil && result.PairAddress != "" && result.ChainID != ""
		default:
			return false
		}
	}
	if pref := result.PreferredChartVenue; pref != providers.CoinChartVenueUnknown && try(pref) {
		return pref
	}
	for _, venue := range []providers.CoinChartVenue{
		providers.CoinChartVenueBinance,
		providers.CoinChartVenueUpbit,
		providers.CoinChartVenueDEX,
	} {
		if try(venue) {
			return venue
		}
	}
	return providers.CoinChartVenueUnknown
}

func (h *ChartHandler) fetchOHLC(ctx context.Context, asset resolvedChartAsset) (providers.OHLCData, error) {
	switch asset.kind {
	case assetCoin:
		switch asset.coinVenue {
		case providers.CoinChartVenueUpbit:
			if h.upbitOHLC == nil {
				return providers.OHLCData{}, fmt.Errorf("upbit OHLC not available")
			}
			return h.upbitOHLC.Fetch(ctx, asset.symbol, asset.tf)
		case providers.CoinChartVenueBinance:
			if h.binanceOHLC == nil {
				return providers.OHLCData{}, fmt.Errorf("binance OHLC not available")
			}
			return h.binanceOHLC.Fetch(ctx, asset.symbol, asset.tf)
		default:
			return providers.OHLCData{}, fmt.Errorf("unknown coin chart venue: %s", asset.coinVenue)
		}
	case assetDomestic:
		return h.stockOHLC.FetchDomestic(ctx, asset.symbol, asset.tf)
	case assetWorld:
		return h.stockOHLC.FetchWorld(ctx, asset.symbol, asset.tf)
	case assetDEX:
		if h.dexOHLC == nil || asset.dex == nil {
			return providers.OHLCData{}, fmt.Errorf("DEX OHLC not available")
		}
		return h.dexOHLC.Fetch(ctx, asset.dex.chainID, asset.dex.pairAddress, asset.tf)
	default:
		return providers.OHLCData{}, fmt.Errorf("unknown asset type: %s", asset.kind)
	}
}

func (h *ChartHandler) buildChartData(ohlc providers.OHLCData, name, symbol string, tf providers.Timeframe, at assetType) chart.PriceChartData {
	prices, timestamps, volumes, candles, lowest, highest, hasVolume := buildChartSeries(ohlc)
	currentClose := ohlc.Points[len(ohlc.Points)-1].Close
	firstClose := ohlc.Points[0].Close
	currentPrice := prices[len(prices)-1]
	firstPrice := prices[0]

	data := chart.PriceChartData{
		Prices:       prices,
		Timestamps:   timestamps,
		CurrentPrice: currentPrice,
		LowestPrice:  lowest,
		HighestPrice: highest,
		ProductName:  buildChartHeader(name, symbol),
		PeriodLabel:  providers.TimeframeLabel(tf),
		Direction:    resolveChartDirection(currentPrice, firstPrice),
		SubTitle:     buildChartSubtitle(currentClose, firstClose),
		Candles:      candles,
		AssetType:    chartAssetTypeLabel(at),
	}

	if hasVolume {
		data.Volumes = volumes
	}

	return data
}

func buildChartSeries(ohlc providers.OHLCData) ([]int, []time.Time, []float64, []chart.CandlePoint, int, int, bool) {
	n := len(ohlc.Points)
	prices := make([]int, n)
	timestamps := make([]time.Time, n)
	volumes := make([]float64, n)
	candles := make([]chart.CandlePoint, n)
	hasVolume := false

	var lowest, highest int
	for i, p := range ohlc.Points {
		price := int(p.Close)
		prices[i] = price
		timestamps[i] = p.Time
		volumes[i] = p.Volume
		candles[i] = chart.CandlePoint{
			Time:   p.Time,
			Open:   p.Open,
			High:   p.High,
			Low:    p.Low,
			Close:  p.Close,
			Volume: p.Volume,
		}
		if p.Volume > 0 {
			hasVolume = true
		}

		if i == 0 || price < lowest {
			lowest = price
		}
		if i == 0 || price > highest {
			highest = price
		}
	}

	return prices, timestamps, volumes, candles, lowest, highest, hasVolume
}

func resolveChartDirection(currentPrice, firstPrice int) chart.ChartDirection {
	if currentPrice > firstPrice {
		return chart.DirectionUp
	}
	if currentPrice < firstPrice {
		return chart.DirectionDown
	}
	return ""
}

func buildChartHeader(name, symbol string) string {
	if symbol == "" || symbol == name {
		return name
	}
	return fmt.Sprintf("%s (%s)", name, symbol)
}

func buildChartSubtitle(currentClose, firstClose float64) string {
	changePct := 0.0
	if firstClose > 0 {
		changePct = (currentClose - firstClose) / firstClose * 100
	}
	if changePct >= 0 {
		return fmt.Sprintf("%s (+%.2f%%)", formatChartPrice(currentClose), changePct)
	}
	return fmt.Sprintf("%s (%.2f%%)", formatChartPrice(currentClose), changePct)
}

func chartAssetTypeLabel(at assetType) string {
	if at == assetCoin || at == assetDEX {
		return "coin"
	}
	return "stock"
}

func (h *ChartHandler) buildSummaryText(name, symbol string, tf providers.Timeframe) string {
	return formatter.FormatChartSummary(name, symbol, "", "", providers.TimeframeLabel(tf))
}

func chartCacheTTL(tf providers.Timeframe) time.Duration {
	switch tf {
	case providers.Timeframe1D:
		return 3 * time.Minute
	case providers.Timeframe1W:
		return 15 * time.Minute
	case providers.Timeframe1M:
		return 30 * time.Minute
	default:
		return 1 * time.Hour
	}
}

// parseChartInput extracts asset query and timeframe from a bare text input
// containing the standalone "차트" keyword (e.g., "비트코인 차트", "차트 삼전 1달").
func parseChartInput(content string) (string, providers.Timeframe) {
	query, tf, _ := parseChartInputDetailed(content)
	return query, tf
}

func parseChartInputDetailed(content string) (string, providers.Timeframe, chartCandidateShape) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", "", chartCandidateShapeNone
	}

	fields := strings.Fields(content)
	if len(fields) == 0 {
		return "", "", chartCandidateShapeNone
	}

	if query, tf, shape, ok := parseChartPrefixedInput(fields); ok {
		return query, tf, shape
	}

	if query, tf, shape, ok := parseChartSuffixedInput(fields); ok {
		return query, tf, shape
	}

	if hasChartKeywordSubstring(fields) {
		return "", "", chartCandidateShapeSubstring
	}

	return "", "", chartCandidateShapeNone
}

func parseChartPrefixedInput(fields []string) (string, providers.Timeframe, chartCandidateShape, bool) {
	if fields[0] != chartKeyword {
		return "", "", chartCandidateShapeNone, false
	}
	if len(fields) == 1 {
		return "", "", chartCandidateShapeKeyword, true
	}
	query, tf, shape := parseChartRest(fields[1:], chartCandidateShapePrefix)
	return query, tf, shape, true
}

func parseChartSuffixedInput(fields []string) (string, providers.Timeframe, chartCandidateShape, bool) {
	last := fields[len(fields)-1]
	if tf, ok := providers.ParseTimeframe(last); ok && len(fields) >= 3 && fields[len(fields)-2] == chartKeyword {
		query, tf, shape := joinChartQuery(fields[:len(fields)-2], tf, chartCandidateShapeSuffix)
		return query, tf, shape, true
	}
	if last == chartKeyword && len(fields) >= 2 {
		query, tf, shape := parseChartRest(fields[:len(fields)-1], chartCandidateShapeSuffix)
		return query, tf, shape, true
	}
	return "", "", chartCandidateShapeNone, false
}

func joinChartQuery(parts []string, tf providers.Timeframe, shape chartCandidateShape) (string, providers.Timeframe, chartCandidateShape) {
	query := strings.TrimSpace(strings.Join(parts, " "))
	if query == "" {
		return "", "", chartCandidateShapeKeyword
	}
	return query, tf, shape
}

func hasChartKeywordSubstring(fields []string) bool {
	for _, field := range fields {
		if strings.Contains(field, chartKeyword) {
			return true
		}
	}
	return false
}

func parseChartRest(rest []string, shape chartCandidateShape) (string, providers.Timeframe, chartCandidateShape) {
	if len(rest) >= 2 {
		if tf, ok := providers.ParseTimeframe(rest[len(rest)-1]); ok {
			query := strings.TrimSpace(strings.Join(rest[:len(rest)-1], " "))
			if query == "" {
				return "", "", chartCandidateShapeKeyword
			}
			return query, tf, shape
		}
	}

	query := strings.TrimSpace(strings.Join(rest, " "))
	if query == "" {
		return "", "", chartCandidateShapeKeyword
	}
	return query, "", shape
}

// parseChartArgs extracts query and timeframe from chart command args.
func parseChartArgs(args []string) (string, providers.Timeframe) {
	var queryParts []string
	var tf providers.Timeframe

	for _, arg := range args {
		if parsed, ok := providers.ParseTimeframe(arg); ok {
			tf = parsed
			continue
		}
		queryParts = append(queryParts, arg)
	}

	return strings.Join(queryParts, " "), tf
}

// --- lightweight-charts sidecar integration ---

type renderRequest struct {
	Title         string         `json:"title"`
	Subtitle      string         `json:"subtitle"`
	SubtitleColor string         `json:"subtitleColor"`
	Candles       []renderCandle `json:"candles"`
	Volumes       []renderVolume `json:"volumes"`
	SMA           []renderSMA    `json:"sma,omitempty"`
	Markers       []renderMarker `json:"markers,omitempty"`
	TimeVisible   bool           `json:"timeVisible"`
	Width         int            `json:"width"`
	Height        int            `json:"height"`
}

type renderCandle struct {
	Time  string  `json:"time"`
	Open  float64 `json:"open"`
	High  float64 `json:"high"`
	Low   float64 `json:"low"`
	Close float64 `json:"close"`
}

type renderVolume struct {
	Time  string  `json:"time"`
	Value float64 `json:"value"`
	Color string  `json:"color"`
}

type renderSMA struct {
	Time  string  `json:"time"`
	Value float64 `json:"value"`
}

type renderMarker struct {
	Time     string  `json:"time"`
	Position string  `json:"position"` // "aboveBar" or "belowBar"
	Color    string  `json:"color"`
	Shape    string  `json:"shape"`
	Text     string  `json:"text"`
	Price    float64 `json:"price"`
}

func (h *ChartHandler) renderFromSidecar(ctx context.Context, ohlc providers.OHLCData, name, symbol string, tf providers.Timeframe, at assetType) (string, error) {
	candles, volumes := buildSidecarSeries(ohlc)
	subtitle, subtitleColor := buildSidecarSubtitle(ohlc)
	reqBody := renderRequest{
		Title:         buildChartHeader(name, symbol),
		Subtitle:      subtitle,
		SubtitleColor: subtitleColor,
		Candles:       candles,
		Volumes:       volumes,
		SMA:           buildSidecarSMA(ohlc),
		Markers:       buildSidecarMarkers(ohlc, tf, at),
		TimeVisible:   tf == providers.Timeframe1D,
		Width:         800,
		Height:        600,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.rendererURL+"/render", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("sidecar request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("sidecar status %d: %s", resp.StatusCode, body)
	}

	pngBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	return base64.StdEncoding.EncodeToString(pngBytes), nil
}

func buildSidecarSeries(ohlc providers.OHLCData) ([]renderCandle, []renderVolume) {
	candles := make([]renderCandle, len(ohlc.Points))
	volumes := make([]renderVolume, len(ohlc.Points))
	for i, point := range ohlc.Points {
		timestamp := point.Time.Format(chartDateLayout)
		candles[i] = renderCandle{
			Time:  timestamp,
			Open:  point.Open,
			High:  point.High,
			Low:   point.Low,
			Close: point.Close,
		}
		volumes[i] = renderVolume{
			Time:  timestamp,
			Value: point.Volume,
			Color: renderVolumeColor(point),
		}
	}
	return candles, volumes
}

func renderVolumeColor(point providers.OHLCPoint) string {
	if point.Close < point.Open {
		return "#EF535080"
	}
	return "#26A69A80"
}

func buildSidecarSMA(ohlc providers.OHLCData) []renderSMA {
	const smaPeriod = 20
	if len(ohlc.Points) < smaPeriod {
		return nil
	}
	sma := make([]renderSMA, 0, len(ohlc.Points)-smaPeriod+1)
	for i := smaPeriod - 1; i < len(ohlc.Points); i++ {
		sum := 0.0
		for j := i - (smaPeriod - 1); j <= i; j++ {
			sum += ohlc.Points[j].Close
		}
		sma = append(sma, renderSMA{
			Time:  ohlc.Points[i].Time.Format(chartDateLayout),
			Value: sum / smaPeriod,
		})
	}
	return sma
}

func buildSidecarSubtitle(ohlc providers.OHLCData) (string, string) {
	currentPrice := ohlc.Points[len(ohlc.Points)-1].Close
	firstPrice := ohlc.Points[0].Close
	subtitle := buildChartSubtitle(currentPrice, firstPrice)
	if firstPrice > 0 && currentPrice < firstPrice {
		return subtitle, "#EF5350"
	}
	return subtitle, "#26A69A"
}

func buildSidecarMarkers(ohlc providers.OHLCData, tf providers.Timeframe, at assetType) []renderMarker {
	pinpoints := chart.DetectPinpoints(toChartCandles(ohlc.Points), chartAssetTypeLabel(at), providers.TimeframeLabel(tf))
	markers := make([]renderMarker, 0, len(pinpoints))
	for _, pp := range pinpoints {
		markers = append(markers, renderMarker{
			Time:     pp.Time.Format(chartDateLayout),
			Position: resolveMarkerPosition(pp),
			Text:     formatChartPrice(pp.Price),
			Price:    pp.Price,
		})
	}
	return markers
}

func toChartCandles(points []providers.OHLCPoint) []chart.CandlePoint {
	candles := make([]chart.CandlePoint, len(points))
	for i, point := range points {
		candles[i] = chart.CandlePoint{
			Time:   point.Time,
			Open:   point.Open,
			High:   point.High,
			Low:    point.Low,
			Close:  point.Close,
			Volume: point.Volume,
		}
	}
	return candles
}

func resolveMarkerPosition(pin chart.Pinpoint) string {
	if pin.IsHigh {
		return "aboveBar"
	}
	return "belowBar"
}

func formatChartPrice(price float64) string {
	sign := ""
	if price < 0 {
		sign = "-"
		price = -price
	}

	parts := strings.SplitN(fmt.Sprintf("%.2f", price), ".", 2)
	intPart := parts[0]
	fracPart := "00"
	if len(parts) == 2 {
		fracPart = parts[1]
	}

	if len(intPart) <= 3 {
		return sign + intPart + "." + fracPart
	}

	result := make([]byte, 0, len(intPart)+len(intPart)/3+1+len(fracPart))
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	result = append(result, '.')
	result = append(result, fracPart...)
	return sign + string(result)
}
