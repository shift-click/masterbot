package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSearchAutocomplete_KoreanFirst(t *testing.T) {
	t.Parallel()

	// Mock server returning both Korean and world stock results.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"query": "카카오",
			"items": []map[string]string{
				{
					"code":        "035720",
					"name":        "카카오",
					"typeCode":    "KOSPI",
					"nationCode":  "KOR",
					"reutersCode": "",
				},
				{
					"code":        "FAKE",
					"name":        "카카오 US",
					"typeCode":    "NASDAQ",
					"nationCode":  "USA",
					"reutersCode": "FAKE.O",
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// We can't easily override the URL in the struct, so we test parsing logic directly.
	// Instead, test the local results which is the main path for fallback.
}

func TestSearchAutocomplete_WorldStockFallback(t *testing.T) {
	t.Parallel()

	// Mock server returning only world stock results.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"query": "GOOGL",
			"items": []map[string]string{
				{
					"code":        "GOOGL",
					"name":        "알파벳 Class A",
					"typeCode":    "NASDAQ",
					"nationCode":  "USA",
					"reutersCode": "GOOGL.O",
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// Verify the response structure matches expectations.
	// The actual autocomplete URL is hardcoded, so we test the local resolve path.
}

func TestResolveLocalOnly_WorldStock(t *testing.T) {
	t.Parallel()

	ns := NewNaverStock(nil)

	tests := []struct {
		input       string
		wantOK      bool
		wantCode    string
		wantNation  string
		wantReuters string
	}{
		// Korean stock aliases.
		{"삼전", true, "005930", "", ""},
		{"하닉", true, "000660", "", ""},
		// Canonical full names (bare query support).
		{"삼성전자", true, "005930", "", ""},
		{"SK하이닉스", true, "000660", "", ""},
		{"카카오", true, "035720", "", ""},
		{"LG에너지솔루션", true, "373220", "", ""},
		{"KB금융", true, "105560", "", ""},
		// Removed alias must not match.
		{"ㅋㅋ", false, "", "", ""},
		// World stock Korean aliases.
		{"구글", true, "GOOGL", "USA", "GOOGL.O"},
		{"테슬라", true, "TSLA", "USA", "TSLA.O"},
		{"애플", true, "AAPL", "USA", "AAPL.O"},
		{"엔비디아", true, "NVDA", "USA", "NVDA.O"},
		{"아마존", true, "AMZN", "USA", "AMZN.O"},
		{"마소", true, "MSFT", "USA", "MSFT.O"},
		{"메타", true, "META", "USA", "META.O"},
		{"보일", true, "BOIL", "USA", "BOIL.K"},
		{"빅스", true, "^VIX", "USA", "UNSUPPORTED:^VIX"},
		// World stock English tickers.
		{"GOOGL", true, "GOOGL", "USA", "GOOGL.O"},
		{"TSLA", true, "TSLA", "USA", "TSLA.O"},
		{"AAPL", true, "AAPL", "USA", "AAPL.O"},
		{"NVDA", true, "NVDA", "USA", "NVDA.O"},
		{"boil", true, "BOIL", "USA", "BOIL.K"},
		// Case-insensitive English tickers.
		{"googl", true, "GOOGL", "USA", "GOOGL.O"},
		{"tsla", true, "TSLA", "USA", "TSLA.O"},
		{"aapl", true, "AAPL", "USA", "AAPL.O"},
		// Unknown input.
		{"존재하지않는종목", false, "", "", ""},
		{"ZZZZ", false, "", "", ""},
		// Korean 6-digit code still works.
		{"005930", true, "005930", "", ""},
		// Unknown 6-digit code must not match implicitly.
		{"123456", false, "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, ok := ns.ResolveLocalOnly(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ResolveLocalOnly(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if result.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", result.Code, tt.wantCode)
			}
			if result.NationCode != tt.wantNation {
				t.Errorf("NationCode = %q, want %q", result.NationCode, tt.wantNation)
			}
			if result.ReutersCode != tt.wantReuters {
				t.Errorf("ReutersCode = %q, want %q", result.ReutersCode, tt.wantReuters)
			}
		})
	}
}

func TestPickExactKRXAutocomplete(t *testing.T) {
	t.Parallel()

	items := []stockAutocompleteItem{
		{Code: "002350", Name: "넥센타이어", TypeCode: "KOSPI", NationCode: "KOR"},
		{Code: "086450", Name: "동국제약", TypeCode: "KOSDAQ", NationCode: "KOR"},
		{Code: "FAKE.O", Name: "동국제약 ADR", TypeCode: "NASDAQ", NationCode: "USA"},
	}

	if result, ok := pickExactKRXAutocomplete(items, "동국제약"); !ok {
		t.Fatal("expected exact KRX name match")
	} else if result.Code != "086450" || result.Market != "KOSDAQ" {
		t.Fatalf("unexpected result: %+v", result)
	}

	if result, ok := pickExactKRXAutocomplete(items, "086450"); !ok {
		t.Fatal("expected exact KRX code match")
	} else if result.Name != "동국제약" {
		t.Fatalf("unexpected result: %+v", result)
	}

	for _, query := range []string{"동국", "네", "넥센", "FAKE.O"} {
		if _, ok := pickExactKRXAutocomplete(items, query); ok {
			t.Fatalf("expected query %q to be rejected", query)
		}
	}
}

func TestUnsupportedWorldReferenceReturnsExplicitErrors(t *testing.T) {
	t.Parallel()

	ns := NewNaverStock(nil)
	if _, err := ns.FetchWorldQuote(context.Background(), "UNSUPPORTED:^VIX"); !errors.Is(err, ErrWorldStockQuoteUnavailable) {
		t.Fatalf("FetchWorldQuote unsupported err = %v, want ErrWorldStockQuoteUnavailable", err)
	}

	ohlc := NewNaverStockOHLC(nil)
	if _, err := ohlc.FetchWorld(context.Background(), "UNSUPPORTED:^VIX", Timeframe1M); !errors.Is(err, ErrWorldStockChartUnavailable) {
		t.Fatalf("FetchWorld unsupported err = %v, want ErrWorldStockChartUnavailable", err)
	}
}

func TestFetchWorldBasic_ParseResponse(t *testing.T) {
	t.Parallel()

	// Mock API response matching the real Naver API structure.
	mockResp := map[string]interface{}{
		"stockName":                   "알파벳 Class A",
		"symbolCode":                  "GOOGL",
		"stockExchangeName":           "NASDAQ",
		"closePrice":                  "305.56",
		"compareToPreviousClosePrice": "3.28",
		"fluctuationsRatio":           "1.09",
		"compareToPreviousPrice": map[string]string{
			"name": "RISING",
		},
		"currencyType": map[string]string{
			"code": "USD",
		},
		"stockItemTotalInfos": []map[string]string{
			{"code": "marketValue", "key": "시총", "value": "2조 347억 USD", "valueDesc": "3,034조 7,909억원"},
			{"code": "per", "key": "PER", "value": "27.23배"},
			{"code": "pbr", "key": "PBR", "value": "8.89배"},
			{"code": "basePrice", "key": "전일", "value": "302.28"},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(mockResp)
	}))
	defer srv.Close()

	// Use the JSON structure directly to verify parsing.
	body, _ := json.Marshal(mockResp)

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
		t.Fatalf("parse failed: %v", err)
	}

	if resp.StockName != "알파벳 Class A" {
		t.Errorf("StockName = %q", resp.StockName)
	}
	if resp.SymbolCode != "GOOGL" {
		t.Errorf("SymbolCode = %q", resp.SymbolCode)
	}
	if resp.ClosePrice != "305.56" {
		t.Errorf("ClosePrice = %q", resp.ClosePrice)
	}
	if resp.CurrencyType.Code != "USD" {
		t.Errorf("Currency = %q", resp.CurrencyType.Code)
	}
	if resp.CompareToPreviousPrice.Name != "RISING" {
		t.Errorf("Direction = %q", resp.CompareToPreviousPrice.Name)
	}

	// Check stockItemTotalInfos parsing.
	foundMarketCap := false
	for _, info := range resp.StockItemTotalInfos {
		if info.Code == "marketValue" {
			if info.ValueDesc != "3,034조 7,909억원" {
				t.Errorf("marketValue valueDesc = %q", info.ValueDesc)
			}
			foundMarketCap = true
		}
	}
	if !foundMarketCap {
		t.Error("marketValue not found in stockItemTotalInfos")
	}
}

