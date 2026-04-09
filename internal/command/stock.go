package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/pkg/formatter"
)

// StockHandler handles stock quote lookups.
// It works as a bare-query handler and as a local-auto fallback handler.
type StockHandler struct {
	descriptorSupport
	naver      stockProvider
	hotlist    *scraper.HotList
	themeIndex stockThemeIndex
	logger     *slog.Logger
}

type stockProvider interface {
	ResolveLocalOnly(string) (providers.StockSearchResult, bool)
	ResolveBareKRXExact(context.Context, string) (providers.StockSearchResult, error)
	Resolve(context.Context, string) (providers.StockSearchResult, error)
	FetchWorldQuote(context.Context, string) (providers.StockQuote, error)
	FetchQuote(context.Context, string) (providers.StockQuote, error)
	FetchBasicPublic(context.Context, string) (providers.BasicInfo, error)
}

type stockThemeIndex interface {
	Match(string) []scraper.ThemeMatchResult
	FetchJudalStockCodes(context.Context, int) ([]string, error)
	FetchDetail(context.Context, int) (providers.ThemeDetail, error)
}

// NewStockHandler creates a new stock handler.
func NewStockHandler(naver stockProvider, hotlist *scraper.HotList, themeIndex stockThemeIndex, logger *slog.Logger) *StockHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &StockHandler{
		descriptorSupport: newDescriptorSupport("stock"),
		naver:             naver,
		hotlist:           hotlist,
		themeIndex:        themeIndex,
		logger:            logger.With("component", "stock_handler"),
	}
}

func (h *StockHandler) SupportsSlashCommands() bool { return false }

func (h *StockHandler) MatchAutoQueryCandidate(ctx context.Context, content string) bool {
	query, ok := extractAutoCandidate(content, 3)
	if !ok || len([]rune(query)) > 20 {
		return false
	}
	_, matched := h.naver.ResolveLocalOnly(query)
	return matched
}

func (h *StockHandler) Execute(ctx context.Context, cmd bot.CommandContext) error {
	if len(cmd.Args) == 0 {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "종목명을 입력해주세요. (예: 삼성전자)",
		})
	}

	query := strings.Join(cmd.Args, " ")

	// Check for quantity multiplier: "삼전 * 10"
	baseQuery, qty, hasQty := parseQuantifiedQuery(query)
	if hasQty && qty != 1 {
		return h.executeQuantity(ctx, cmd, baseQuery, qty)
	}

	return h.lookup(ctx, cmd, query, false)
}

func (h *StockHandler) MatchBareQuery(ctx context.Context, content string) ([]string, bool) {
	query := strings.TrimSpace(content)
	if query == "" || len([]rune(query)) > 48 {
		return nil, false
	}

	// Try quantity pattern first: "삼전 * 10", "삼전*10"
	baseQuery, _, qOk := parseQuantifiedQuery(query)
	if qOk {
		baseQuery = strings.TrimSpace(baseQuery)
	} else {
		baseQuery = query
	}

	// Reject multi-word non-quantity queries (more than 1 space).
	if !qOk && strings.Count(query, " ") > 1 {
		return nil, false
	}
	if !qOk {
		if _, themeShaped := extractThemeKeyword(query); themeShaped {
			return nil, false
		}
	}

	// Exact local match only for bare stock queries.
	if _, ok := h.naver.ResolveLocalOnly(baseQuery); ok {
		return []string{query}, true
	}
	if _, err := h.naver.ResolveBareKRXExact(ctx, baseQuery); err == nil {
		return []string{query}, true
	}
	return nil, false
}

// HandleFallback handles local-auto stock candidates such as "삼전" or "오늘 삼전".
func (h *StockHandler) HandleFallback(ctx context.Context, cmd bot.CommandContext) error {
	query, ok := extractAutoCandidate(cmd.Message.Msg, 3)
	if !ok {
		return nil
	}

	// Quick filter: skip very long messages (likely not stock queries).
	if len([]rune(query)) > 20 {
		return nil
	}

	// local-auto stays exact-only.
	return h.lookup(ctx, cmd, query, true)
}

