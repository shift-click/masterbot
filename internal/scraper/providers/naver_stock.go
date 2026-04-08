package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// StockQuote holds parsed stock data from Naver Finance APIs.
type StockQuote struct {
	Code            string    `json:"code"`
	Name            string    `json:"name"`
	Market          string    `json:"market"`
	Price           string    `json:"price"`
	PrevClose       string    `json:"prev_close"`
	Change          string    `json:"change"`
	ChangePercent   string    `json:"change_percent"`
	ChangeDirection string    `json:"change_direction"` // RISING, FALLING, FLAT
	MarketCap       string    `json:"market_cap"`
	PER             string    `json:"per"`
	PBR             string    `json:"pbr"`
	Revenue         string    `json:"revenue"`
	OperatingProfit string    `json:"operating_profit"`
	ForeignNet      string    `json:"foreign_net"`
	InstitutionNet  string    `json:"institution_net"`
	IndividualNet   string    `json:"individual_net"`
	TrendDate       string    `json:"trend_date"`
	UpdatedAt       time.Time `json:"updated_at"`

	// World stock fields
	IsWorldStock bool   `json:"is_world_stock,omitempty"`
	ReutersCode  string `json:"reuters_code,omitempty"`
	Currency     string `json:"currency,omitempty"` // e.g. "USD"
	EBITDA       string `json:"ebitda,omitempty"`
	SymbolCode   string `json:"symbol_code,omitempty"` // e.g. "GOOGL"
}

// StockSearchResult represents a resolved stock from autocomplete.
type StockSearchResult struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Market      string `json:"market"`
	NationCode  string `json:"nation_code,omitempty"`  // e.g. "KOR", "USA"
	ReutersCode string `json:"reuters_code,omitempty"` // e.g. "GOOGL.O" (world stocks only)
}

type stockAutocompleteItem struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	TypeCode    string `json:"typeCode"`
	NationCode  string `json:"nationCode"`
	ReutersCode string `json:"reutersCode"`
}

// ErrStockNotFound is returned when a stock cannot be resolved from user input.
var ErrStockNotFound = errors.New("stock not found")

// NaverStock fetches stock data from Naver Finance unofficial APIs.
type NaverStock struct {
	client       *BreakerHTTPClient
	aliases      map[string]string
	localResults map[string]StockSearchResult
	logger       *slog.Logger
}

// NewNaverStock creates a new NaverStock provider.
func NewNaverStock(logger *slog.Logger) *NaverStock {
	if logger == nil {
		logger = slog.Default()
	}
	return &NaverStock{
		client:       DefaultBreakerClient(5*time.Second, "naver_stock", logger),
		aliases:      defaultAliases(),
		localResults: defaultLocalResults(),
		logger:       logger.With("component", "naver_stock"),
	}
}

// ValidateWorldStockCodes checks world stock entries in localResults against the
// Naver autocomplete API and corrects stale Reuters codes. Call once at startup.
func (n *NaverStock) ValidateWorldStockCodes(ctx context.Context) {
	for key, result := range n.localResults {
		if result.NationCode == "" || result.NationCode == "KOR" {
			continue
		}
		if result.ReutersCode == "" {
			continue
		}

		// Use the stock code (e.g. "KO") as the autocomplete query.
		acResult, err := n.searchAutocomplete(ctx, result.Code)
		if err != nil {
			n.logger.Warn("world stock code validation failed, keeping existing",
				"key", key, "code", result.Code, "error", err)
			continue
		}

		if acResult.ReutersCode != "" && acResult.ReutersCode != result.ReutersCode {
			n.logger.Info("world stock reuters code corrected",
				"key", key,
				"old", result.ReutersCode,
				"new", acResult.ReutersCode)
			result.ReutersCode = acResult.ReutersCode
			n.localResults[key] = result
		}
	}
}

