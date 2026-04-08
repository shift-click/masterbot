package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestYouTubeURLHelpers(t *testing.T) {
	t.Parallel()

	if !IsYouTubeURL("이 링크 봐줘 https://YouTube.com/watch?v=dQw4w9WgXcQ") {
		t.Fatal("expected IsYouTubeURL true")
	}
	if IsYouTubeURL("https://example.com/watch?v=dQw4w9WgXcQ") {
		t.Fatal("expected non-youtube URL false")
	}

	url := ExtractYouTubeURL("text https://youtu.be/dQw4w9WgXcQ more")
	if !strings.Contains(url, "youtu.be/dQw4w9WgXcQ") {
		t.Fatalf("unexpected extracted url: %q", url)
	}

	if got := ExtractVideoID("https://www.youtube.com/watch?v=dQw4w9WgXcQ"); got != "dQw4w9WgXcQ" {
		t.Fatalf("ExtractVideoID watch = %q", got)
	}
	if got := ExtractVideoID("https://youtu.be/dQw4w9WgXcQ"); got != "dQw4w9WgXcQ" {
		t.Fatalf("ExtractVideoID short = %q", got)
	}
	if got := ExtractVideoID("invalid"); got != "" {
		t.Fatalf("ExtractVideoID invalid = %q", got)
	}
}

func TestBinanceWSHelpers(t *testing.T) {
	t.Parallel()

	var updates []BinanceTickerUpdate
	b := NewBinanceWS([]string{"BTC", "ETH"}, func(u BinanceTickerUpdate) {
		updates = append(updates, u)
	}, nil)

	if got := b.buildURL(); !strings.Contains(got, "btcusdt@ticker") || !strings.Contains(got, "ethusdt@ticker") {
		t.Fatalf("unexpected buildURL: %s", got)
	}

	b.handleMessage([]byte(`{"stream":"btcusdt@ticker","data":{"s":"BTCUSDT","c":"101.5","x":"100.0","p":"1.5","P":"1.50"}}`))
	b.parseTicker([]byte(`{"s":"ETHUSDT","c":"200.0","x":"198.0","p":"2.0","P":"1.01"}`))
	b.handleMessage([]byte(`{"s":"BTCKRW","c":"100"}`))
	b.handleMessage([]byte(`invalid-json`))

	if len(updates) < 2 {
		t.Fatalf("expected updates from handleMessage, got %d", len(updates))
	}
	if updates[0].Symbol == "" || updates[0].Price == 0 {
		t.Fatalf("unexpected first update: %+v", updates[0])
	}

	if got := jsonFloat("12.34"); got != 12.34 {
		t.Fatalf("jsonFloat string = %v", got)
	}
	if got := jsonFloat(float64(9.8)); got != 9.8 {
		t.Fatalf("jsonFloat number = %v", got)
	}
	if got := jsonFloat(true); got != 0 {
		t.Fatalf("jsonFloat bool = %v", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.connect(ctx); err == nil {
		t.Fatal("expected connect canceled error")
	}
	b.Start(ctx)
	b.Close()
}

func TestUpbitWSHelpersAndSubscribe(t *testing.T) {
	t.Parallel()

	var updates []UpbitTickerUpdate
	u := NewUpbitWS([]string{"BTC", "ETH"}, func(update UpbitTickerUpdate) {
		updates = append(updates, update)
	}, nil)

	u.handleMessage([]byte(`{"type":"ticker","code":"KRW-BTC","trade_price":110.0,"prev_closing_price":100.0,"signed_change_price":10.0,"signed_change_rate":0.1}`))
	u.handleMessage([]byte(`{"type":"other","code":"KRW-BTC"}`))
	u.handleMessage([]byte(`invalid-json`))
	if len(updates) != 1 {
		t.Fatalf("expected one upbit update, got %d", len(updates))
	}
	if updates[0].Symbol != "BTC" || updates[0].TradePrice != 110 {
		t.Fatalf("unexpected upbit update: %+v", updates[0])
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := u.connect(ctx); err == nil {
		t.Fatal("expected connect canceled error")
	}
	u.Start(ctx)

	upgrader := websocket.Upgrader{}
	msgCh := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, msg, err := conn.ReadMessage()
		if err == nil {
			msgCh <- string(msg)
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close()

	if err := u.subscribe(conn); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	select {
	case msg := <-msgCh:
		var payload []map[string]any
		if err := json.Unmarshal([]byte(msg), &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if len(payload) < 2 {
			t.Fatalf("unexpected subscribe payload: %v", payload)
		}
		codes, _ := payload[1]["codes"].([]any)
		if len(codes) == 0 {
			t.Fatalf("subscribe codes missing: %v", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive subscribe payload")
	}

	u.Close()
}
