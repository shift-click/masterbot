package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBinanceOHLC_Fetch(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			[1711929600000,"67000.00","67500.00","66800.00","67200.00","100.50",1711935999999,"6744000.00",500,"50.25","3372000.00","0"],
			[1712016000000,"67200.00","68000.00","67000.00","67800.00","120.30",1712022399999,"8157540.00",600,"60.15","4078770.00","0"],
			[1712102400000,"67800.00","68200.00","67500.00","67900.00","90.10",1712108799999,"6117590.00",400,"45.05","3058795.00","0"]
		]`))
	}))
	defer srv.Close()

	b := NewBinanceOHLC(nil)
	b.client.SetTransport(roundTripFunc(func(req *http.Request) *http.Response {
		req.URL.Scheme = "http"
		req.URL.Host = srv.Listener.Addr().String()
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}))

	data, err := b.Fetch(context.Background(), "BTC", Timeframe1W)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if len(data.Points) != 3 {
		t.Fatalf("got %d points, want 3", len(data.Points))
	}
	if data.Symbol != "BTC" {
		t.Fatalf("symbol = %q, want BTC", data.Symbol)
	}

	p := data.Points[0]
	if p.Open != 67000 {
		t.Errorf("open = %f, want 67000", p.Open)
	}
	if p.High != 67500 {
		t.Errorf("high = %f, want 67500", p.High)
	}
	if p.Close != 67200 {
		t.Errorf("close = %f, want 67200", p.Close)
	}
	if p.Volume != 100.50 {
		t.Errorf("volume = %f, want 100.5", p.Volume)
	}
}

func TestBinanceOHLC_Fetch_Error(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"code":-1015,"msg":"Too many requests"}`))
	}))
	defer srv.Close()

	b := NewBinanceOHLC(nil)
	b.client.SetTransport(roundTripFunc(func(req *http.Request) *http.Response {
		req.URL.Scheme = "http"
		req.URL.Host = srv.Listener.Addr().String()
		resp, _ := http.DefaultTransport.RoundTrip(req)
		return resp
	}))

	_, err := b.Fetch(context.Background(), "BTC", Timeframe1W)
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
}

type roundTripFunc func(*http.Request) *http.Response

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}