// Resolve converts user input (alias, stock name, or code) into a StockSearchResult.
// Flow: alias dict → Naver autocomplete → return first KOR stock result.
func (n *NaverStock) Resolve(ctx context.Context, input string) (StockSearchResult, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return StockSearchResult{}, fmt.Errorf("empty input")
	}

	if !looksLikeDomesticStockCode(input) {
		if result, ok := n.ResolveLocalOnly(input); ok {
			return result, nil
		}
	}

	// Step 1: Check local alias dictionary.
	query := input
	if canonical, ok := n.aliases[input]; ok {
		query = canonical
	}

	// Step 2: Call Naver autocomplete API.
	return n.searchAutocomplete(ctx, query)
}

// ResolveBareKRXExact resolves only exact bare-query KRX stock names/codes.
// It intentionally does not use local aliases or fuzzy/prefix matching.
func (n *NaverStock) ResolveBareKRXExact(ctx context.Context, input string) (StockSearchResult, error) {
	query := strings.TrimSpace(input)
	if query == "" {
		return StockSearchResult{}, fmt.Errorf("empty input")
	}

	items, err := n.fetchAutocompleteItems(ctx, query)
	if err != nil {
		return StockSearchResult{}, err
	}

	if result, ok := pickExactKRXAutocomplete(items, query); ok {
		return result, nil
	}

	return StockSearchResult{}, fmt.Errorf("%w for %q", ErrStockNotFound, query)
}

// ResolveLocalOnly resolves user input using only local aliases or explicit local keys.
// Supports both Korean/world stocks that exist in the curated local registry.
func (n *NaverStock) ResolveLocalOnly(input string) (StockSearchResult, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return StockSearchResult{}, false
	}

	if result, ok := n.localResults[input]; ok {
		return result, true
	}
	if canonical, ok := n.aliases[input]; ok {
		if result, ok := n.localResults[canonical]; ok {
			return result, true
		}
	}

	// Check case-insensitive for English ticker symbols (e.g., "aapl" → "AAPL").
	upper := strings.ToUpper(input)
	if upper != input {
		if result, ok := n.localResults[upper]; ok {
			return result, true
		}
		if canonical, ok := n.aliases[upper]; ok {
			if result, ok := n.localResults[canonical]; ok {
				return result, true
			}
		}
	}

	return StockSearchResult{}, false
}

// FetchQuote fetches complete stock data for a given stock code.
// Calls integration + finance/annual APIs in parallel.
func (n *NaverStock) FetchQuote(ctx context.Context, code string) (StockQuote, error) {
	type integrationResult struct {
		quote StockQuote
		err   error
	}
	type financeResult struct {
		revenue         string
		operatingProfit string
		err             error
	}

	intCh := make(chan integrationResult, 1)
	finCh := make(chan financeResult, 1)

	go func() {
		q, err := n.fetchIntegration(ctx, code)
		intCh <- integrationResult{q, err}
	}()

	go func() {
		rev, op, err := n.fetchFinance(ctx, code)
		finCh <- financeResult{rev, op, err}
	}()

	intRes := <-intCh
	if intRes.err != nil {
		return StockQuote{}, intRes.err
	}

	finRes := <-finCh
	if finRes.err != nil {
		n.logger.Warn("finance fetch failed, continuing without", "code", code, "error", finRes.err)
	} else {
		intRes.quote.Revenue = finRes.revenue
		intRes.quote.OperatingProfit = finRes.operatingProfit
	}

	intRes.quote.UpdatedAt = time.Now()
	return intRes.quote, nil
}

// searchAutocomplete calls Naver stock autocomplete API.
// Returns Korean stocks first, world stocks as fallback.
func (n *NaverStock) searchAutocomplete(ctx context.Context, query string) (StockSearchResult, error) {
	items, err := n.fetchAutocompleteItems(ctx, query)
	if err != nil {
		return StockSearchResult{}, err
	}

	// Priority: Korean stocks first, then world stocks.
	var worldFallback *StockSearchResult
	for _, item := range items {
		if item.NationCode == "KOR" {
			return StockSearchResult{
				Code:       item.Code,
				Name:       item.Name,
				Market:     item.TypeCode,
				NationCode: item.NationCode,
			}, nil
		}
		// Remember first world stock as fallback.
		if worldFallback == nil && item.NationCode != "" {
			result := StockSearchResult{
				Code:        item.Code,
				Name:        item.Name,
				Market:      item.TypeCode,
				NationCode:  item.NationCode,
				ReutersCode: item.ReutersCode,
			}
			worldFallback = &result
		}
	}

	if worldFallback != nil {
		return *worldFallback, nil
	}

	return StockSearchResult{}, fmt.Errorf("%w for %q", ErrStockNotFound, query)
}