// lookup resolves a query and returns formatted stock data.
func (h *StockHandler) lookup(ctx context.Context, cmd bot.CommandContext, query string, localOnly bool) error {
	result, resolved, resolution := h.resolveLookupTarget(ctx, cmd, query, localOnly)
	if resolution != nil {
		return resolution
	}
	if !resolved {
		return nil
	}

	hotlistKey, isWorldStock := resolveStockHotlistKey(result)
	if handled, err := h.replyFromHotlist(ctx, cmd, hotlistKey); err != nil {
		return err
	} else if handled {
		return handledByCommandType(cmd)
	}

	quote, err := h.fetchQuote(ctx, result, isWorldStock)
	if err != nil {
		return h.replyLookupFetchError(ctx, cmd, hotlistKey, err)
	}
	if quote.Market == "" {
		quote.Market = result.Market
	}

	h.registerHotlist(hotlistKey, quote, isWorldStock)
	if err := h.replyQuote(ctx, cmd, quote); err != nil {
		return err
	}
	return handledByCommandType(cmd)
}

func (h *StockHandler) resolveLookupTarget(ctx context.Context, cmd bot.CommandContext, query string, localOnly bool) (providers.StockSearchResult, bool, error) {
	if !shouldForceRemoteStockResolve(cmd, query) {
		if result, ok := h.naver.ResolveLocalOnly(query); ok {
			return result, true, nil
		}
	}
	if localOnly {
		return providers.StockSearchResult{}, false, nil
	}
	if themeKeyword, ok := extractThemeKeyword(query); ok && h.themeIndex != nil && h.tryThemeLookup(ctx, cmd, themeKeyword) {
		return providers.StockSearchResult{}, false, handledByCommandType(cmd)
	}
	result, err := h.naver.Resolve(ctx, query)
	if err != nil {
		if cmd.Command != "" {
			h.logger.Debug("stock resolve failed", "query", query, "error", err)
			return providers.StockSearchResult{}, false, h.replyResolveError(ctx, cmd, query, err)
		}
		return providers.StockSearchResult{}, false, nil
	}
	return result, true, nil
}

func shouldForceRemoteStockResolve(cmd bot.CommandContext, query string) bool {
	if cmd.Command == "" {
		return false
	}
	return isDigitsOnly(strings.TrimSpace(query))
}

func isDigitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func resolveStockHotlistKey(result providers.StockSearchResult) (string, bool) {
	isWorldStock := result.NationCode != "" && result.NationCode != "KOR"
	if isWorldStock && result.ReutersCode != "" {
		return result.ReutersCode, true
	}
	return result.Code, isWorldStock
}

func (h *StockHandler) replyFromHotlist(ctx context.Context, cmd bot.CommandContext, hotlistKey string) (bool, error) {
	snapshot, ok := h.hotlist.Get(hotlistKey)
	if !ok {
		return false, nil
	}
	var quote providers.StockQuote
	if err := json.Unmarshal(snapshot.Data, &quote); err != nil {
		return false, nil
	}
	return true, h.replyQuote(ctx, cmd, quote)
}

func (h *StockHandler) fetchQuote(ctx context.Context, result providers.StockSearchResult, isWorldStock bool) (providers.StockQuote, error) {
	if isWorldStock && result.ReutersCode != "" {
		return h.naver.FetchWorldQuote(ctx, result.ReutersCode)
	}
	return h.naver.FetchQuote(ctx, result.Code)
}

func (h *StockHandler) replyResolveError(ctx context.Context, cmd bot.CommandContext, query string, resolveErr error) error {
	if errors.Is(resolveErr, providers.ErrStockNotFound) {
		msg := "종목을 찾을 수 없습니다."
		if isDigitsOnly(strings.TrimSpace(query)) {
			msg = fmt.Sprintf("존재하지 않는 종목코드입니다: %s", strings.TrimSpace(query))
		}
		if err := cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: formatter.Prefix("⚠️", msg),
		}); err != nil {
			return err
		}
		return bot.NewHandledFailure("invalid_input", false, "invalid stock input: "+strings.TrimSpace(query), resolveErr)
	}
	if err := cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: formatter.Error(resolveErr),
	}); err != nil {
		return err
	}
	return bot.NewHandledFailure("fetch_error", true, resolveErr.Error(), resolveErr)
}

