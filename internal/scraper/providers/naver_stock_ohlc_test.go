package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNaverStockOHLC_FetchDomestic(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`
[['날짜', '시가', '고가', '저가', '종가', '거래량', '외국인소진율'],
["20260327", 172100, 181700, 172000, 179700, 29138966, 48.62],
["20260330", 171000, 176650, 170600, 176300, 22269147, 48.62],
["20260331", 170000, 174700, 167500, 169400, 29650634, 48.62]
]
`))
	}))
	defer srv.Close()

	n := NewNaverStockOHLC(nil)
	n.now = func() time.Time {
		return time.Date(2026, 3, 31, 9, 0, 0, 0, time.UTC)
	}
	n.client.SetTransport(roundTripFunc(func(req *http.Request) *http.Response {
		req.URL.Scheme = "http"
		req.URL.Host = srv.Listener.Addr().String()
		resp, _ := http.DefaultTransport.RoundTrip(req)
		return resp
	}))

	data, err := n.FetchDomestic(context.Background(), "005930", Timeframe1M)
	if err != nil {
		t.Fatalf("FetchDomestic: %v", err)
	}

	if len(data.Points) != 3 {
		t.Fatalf("got %d points, want 3", len(data.Points))
	}
	if data.Symbol != "005930" {
		t.Fatalf("symbol = %q, want 005930", data.Symbol)
	}

	p := data.Points[0]
	if p.Open != 172100 {
		t.Errorf("open = %f, want 172100", p.Open)
	}
	if p.Close != 179700 {
		t.Errorf("close = %f, want 179700", p.Close)
	}
	if p.Volume != 29138966 {
		t.Errorf("volume = %f, want 29138966", p.Volume)
	}
}

func TestNaverStockOHLC_FetchDomestic_ThreeMonthWindowAnchorsToLookupDate(t *testing.T) {
	t.Parallel()

	var requestedStart string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedStart = r.URL.Query().Get("startTime")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`
[['날짜', '시가', '고가', '저가', '종가', '거래량', '외국인소진율'],
["20260105", 1, 1, 1, 1, 10, 48.62],
["20260106", 2, 2, 2, 2, 20, 48.62],
["20260406", 3, 3, 3, 3, 30, 48.62]
]
`))
	}))
	defer srv.Close()

	n := NewNaverStockOHLC(nil)
	n.now = func() time.Time {
		return time.Date(2026, 4, 6, 15, 4, 5, 0, time.FixedZone("KST", 9*60*60))
	}
	n.client.SetTransport(roundTripFunc(func(req *http.Request) *http.Response {
		req.URL.Scheme = "http"
		req.URL.Host = srv.Listener.Addr().String()
		resp, _ := http.DefaultTransport.RoundTrip(req)
		return resp
	}))

	data, err := n.FetchDomestic(context.Background(), "005930", Timeframe3M)
	if err != nil {
		t.Fatalf("FetchDomestic(3M): %v", err)
	}

	if requestedStart != "20260106" {
		t.Fatalf("startTime = %q, want 20260106", requestedStart)
	}
	if len(data.Points) != 2 {
		t.Fatalf("got %d points, want 2", len(data.Points))
	}
	if got := data.Points[0].Time.Format("20060102"); got != "20260106" {
		t.Fatalf("first point date = %s, want 20260106", got)
	}
}