func (n *NaverStock) fetchAutocompleteItems(ctx context.Context, query string) ([]stockAutocompleteItem, error) {
	u := fmt.Sprintf("https://ac.stock.naver.com/ac?q=%s&target=stock&st=111", url.QueryEscape(query))

	body, err := n.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("autocomplete request: %w", err)
	}

	var resp struct {
		Items []stockAutocompleteItem `json:"items"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("autocomplete parse: %w", err)
	}

	return resp.Items, nil
}

func pickExactKRXAutocomplete(items []stockAutocompleteItem, query string) (StockSearchResult, bool) {
	query = strings.TrimSpace(query)
	if query == "" {
		return StockSearchResult{}, false
	}

	for _, item := range items {
		if item.NationCode != "KOR" {
			continue
		}
		if item.Name != query && item.Code != query {
			continue
		}
		return StockSearchResult{
			Code:       item.Code,
			Name:       item.Name,
			Market:     item.TypeCode,
			NationCode: item.NationCode,
		}, true
	}

	return StockSearchResult{}, false
}

// fetchIntegration calls the Naver stock integration API.
func (n *NaverStock) fetchIntegration(ctx context.Context, code string) (StockQuote, error) {
	u := fmt.Sprintf("https://m.stock.naver.com/api/stock/%s/integration", code)

	body, err := n.doGet(ctx, u)
	if err != nil {
		return StockQuote{}, fmt.Errorf("integration request: %w", err)
	}

	var resp struct {
		StockName  string `json:"stockName"`
		ItemCode   string `json:"itemCode"`
		TotalInfos []struct {
			Code  string `json:"code"`
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"totalInfos"`
		DealTrendInfos []struct {
			Bizdate             string `json:"bizdate"`
			ForeignerPureBuyQt  string `json:"foreignerPureBuyQuant"`
			OrganPureBuyQt      string `json:"organPureBuyQuant"`
			IndividualPureBuyQt string `json:"individualPureBuyQuant"`
		} `json:"dealTrendInfos"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return StockQuote{}, fmt.Errorf("integration parse: %w", err)
	}

	q := StockQuote{
		Code: resp.ItemCode,
		Name: resp.StockName,
	}

	for _, info := range resp.TotalInfos {
		switch info.Code {
		case "lastClosePrice":
			q.PrevClose = info.Value
		case "marketValue":
			q.MarketCap = info.Value
		case "per":
			q.PER = info.Value
		case "pbr":
			q.PBR = info.Value
		}
	}

	// Get current price from basic API data embedded or use a separate call.
	// The integration API doesn't directly expose current price with direction,
	// so we fetch basic info to get closePrice and fluctuationsRatio.
	basic, err := n.fetchBasic(ctx, code)
	if err == nil {
		q.Price = basic.price
		q.Change = basic.change
		q.ChangePercent = basic.changePercent
		q.ChangeDirection = basic.direction
		q.Market = basic.market
	}

	if len(resp.DealTrendInfos) > 0 {
		trend := resp.DealTrendInfos[0]
		q.ForeignNet = trend.ForeignerPureBuyQt
		q.InstitutionNet = trend.OrganPureBuyQt
		q.IndividualNet = trend.IndividualPureBuyQt
		q.TrendDate = formatBizdate(trend.Bizdate)
	}

	return q, nil
}

type basicInfo struct {
	price         string
	change        string
	changePercent string
	direction     string
	market        string
}

// BasicInfo is the exported version of basicInfo for use by other packages.
type BasicInfo struct {
	Name          string
	Price         string
	Change        string
	ChangePercent string
	Direction     string
	Market        string
}

// FetchBasicPublic fetches basic stock info (price, change, market) for a single stock code.
func (n *NaverStock) FetchBasicPublic(ctx context.Context, code string) (BasicInfo, error) {
	u := fmt.Sprintf("https://m.stock.naver.com/api/stock/%s/basic", code)

	body, err := n.doGet(ctx, u)
	if err != nil {
		return BasicInfo{}, fmt.Errorf("basic request: %w", err)
	}

	var resp struct {
		StockName                   string `json:"stockName"`
		ClosePrice                  string `json:"closePrice"`
		CompareToPreviousClosePrice string `json:"compareToPreviousClosePrice"`
		FluctuationsRatio           string `json:"fluctuationsRatio"`
		CompareToPreviousPrice      struct {
			Name string `json:"name"`
		} `json:"compareToPreviousPrice"`
		StockExchangeName string `json:"stockExchangeName"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return BasicInfo{}, fmt.Errorf("basic parse: %w", err)
	}

	return BasicInfo{
		Name:          resp.StockName,
		Price:         resp.ClosePrice,
		Change:        resp.CompareToPreviousClosePrice,
		ChangePercent: resp.FluctuationsRatio,
		Direction:     resp.CompareToPreviousPrice.Name,
		Market:        resp.StockExchangeName,
	}, nil
}

