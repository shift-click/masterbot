package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestUpbitOHLC_Fetch(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if call == 1 {
			// Upbit returns newest first
			w.Write([]byte(`[
				{"market":"KRW-BTC","candle_date_time_kst":"2026-03-31T09:00:00","opening_price":101468000,"high_price":103734000,"low_price":101183000,"trade_price":103215000,"candle_acc_trade_volume":505.30},
				{"market":"KRW-BTC","candle_date_time_kst":"2026-03-30T09:00:00","opening_price":100000000,"high_price":102000000,"low_price":99500000,"trade_price":101468000,"candle_acc_trade_volume":400.10}
			]`))
		} else {
			w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()

	u := NewUpbitOHLC(nil)
	u.client.SetTransport(roundTripFunc(func(req *http.Request) *http.Response {
		req.URL.Scheme = "http"
		req.URL.Host = srv.Listener.Addr().String()
		resp, _ := http.DefaultTransport.RoundTrip(req)
		return resp
	}))

	data, err := u.Fetch(context.Background(), "BTC", Timeframe1W)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if len(data.Points) != 2 {
		t.Fatalf("got %d points, want 2", len(data.Points))
	}
	if data.Symbol != "BTC" {
		t.Fatalf("symbol = %q, want BTC", data.Symbol)
	}

	// Should be sorted chronologically (oldest first)
	if !data.Points[0].Time.Before(data.Points[1].Time) {
		t.Fatal("points not sorted chronologically")
	}

	p := data.Points[0]
	if p.Open != 100000000 {
		t.Errorf("open = %f, want 100000000", p.Open)
	}
	if p.Close != 101468000 {
		t.Errorf("close = %f, want 101468000", p.Close)
	}
}

func TestNextUpbitCursor(t *testing.T) {
	t.Parallel()

	cursor, ok := nextUpbitCursor(upbitCandle{CandleDateTimeKST: "2026-03-31T09:00:00"})
	if !ok {
		t.Fatal("expected valid cursor")
	}
	expected := time.Date(2026, 3, 31, 9, 0, 0, 0, time.UTC).Format(upbitCandleDateTimeLayout)
	if cursor != expected {
		t.Fatalf("cursor = %q, want %q", cursor, expected)
	}

	if _, ok := nextUpbitCursor(upbitCandle{CandleDateTimeKST: "invalid"}); ok {
		t.Fatal("expected invalid cursor parse to fail")
	}
}