func (h *StockHandler) replyLookupFetchError(ctx context.Context, cmd bot.CommandContext, hotlistKey string, fetchErr error) error {
	h.logger.Warn("stock fetch failed", "code", hotlistKey, "error", fetchErr)
	if errors.Is(fetchErr, providers.ErrWorldStockQuoteUnavailable) {
		if err := cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: formatter.Prefix("⚠️", "현재 공급자에서 지원하지 않는 해외 종목입니다."),
		}); err != nil {
			return err
		}
		return bot.NewHandledFailure("fetch_error", false, fetchErr.Error(), fetchErr)
	}
	if cmd.Command == "" {
		return bot.NewHandledFailure("fetch_error", true, fetchErr.Error(), fetchErr)
	}
	if err := cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: formatter.Error(fetchErr),
	}); err != nil {
		return err
	}
	return bot.NewHandledFailure("fetch_error", true, fetchErr.Error(), fetchErr)
}

func (h *StockHandler) registerHotlist(hotlistKey string, quote providers.StockQuote, isWorldStock bool) {
	data, _ := json.Marshal(quote)
	h.hotlist.RegisterWithMeta(hotlistKey, data, isWorldStock, "")
}

func (h *StockHandler) replyQuote(ctx context.Context, cmd bot.CommandContext, quote providers.StockQuote) error {
	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: h.formatQuote(quote),
	})
}

func handledByCommandType(cmd bot.CommandContext) error {
	if cmd.Command == "" {
		return bot.ErrHandled
	}
	return nil
}

// tryThemeLookup attempts to match the query against the theme index.
// Returns true if a response was sent (theme matched), false otherwise.
func (h *StockHandler) tryThemeLookup(ctx context.Context, cmd bot.CommandContext, query string) bool {
	matches := h.themeIndex.Match(query)
	if len(matches) == 0 {
		return false
	}
	if len(matches) > 1 {
		names := make([]string, 0, len(matches))
		seen := make(map[string]struct{}, len(matches))
		for _, match := range matches {
			if _, ok := seen[match.Name]; ok {
				continue
			}
			seen[match.Name] = struct{}{}
			names = append(names, match.Name)
		}
		_ = cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: formatter.FormatThemeDisambiguation(query, names),
		})
		return true
	}

	match := matches[0]

	if match.Source == scraper.ThemeSourceJudal {
		return h.handleJudalTheme(ctx, cmd, match)
	}
	return h.handleNaverTheme(ctx, cmd, match)
}

// handleJudalTheme fetches stock codes from Judal, then prices from Naver fetchBasic.
func (h *StockHandler) handleJudalTheme(ctx context.Context, cmd bot.CommandContext, match scraper.ThemeMatchResult) bool {
	codes, err := h.themeIndex.FetchJudalStockCodes(ctx, match.No)
	if err != nil {
		h.logger.Warn("judal stock codes fetch failed", "theme", match.Name, "error", err)
		return false
	}

	limit := 10
	if len(codes) < limit {
		limit = len(codes)
	}
	codes = codes[:limit]

	// Fetch prices in parallel.
	type result struct {
		idx  int
		info providers.BasicInfo
		err  error
	}
	ch := make(chan result, len(codes))
	for i, code := range codes {
		go func(idx int, code string) {
			info, err := h.naver.FetchBasicPublic(ctx, code)
			ch <- result{idx, info, err}
		}(i, code)
	}

	fmtStocks := make([]formatter.ThemeStockData, len(codes))
	valid := make([]bool, len(codes))
	for range codes {
		r := <-ch
		if r.err != nil {
			h.logger.Debug("fetchBasic failed for theme stock", "code", codes[r.idx], "error", r.err)
			continue
		}
		fmtStocks[r.idx] = formatter.ThemeStockData{
			Name:            r.info.Name,
			Market:          r.info.Market,
			Price:           r.info.Price,
			Change:          r.info.Change,
			ChangePercent:   r.info.ChangePercent,
			ChangeDirection: r.info.Direction,
		}
		valid[r.idx] = true
	}

	// Filter out failed stocks, preserving Judal order.
	var filtered []formatter.ThemeStockData
	for i, v := range valid {
		if v {
			filtered = append(filtered, fmtStocks[i])
		}
	}

	if len(filtered) == 0 {
		return false
	}

	// Strip parenthetical from theme name for display: "금(Gold)" → "금"
	displayName := match.Name
	if idx := strings.IndexByte(displayName, '('); idx > 0 {
		displayName = displayName[:idx]
	}

	text := formatter.FormatThemeStocks(displayName, filtered)
	_ = cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
	})
	return true
}