// fetchBasic calls the Naver stock basic API for current price info.
func (n *NaverStock) fetchBasic(ctx context.Context, code string) (basicInfo, error) {
	u := fmt.Sprintf("https://m.stock.naver.com/api/stock/%s/basic", code)

	body, err := n.doGet(ctx, u)
	if err != nil {
		return basicInfo{}, fmt.Errorf("basic request: %w", err)
	}

	var resp struct {
		ClosePrice                  string `json:"closePrice"`
		CompareToPreviousClosePrice string `json:"compareToPreviousClosePrice"`
		FluctuationsRatio           string `json:"fluctuationsRatio"`
		CompareToPreviousPrice      struct {
			Name string `json:"name"`
		} `json:"compareToPreviousPrice"`
		StockExchangeName string `json:"stockExchangeName"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return basicInfo{}, fmt.Errorf("basic parse: %w", err)
	}

	return basicInfo{
		price:         resp.ClosePrice,
		change:        resp.CompareToPreviousClosePrice,
		changePercent: resp.FluctuationsRatio,
		direction:     resp.CompareToPreviousPrice.Name,
		market:        resp.StockExchangeName,
	}, nil
}

// fetchFinance calls the Naver stock finance/annual API.
func (n *NaverStock) fetchFinance(ctx context.Context, code string) (revenue, operatingProfit string, err error) {
	u := fmt.Sprintf("https://m.stock.naver.com/api/stock/%s/finance/annual", code)

	body, err := n.doGet(ctx, u)
	if err != nil {
		return "", "", fmt.Errorf("finance request: %w", err)
	}

	var resp struct {
		FinanceInfo struct {
			TrTitleList []struct {
				IsConsensus string `json:"isConsensus"`
				Key         string `json:"key"`
			} `json:"trTitleList"`
			RowList []struct {
				Title   string `json:"title"`
				Columns map[string]struct {
					Value string `json:"value"`
				} `json:"columns"`
			} `json:"rowList"`
		} `json:"financeInfo"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", "", fmt.Errorf("finance parse: %w", err)
	}

	// Find the latest non-consensus period key.
	var latestKey string
	for _, title := range resp.FinanceInfo.TrTitleList {
		if title.IsConsensus == "N" {
			latestKey = title.Key
		}
	}
	if latestKey == "" {
		return "", "", fmt.Errorf("no finance period found")
	}

	for _, row := range resp.FinanceInfo.RowList {
		col, ok := row.Columns[latestKey]
		if !ok {
			continue
		}
		switch row.Title {
		case "매출액":
			revenue = col.Value
		case "영업이익":
			operatingProfit = col.Value
		}
	}

	return revenue, operatingProfit, nil
}