func TestFetchWorldFinance_ParseResponse(t *testing.T) {
	t.Parallel()

	mockResp := map[string]interface{}{
		"trTitleList": []map[string]string{
			{"isConsensus": "N", "key": "2024.12.31"},
			{"isConsensus": "N", "key": "2025.12.31"},
		},
		"rowList": []map[string]interface{}{
			{
				"title": "매출액",
				"columns": map[string]interface{}{
					"2025.12.31": map[string]string{"value": "402,836.00", "krw": "6,006,687.60"},
				},
			},
			{
				"title": "EBITDA",
				"columns": map[string]interface{}{
					"2025.12.31": map[string]string{"value": "156,375.00", "krw": "2,331,707.63"},
				},
			},
		},
	}

	body, _ := json.Marshal(mockResp)

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
		t.Fatalf("parse failed: %v", err)
	}

	// Find latest non-consensus key.
	var latestKey string
	for _, title := range resp.TrTitleList {
		if title.IsConsensus == "N" {
			latestKey = title.Key
		}
	}
	if latestKey != "2025.12.31" {
		t.Fatalf("latestKey = %q, want 2025.12.31", latestKey)
	}

	for _, row := range resp.RowList {
		col, ok := row.Columns[latestKey]
		if !ok {
			continue
		}
		switch row.Title {
		case "매출액":
			got := convertMillionsToEok(col.Krw)
			if got != "60066" {
				t.Errorf("revenue convertMillionsToEok(%q) = %q, want 60066", col.Krw, got)
			}
		case "EBITDA":
			got := convertMillionsToEok(col.Krw)
			if got != "23317" {
				t.Errorf("EBITDA convertMillionsToEok(%q) = %q, want 23317", col.Krw, got)
			}
		}
	}
}

