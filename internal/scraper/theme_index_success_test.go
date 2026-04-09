package scraper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/shift-click/masterbot/internal/scraper/providers"
)

type rtFn func(*http.Request) (*http.Response, error)

func (f rtFn) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func setPrivateHTTPClient(target any, client *http.Client) {
	wrapped := providers.NewBreakerHTTPClient(client, "test", nil)
	field := reflect.ValueOf(target).Elem().FieldByName("client")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(wrapped))
}

func newJSONResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestThemeIndexSuccessPathsWithMockedProviders(t *testing.T) {
	naver := providers.NewNaverStock(nil)
	judal := providers.NewJudalScraper(nil)

	client := &http.Client{
		Transport: rtFn(func(req *http.Request) (*http.Response, error) {
			switch {
			case strings.Contains(req.URL.String(), "/api/stocks/theme?page="):
				return newJSONResponse(`{"groups":[{"no":1,"name":"반도체","totalCount":1},{"no":2,"name":"전기차","totalCount":1}]}`), nil
			case strings.Contains(req.URL.String(), "/api/stocks/theme/1?pageSize=100"):
				return newJSONResponse(`{
					"groupInfo":{"name":"반도체"},
					"stocks":[
						{"itemCode":"0001","stockName":"A","sosok":"0","closePrice":"100","compareToPreviousClosePrice":"1","compareToPreviousPrice":{"name":"상승"},"fluctuationsRatio":"1.0","marketValue":"1,000"},
						{"itemCode":"0002","stockName":"B","sosok":"1","closePrice":"200","compareToPreviousClosePrice":"-1","compareToPreviousPrice":{"name":"하락"},"fluctuationsRatio":"-0.5","marketValue":"2,000"}
					]
				}`), nil
			case strings.Contains(req.URL.String(), "view=themeList"):
				return newJSONResponse(`<a href="?view=stockList&themeIdx=12" title="반도체 테마토크"></a><a href="?view=stockList&themeIdx=34" title="전기차 테마토크"></a>`), nil
			case strings.Contains(req.URL.String(), "view=stockList&themeIdx=12"):
				return newJSONResponse(`<a href="?code=005930"></a><a href="?code=000660"></a><a href="?code=005930"></a>`), nil
			default:
				return nil, fmt.Errorf("unexpected request url: %s", req.URL.String())
			}
		}),
	}
	setPrivateHTTPClient(naver, client)
	setPrivateHTTPClient(judal, client)

	index := NewThemeIndex(naver, judal, nil)

	index.refreshNaver(context.Background())
	if !index.naverReady || len(index.naverEntries) != 2 {
		t.Fatalf("naver refresh failed: ready=%v entries=%d", index.naverReady, len(index.naverEntries))
	}

	index.refreshJudal(context.Background())
	if !index.judalReady || len(index.judalEntries) != 2 {
		t.Fatalf("judal refresh failed: ready=%v entries=%d", index.judalReady, len(index.judalEntries))
	}

	codes, err := index.FetchJudalStockCodes(context.Background(), 12)
	if err != nil {
		t.Fatalf("FetchJudalStockCodes() error = %v", err)
	}
	if len(codes) != 2 || codes[0] != "005930" || codes[1] != "000660" {
		t.Fatalf("unexpected stock codes: %v", codes)
	}

	detail, err := index.FetchDetail(context.Background(), 1)
	if err != nil {
		t.Fatalf("FetchDetail() error = %v", err)
	}
	if detail.Name != "반도체" || len(detail.Stocks) != 2 {
		t.Fatalf("unexpected detail: %+v", detail)
	}
	// Sorted descending by market value.
	if detail.Stocks[0].Code != "0002" || detail.Stocks[0].Market != "KOSDAQ" {
		t.Fatalf("expected sorted KOSDAQ stock first, got %+v", detail.Stocks[0])
	}
}