// doGet performs a GET request and returns the response body.
func (n *NaverStock) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 10) AppleWebKit/537.36")

	resp, err := n.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	return io.ReadAll(resp.Body)
}

// formatBizdate converts "20260313" to "03.13".
func formatBizdate(d string) string {
	if len(d) != 8 {
		return d
	}
	return d[4:6] + "." + d[6:8]
}

// FetchWorldQuote fetches complete stock data for a world stock using its reuters code.
// Calls basic + finance/annual APIs in parallel.
func (n *NaverStock) FetchWorldQuote(ctx context.Context, reutersCode string) (StockQuote, error) {
	if isUnsupportedReferenceID(reutersCode) {
		return StockQuote{}, fmt.Errorf("%w: %s", ErrWorldStockQuoteUnavailable, reutersCode)
	}

	type basicResult struct {
		quote StockQuote
		err   error
	}
	type financeResult struct {
		revenue string
		ebitda  string
		err     error
	}

	bCh := make(chan basicResult, 1)
	fCh := make(chan financeResult, 1)

	go func() {
		q, err := n.fetchWorldBasic(ctx, reutersCode)
		bCh <- basicResult{q, err}
	}()

	go func() {
		rev, ebitda, err := n.fetchWorldFinance(ctx, reutersCode)
		fCh <- financeResult{rev, ebitda, err}
	}()

	bRes := <-bCh
	if bRes.err != nil {
		return StockQuote{}, bRes.err
	}

	fRes := <-fCh
	if fRes.err != nil {
		n.logger.Warn("world finance fetch failed, continuing without", "reuters", reutersCode, "error", fRes.err)
	} else {
		bRes.quote.Revenue = fRes.revenue
		bRes.quote.EBITDA = fRes.ebitda
	}

	bRes.quote.IsWorldStock = true
	bRes.quote.ReutersCode = reutersCode
	bRes.quote.UpdatedAt = time.Now()
	return bRes.quote, nil
}