func extractThemeKeyword(query string) (string, bool) {
	query = strings.TrimSpace(query)
	for _, suffix := range []string{"관련주", "테마", "수혜주"} {
		if !strings.HasSuffix(query, suffix) {
			continue
		}
		keyword := strings.TrimSpace(strings.TrimSuffix(query, suffix))
		if keyword == "" {
			return "", false
		}
		return keyword, true
	}
	return "", false
}

// handleNaverTheme fetches theme detail from Naver theme API.
func (h *StockHandler) handleNaverTheme(ctx context.Context, cmd bot.CommandContext, match scraper.ThemeMatchResult) bool {
	detail, err := h.themeIndex.FetchDetail(ctx, match.No)
	if err != nil {
		h.logger.Warn("theme detail fetch failed", "theme", match.Name, "themeNo", match.No, "error", err)
		return false
	}

	sort.Slice(detail.Stocks, func(i, j int) bool {
		return detail.Stocks[i].MarketValue > detail.Stocks[j].MarketValue
	})
	limit := 10
	if len(detail.Stocks) < limit {
		limit = len(detail.Stocks)
	}
	fmtStocks := make([]formatter.ThemeStockData, limit)
	for i := 0; i < limit; i++ {
		s := detail.Stocks[i]
		fmtStocks[i] = formatter.ThemeStockData{
			Name:            s.Name,
			Market:          s.Market,
			Price:           s.Price,
			Change:          s.Change,
			ChangePercent:   s.ChangePercent,
			ChangeDirection: s.ChangeDirection,
		}
	}

	text := formatter.FormatThemeStocks(detail.Name, fmtStocks)
	_ = cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
	})
	return true
}

func (h *StockHandler) executeQuantity(ctx context.Context, cmd bot.CommandContext, query string, qty float64) error {
	// Resolve the stock.
	result, ok := h.naver.ResolveLocalOnly(query)
	if !ok {
		var err error
		result, err = h.naver.Resolve(ctx, query)
		if err != nil {
			return nil
		}
	}

	isWorld := result.NationCode != "" && result.NationCode != "KOR"
	var quote providers.StockQuote
	var fetchErr error

	if isWorld && result.ReutersCode != "" {
		quote, fetchErr = h.naver.FetchWorldQuote(ctx, result.ReutersCode)
	} else {
		quote, fetchErr = h.naver.FetchQuote(ctx, result.Code)
	}
	if fetchErr != nil {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: formatter.Error(fetchErr),
		})
	}

	// Parse price string to float (e.g., "188,800" → 188800).
	price := parseStockPriceToFloat(quote.Price)
	if price == 0 {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "가격 정보를 파싱할 수 없습니다.",
		})
	}

	currency := "KRW"
	if isWorld && quote.Currency != "" {
		currency = quote.Currency
	}

	text := formatter.FormatStockQuantity(formatter.StockQuantityData{
		Name:       quote.Name,
		SymbolCode: quote.SymbolCode,
		Quantity:   qty,
		Price:      price,
		Currency:   currency,
	})
	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
	})
}

// parseStockPriceToFloat parses a formatted price string like "188,800" or "305.56" to float.
func parseStockPriceToFloat(s string) float64 {
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err != nil {
		return 0
	}
	return f
}

// formatQuote converts a StockQuote into formatted text using the formatter package.
func (h *StockHandler) formatQuote(q providers.StockQuote) string {
	return formatter.FormatStockQuote(formatter.StockData{
		Name:            q.Name,
		Market:          q.Market,
		Price:           q.Price,
		PrevClose:       q.PrevClose,
		Change:          q.Change,
		ChangePercent:   q.ChangePercent,
		ChangeDirection: q.ChangeDirection,
		MarketCap:       q.MarketCap,
		PER:             q.PER,
		PBR:             q.PBR,
		Revenue:         q.Revenue,
		OperatingProfit: q.OperatingProfit,
		ForeignNet:      q.ForeignNet,
		InstitutionNet:  q.InstitutionNet,
		IndividualNet:   q.IndividualNet,
		TrendDate:       q.TrendDate,
		IsWorldStock:    q.IsWorldStock,
		Currency:        q.Currency,
		EBITDA:          q.EBITDA,
		SymbolCode:      q.SymbolCode,
	})
}