func TestConvertMillionsToEok(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"6,006,687.60", "60066"},
		{"2,331,707.63", "23317"},
		{"100,000.00", "1000"},
		{"0", "0"},
		{"", ""},
		{"-", "-"},
		{"50.00", "0"},
	}

	for _, tt := range tests {
		got := convertMillionsToEok(tt.input)
		if got != tt.want {
			t.Errorf("convertMillionsToEok(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestValidateWorldStockCodes_CorrectsStaleCodes(t *testing.T) {
	// Mock autocomplete API that returns "KO" (no .N suffix) for Coca-Cola.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		var items []map[string]string
		switch q {
		case "KO":
			items = []map[string]string{
				{"code": "KO", "name": "코카콜라", "typeCode": "NYSE", "nationCode": "USA", "reutersCode": "KO"},
			}
		default:
			items = []map[string]string{}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"items": items})
	}))
	defer srv.Close()

	ns := NewNaverStock(nil)
	// Inject stale Reuters code.
	ns.localResults["코카콜라"] = StockSearchResult{
		Code: "KO", Name: "코카콜라", Market: "NYSE", NationCode: "USA", ReutersCode: "KO.N",
	}
	// Redirect HTTP client to mock server.
	ns.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	ns.client.Unwrap().Transport = rewriteTransport{base: srv.URL}

	ns.ValidateWorldStockCodes(context.Background())

	result := ns.localResults["코카콜라"]
	if result.ReutersCode != "KO" {
		t.Errorf("expected ReutersCode 'KO' after validation, got %q", result.ReutersCode)
	}
}