// fetchWorldBasic calls the Naver world stock basic API.
func (n *NaverStock) fetchWorldBasic(ctx context.Context, reutersCode string) (StockQuote, error) {
	u := fmt.Sprintf("https://api.stock.naver.com/stock/%s/basic", reutersCode)

	body, err := n.doGet(ctx, u)
	if err != nil {
		return StockQuote{}, fmt.Errorf("world basic request: %w", err)
	}

	var resp struct {
		StockName                   string `json:"stockName"`
		SymbolCode                  string `json:"symbolCode"`
		StockExchangeName           string `json:"stockExchangeName"`
		ClosePrice                  string `json:"closePrice"`
		CompareToPreviousClosePrice string `json:"compareToPreviousClosePrice"`
		FluctuationsRatio           string `json:"fluctuationsRatio"`
		CompareToPreviousPrice      struct {
			Name string `json:"name"`
		} `json:"compareToPreviousPrice"`
		CurrencyType struct {
			Code string `json:"code"`
		} `json:"currencyType"`
		StockItemTotalInfos []struct {
			Code      string `json:"code"`
			Key       string `json:"key"`
			Value     string `json:"value"`
			ValueDesc string `json:"valueDesc"`
		} `json:"stockItemTotalInfos"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return StockQuote{}, fmt.Errorf("world basic parse: %w", err)
	}

	q := StockQuote{
		Code:            resp.SymbolCode,
		Name:            resp.StockName,
		Market:          resp.StockExchangeName,
		Price:           resp.ClosePrice,
		Change:          resp.CompareToPreviousClosePrice,
		ChangePercent:   resp.FluctuationsRatio,
		ChangeDirection: resp.CompareToPreviousPrice.Name,
		Currency:        resp.CurrencyType.Code,
		SymbolCode:      resp.SymbolCode,
	}

	// Extract PER, PBR, market cap from stockItemTotalInfos.
	for _, info := range resp.StockItemTotalInfos {
		switch info.Code {
		case "marketValue":
			// Use valueDesc (KRW conversion) if available, otherwise value.
			if info.ValueDesc != "" {
				q.MarketCap = info.ValueDesc
			} else {
				q.MarketCap = info.Value
			}
		case "per":
			q.PER = info.Value
		case "pbr":
			q.PBR = info.Value
		}
	}

	// Calculate prevClose from closePrice - change (basePrice in stockItemTotalInfos
	// is the session's reference price, NOT the previous day's close).
	q.PrevClose = calcPrevClose(resp.ClosePrice, resp.CompareToPreviousClosePrice)

	return q, nil
}

// fetchWorldFinance calls the Naver world stock finance/annual API.
// Returns revenue and EBITDA in KRW 억원 format.
func (n *NaverStock) fetchWorldFinance(ctx context.Context, reutersCode string) (revenue, ebitda string, err error) {
	u := fmt.Sprintf("https://api.stock.naver.com/stock/%s/finance/annual", reutersCode)

	body, err := n.doGet(ctx, u)
	if err != nil {
		return "", "", fmt.Errorf("world finance request: %w", err)
	}

	var resp struct {
		TrTitleList []struct {
			IsConsensus string `json:"isConsensus"`
			Key         string `json:"key"`
		} `json:"trTitleList"`
		RowList []struct {
			Title   string `json:"title"`
			Columns map[string]struct {
				Value string `json:"value"`
				Krw   string `json:"krw"`
			} `json:"columns"`
		} `json:"rowList"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", "", fmt.Errorf("world finance parse: %w", err)
	}

	// Find the latest non-consensus period key.
	var latestKey string
	for _, title := range resp.TrTitleList {
		if title.IsConsensus == "N" {
			latestKey = title.Key
		}
	}
	if latestKey == "" {
		return "", "", fmt.Errorf("no finance period found")
	}

	for _, row := range resp.RowList {
		col, ok := row.Columns[latestKey]
		if !ok {
			continue
		}
		switch row.Title {
		case "매출액":
			// krw field is in millions of KRW. Convert to 억원.
			revenue = convertMillionsToEok(col.Krw)
		case "EBITDA":
			ebitda = convertMillionsToEok(col.Krw)
		}
	}

	return revenue, ebitda, nil
}

// convertMillionsToEok converts a value in millions (e.g. "6,006,687.60") to 억원 units as a string.
// Input: KRW millions (from Naver finance API krw field).
// Output: integer string in 억원 units (e.g. "6006687" → drop decimals, divide by 100 is NOT needed
// because the krw field is already in millions, and 1억 = 100백만, so divide by 100).
// Actually: "6,006,687.60" means 6,006,687.60 백만원 = 60,066.876 억원 = "60,066" in 억원.
func convertMillionsToEok(s string) string {
	if s == "" {
		return ""
	}
	// Remove commas and parse as float.
	cleaned := strings.ReplaceAll(s, ",", "")
	if cleaned == "" || cleaned == "-" {
		return s
	}

	// Parse the number (could be decimal like "6006687.60").
	var millions float64
	_, err := fmt.Sscanf(cleaned, "%f", &millions)
	if err != nil {
		return s
	}

	// 1억 = 100백만, so divide millions by 100 to get 억.
	eok := int64(millions / 100)
	return fmt.Sprintf("%d", eok)
}

// calcPrevClose computes the previous close price from current price and change.
// e.g. price="305.56", change="3.28" → "302.28"
func calcPrevClose(price, change string) string {
	if price == "" || change == "" {
		return ""
	}

	var p, c float64
	pClean := strings.ReplaceAll(price, ",", "")
	cClean := strings.ReplaceAll(change, ",", "")

	if _, err := fmt.Sscanf(pClean, "%f", &p); err != nil {
		return ""
	}
	if _, err := fmt.Sscanf(cClean, "%f", &c); err != nil {
		return ""
	}

	prev := p - c
	if prev < 0 {
		prev = 0
	}

	// Format to match the precision of the original price.
	if strings.Contains(price, ".") {
		parts := strings.SplitN(price, ".", 2)
		decLen := len(parts[1])
		return fmt.Sprintf("%.*f", decLen, prev)
	}
	return fmt.Sprintf("%.0f", prev)
}