func TestNaverStockOHLC_FetchWorld(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if call == 1 {
			w.Write([]byte(`[
				{"localTradedAt":"2026-03-30T16:00:00-04:00","closePrice":"246.63","openPrice":"250.07","highPrice":"250.87","lowPrice":"245.51"},
				{"localTradedAt":"2026-03-28T16:00:00-04:00","closePrice":"248.80","openPrice":"247.00","highPrice":"249.50","lowPrice":"246.00"}
			]`))
		} else {
			w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()

	n := NewNaverStockOHLC(nil)
	n.now = func() time.Time {
		return time.Date(2026, 3, 30, 9, 0, 0, 0, time.UTC)
	}
	n.client.SetTransport(roundTripFunc(func(req *http.Request) *http.Response {
		req.URL.Scheme = "http"
		req.URL.Host = srv.Listener.Addr().String()
		resp, _ := http.DefaultTransport.RoundTrip(req)
		return resp
	}))

	data, err := n.FetchWorld(context.Background(), "AAPL.O", Timeframe1W)
	if err != nil {
		t.Fatalf("FetchWorld: %v", err)
	}

	if len(data.Points) != 2 {
		t.Fatalf("got %d points, want 2", len(data.Points))
	}

	// Should be sorted chronologically
	if !data.Points[0].Time.Before(data.Points[1].Time) {
		t.Fatal("points not sorted chronologically")
	}

	p := data.Points[1] // newer
	if p.Close != 246.63 {
		t.Errorf("close = %f, want 246.63", p.Close)
	}
	if p.Volume != 0 {
		t.Errorf("volume = %f, want 0 (not available)", p.Volume)
	}
}

func TestNaverStockOHLC_FetchWorld_ThreeMonthWindowAnchorsToLookupDate(t *testing.T) {
	t.Parallel()

	var pageCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		pageCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case "1":
			_, _ = w.Write([]byte(`[
				{"localTradedAt":"2026-04-06T16:00:00-04:00","closePrice":"246.63","openPrice":"250.07","highPrice":"250.87","lowPrice":"245.51"},
				{"localTradedAt":"2026-03-31T16:00:00-04:00","closePrice":"240.00","openPrice":"239.00","highPrice":"241.00","lowPrice":"238.00"}
			]`))
		case "2":
			_, _ = w.Write([]byte(`[
				{"localTradedAt":"2026-01-06T16:00:00-05:00","closePrice":"200.00","openPrice":"198.00","highPrice":"201.00","lowPrice":"197.00"},
				{"localTradedAt":"2026-01-05T16:00:00-05:00","closePrice":"199.00","openPrice":"197.00","highPrice":"200.00","lowPrice":"196.00"}
			]`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()

	n := NewNaverStockOHLC(nil)
	n.now = func() time.Time {
		return time.Date(2026, 4, 6, 9, 0, 0, 0, time.UTC)
	}
	n.client.SetTransport(roundTripFunc(func(req *http.Request) *http.Response {
		req.URL.Scheme = "http"
		req.URL.Host = srv.Listener.Addr().String()
		resp, _ := http.DefaultTransport.RoundTrip(req)
		return resp
	}))

	data, err := n.FetchWorld(context.Background(), "AAPL.O", Timeframe3M)
	if err != nil {
		t.Fatalf("FetchWorld(3M): %v", err)
	}

	if len(data.Points) != 3 {
		var dates []string
		for _, p := range data.Points {
			dates = append(dates, p.Time.Format("2006-01-02"))
		}
		t.Fatalf("got %d points (%s), want 3 in exact anchored range", len(data.Points), strings.Join(dates, ", "))
	}
	if got := data.Points[0].Time.Format("2006-01-02"); got != "2026-01-06" {
		t.Fatalf("first point date = %s, want 2026-01-06", got)
	}
	if got := data.Points[len(data.Points)-1].Time.Format("2006-01-02"); got != "2026-04-06" {
		t.Fatalf("last point date = %s, want 2026-04-06", got)
	}
	if pageCalls.Load() < 2 {
		t.Fatalf("expected at least 2 page fetches, got %d", pageCalls.Load())
	}
}

func TestParseFchartResponse(t *testing.T) {
	t.Parallel()

	body := `
[['날짜', '시가', '고가', '저가', '종가', '거래량', '외국인소진율'],
["20260327", 172100, 181700, 172000, 179700, 29138966, 48.62],
["20260330", 171000, 176650, 170600, 176300, 22269147, 48.62],
]
`
	points, err := parseFchartResponse(body)
	if err != nil {
		t.Fatalf("parseFchartResponse: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("got %d points, want 2", len(points))
	}
	if points[0].Open != 172100 {
		t.Errorf("points[0].Open = %f, want 172100", points[0].Open)
	}
}

func TestParseWorldDate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		year  int
	}{
		{"2026-03-30T16:00:00-04:00", 2026},
		{"2026-03-30", 2026},
		{"invalid", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := parseWorldDate(tt.input)
			if tt.year == 0 {
				if !got.IsZero() {
					t.Fatalf("expected zero time for %q", tt.input)
				}
			} else if got.Year() != tt.year {
				t.Fatalf("year = %d, want %d", got.Year(), tt.year)
			}
		})
	}
}

func TestParseNaverPrice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  float64
	}{
		{"246.63", 246.63},
		{"169,400", 169400},
		{"1,234,567", 1234567},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := parseNaverPrice(tt.input)
			if got != tt.want {
				t.Fatalf("parseNaverPrice(%q) = %f, want %f", tt.input, got, tt.want)
			}
		})
	}
}