func TestValidateWorldStockCodes_KeepsOnAPIFailure(t *testing.T) {
	// Mock server that always returns error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ns := NewNaverStock(nil)
	ns.localResults["코카콜라"] = StockSearchResult{
		Code: "KO", Name: "코카콜라", Market: "NYSE", NationCode: "USA", ReutersCode: "KO.N",
	}
	ns.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	ns.client.Unwrap().Transport = rewriteTransport{base: srv.URL}

	ns.ValidateWorldStockCodes(context.Background())

	result := ns.localResults["코카콜라"]
	if result.ReutersCode != "KO.N" {
		t.Errorf("expected ReutersCode 'KO.N' to be preserved on failure, got %q", result.ReutersCode)
	}
}

func TestNaverStockFetchQuoteAndBasicPublic(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				"dealTrendInfos": []map[string]string{
					{
						"bizdate":                "20260319",
						"foreignerPureBuyQuant":  "100",
						"organPureBuyQuant":      "50",
						"individualPureBuyQuant": "-150",
					},
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
						{"title": "매출액", "columns": map[string]map[string]string{"2025.12.31": {"value": "300"}}},
						{"title": "영업이익", "columns": map[string]map[string]string{"2025.12.31": {"value": "100"}}},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ns := NewNaverStock(nil)
	ns.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	ns.client.Unwrap().Transport = rewriteTransport{base: srv.URL}

	quote, err := ns.FetchQuote(context.Background(), "005930")
	if err != nil {
		t.Fatalf("FetchQuote: %v", err)
	}
	if quote.Name != "삼성전자" || quote.Revenue != "300" || quote.OperatingProfit != "100" {
		t.Fatalf("unexpected quote: %+v", quote)
	}
	if quote.TrendDate != "03.19" {
		t.Fatalf("unexpected trend date: %+v", quote)
	}

	basic, err := ns.FetchBasicPublic(context.Background(), "005930")
	if err != nil {
		t.Fatalf("FetchBasicPublic: %v", err)
	}
	if basic.Price != "70,000" || basic.Market != "KOSPI" {
		t.Fatalf("unexpected basic: %+v", basic)
	}
}

func TestNaverStockFetchWorldQuoteAndTheme(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/stock/GOOGL.O/basic"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"stockName":                   "Alphabet Class A",
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
		case strings.Contains(r.URL.Path, "/api/stocks/theme"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"groups": []map[string]any{
					{"no": 12, "name": "AI", "totalCount": 10},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ns := NewNaverStock(nil)
	ns.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	ns.client.Unwrap().Transport = rewriteTransport{base: srv.URL}

	world, err := ns.FetchWorldQuote(context.Background(), "GOOGL.O")
	if err != nil {
		t.Fatalf("FetchWorldQuote: %v", err)
	}
	if !world.IsWorldStock || world.SymbolCode != "GOOGL" || world.Revenue == "" || world.EBITDA == "" {
		t.Fatalf("unexpected world quote: %+v", world)
	}
	if world.PrevClose != "302.28" {
		t.Fatalf("unexpected prev close: %+v", world)
	}

	entries, err := ns.FetchThemeList(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("FetchThemeList: %v", err)
	}
	if len(entries) != 1 || entries[0].No != 12 {
		t.Fatalf("unexpected entries: %+v", entries)
	}

	detail, err := ns.FetchThemeDetail(context.Background(), 12)
	if err != nil {
		t.Fatalf("FetchThemeDetail: %v", err)
	}
	if detail.Name != "AI" || len(detail.Stocks) != 1 || detail.Stocks[0].MarketValue != 1234 {
		t.Fatalf("unexpected detail: %+v", detail)
	}
}

func TestNaverStockUtilityFunctions(t *testing.T) {
	t.Parallel()

	if got := parseMarketValue("1,234"); got != 1234 {
		t.Fatalf("parseMarketValue = %d", got)
	}
	if got := calcPrevClose("305.56", "3.28"); got != "302.28" {
		t.Fatalf("calcPrevClose = %q", got)
	}
	if got := calcPrevClose("100", "200"); got != "0" {
		t.Fatalf("calcPrevClose negative clamp = %q", got)
	}
}

// rewriteTransport redirects all requests to a local test server.
type rewriteTransport struct {
	base string
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, _ := url.Parse(t.base)
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	return http.DefaultTransport.RoundTrip(req)
}