// defaultAliases returns a map of popular stock aliases (Korean abbreviations).
func defaultAliases() map[string]string {
	return loadNaverAliases()
}

func defaultLocalResults() map[string]StockSearchResult {
	return loadNaverLocalResults()
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func looksLikeDomesticStockCode(s string) bool {
	return len(s) == 6 && isDigits(s)
}

// ThemeEntry represents a single theme from the Naver theme list API.
type ThemeEntry struct {
	No         int    `json:"no"`
	Name       string `json:"name"`
	TotalCount int    `json:"totalCount"`
}

// ThemeStock represents a single stock within a theme.
type ThemeStock struct {
	Code            string `json:"itemCode"`
	Name            string `json:"stockName"`
	Market          string // derived from Sosok: "0"=KOSPI, "1"=KOSDAQ
	Price           string `json:"closePrice"`
	Change          string `json:"compareToPreviousClosePrice"`
	ChangePercent   string `json:"fluctuationsRatio"`
	ChangeDirection string // derived from compareToPreviousPrice.name
	MarketValue     int64  // parsed from marketValue string (unit: 억)
}

// ThemeDetail holds the full theme info plus its stock list.
type ThemeDetail struct {
	Name   string
	Stocks []ThemeStock
}

// FetchThemeList fetches a single page of themes from the Naver mobile API.
func (n *NaverStock) FetchThemeList(ctx context.Context, page, pageSize int) ([]ThemeEntry, error) {
	u := fmt.Sprintf("https://m.stock.naver.com/api/stocks/theme?page=%d&pageSize=%d", page, pageSize)

	body, err := n.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("theme list request: %w", err)
	}

	var resp struct {
		Groups []ThemeEntry `json:"groups"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("theme list parse: %w", err)
	}

	return resp.Groups, nil
}

// FetchThemeDetail fetches the stock list for a specific theme.
func (n *NaverStock) FetchThemeDetail(ctx context.Context, themeNo int) (ThemeDetail, error) {
	u := fmt.Sprintf("https://m.stock.naver.com/api/stocks/theme/%d?pageSize=100", themeNo)

	body, err := n.doGet(ctx, u)
	if err != nil {
		return ThemeDetail{}, fmt.Errorf("theme detail request: %w", err)
	}

	var resp struct {
		GroupInfo struct {
			Name string `json:"name"`
		} `json:"groupInfo"`
		Stocks []struct {
			ItemCode                    string `json:"itemCode"`
			StockName                   string `json:"stockName"`
			Sosok                       string `json:"sosok"`
			ClosePrice                  string `json:"closePrice"`
			CompareToPreviousClosePrice string `json:"compareToPreviousClosePrice"`
			CompareToPreviousPrice      struct {
				Name string `json:"name"`
			} `json:"compareToPreviousPrice"`
			FluctuationsRatio string `json:"fluctuationsRatio"`
			MarketValue       string `json:"marketValue"`
		} `json:"stocks"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return ThemeDetail{}, fmt.Errorf("theme detail parse: %w", err)
	}

	stocks := make([]ThemeStock, 0, len(resp.Stocks))
	for _, s := range resp.Stocks {
		market := "KOSPI"
		if s.Sosok == "1" {
			market = "KOSDAQ"
		}
		stocks = append(stocks, ThemeStock{
			Code:            s.ItemCode,
			Name:            s.StockName,
			Market:          market,
			Price:           s.ClosePrice,
			Change:          s.CompareToPreviousClosePrice,
			ChangePercent:   s.FluctuationsRatio,
			ChangeDirection: s.CompareToPreviousPrice.Name,
			MarketValue:     parseMarketValue(s.MarketValue),
		})
	}

	return ThemeDetail{
		Name:   resp.GroupInfo.Name,
		Stocks: stocks,
	}, nil
}

// parseMarketValue parses a market value string like "1,234" (unit: 억) into int64.
func parseMarketValue(s string) int64 {
	cleaned := strings.ReplaceAll(s, ",", "")
	if cleaned == "" {
		return 0
	}
	var v int64
	fmt.Sscanf(cleaned, "%d", &v)
	return v
}

// IndexQuote holds parsed market index data from Naver Finance APIs.
type IndexQuote struct {
	Code            string    `json:"code"`
	Name            string    `json:"name"`
	Price           string    `json:"price"`
	Change          string    `json:"change"`
	ChangePercent   string    `json:"change_percent"`
	ChangeDirection string    `json:"change_direction"` // RISING, FALLING, FLAT
	MarketStatus    string    `json:"market_status"`    // OPEN, CLOSE
	UpdatedAt       time.Time `json:"updated_at"`
}

// FetchDomesticIndex fetches index data for a Korean market index (KOSPI, KOSDAQ, KPI200).
// Calls m.stock.naver.com/api/index/{code}/basic.
func (n *NaverStock) FetchDomesticIndex(ctx context.Context, code string) (IndexQuote, error) {
	u := fmt.Sprintf("https://m.stock.naver.com/api/index/%s/basic", url.PathEscape(code))

	body, err := n.doGet(ctx, u)
	if err != nil {
		return IndexQuote{}, fmt.Errorf("domestic index request: %w", err)
	}

	var resp struct {
		IndexName                   string `json:"indexName"`
		ClosePrice                  string `json:"closePrice"`
		CompareToPreviousClosePrice string `json:"compareToPreviousClosePrice"`
		FluctuationsRatio           string `json:"fluctuationsRatio"`
		CompareToPreviousPrice      struct {
			Name string `json:"name"`
		} `json:"compareToPreviousPrice"`
		MarketStatus string `json:"marketStatus"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return IndexQuote{}, fmt.Errorf("domestic index parse: %w", err)
	}

	return IndexQuote{
		Code:            code,
		Name:            resp.IndexName,
		Price:           resp.ClosePrice,
		Change:          resp.CompareToPreviousClosePrice,
		ChangePercent:   resp.FluctuationsRatio,
		ChangeDirection: resp.CompareToPreviousPrice.Name,
		MarketStatus:    resp.MarketStatus,
		UpdatedAt:       time.Now(),
	}, nil
}

// FetchWorldIndex fetches index data for a world market index (e.g., .IXIC, .DJI, .INX).
// Calls api.stock.naver.com/index/{reutersCode}/basic.
func (n *NaverStock) FetchWorldIndex(ctx context.Context, reutersCode string) (IndexQuote, error) {
	u := fmt.Sprintf("https://api.stock.naver.com/index/%s/basic", url.PathEscape(reutersCode))

	body, err := n.doGet(ctx, u)
	if err != nil {
		return IndexQuote{}, fmt.Errorf("world index request: %w", err)
	}

	var resp struct {
		IndexName                   string `json:"indexName"`
		ReutersCode                 string `json:"reutersCode"`
		ClosePrice                  string `json:"closePrice"`
		CompareToPreviousClosePrice string `json:"compareToPreviousClosePrice"`
		FluctuationsRatio           string `json:"fluctuationsRatio"`
		CompareToPreviousPrice      struct {
			Name string `json:"name"`
		} `json:"compareToPreviousPrice"`
		MarketStatus string `json:"marketStatus"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return IndexQuote{}, fmt.Errorf("world index parse: %w", err)
	}

	name := resp.IndexName
	if name == "" {
		name = reutersCode
	}

	return IndexQuote{
		Code:            reutersCode,
		Name:            name,
		Price:           resp.ClosePrice,
		Change:          resp.CompareToPreviousClosePrice,
		ChangePercent:   resp.FluctuationsRatio,
		ChangeDirection: resp.CompareToPreviousPrice.Name,
		MarketStatus:    resp.MarketStatus,
		UpdatedAt:       time.Now(),
	}, nil
}
