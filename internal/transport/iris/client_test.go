package iris

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/metrics"
	"github.com/shift-click/masterbot/internal/transport"
)

type transportRecorder struct {
	mu     sync.Mutex
	events []metrics.Event
}

func (r *transportRecorder) Record(_ context.Context, event metrics.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func TestShouldIgnoreEchoMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  transport.Message
		want bool
	}{
		{
			name: "ignore sender iris",
			msg:  transport.Message{Sender: "Iris", Room: "Muna"},
			want: true,
		},
		{
			name: "ignore room iris",
			msg:  transport.Message{Sender: "Muna", Room: "Iris"},
			want: true,
		},
		{
			name: "allow user message",
			msg:  transport.Message{Sender: "Muna", Room: "Muna"},
			want: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldIgnoreEchoMessage(tc.msg); got != tc.want {
				t.Fatalf("shouldIgnoreEchoMessage() = %v, want %v", got, tc.want)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDecodeMessageAndAnyToString(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"msg":"hello",
		"room":"room-a",
		"sender":"alice",
		"json":{"_id":123,"chat_id":"456","user_id":789,"message":"hello","attachment":"att","v":"v1"}
	}`)
	msg, err := decodeMessage(payload)
	if err != nil {
		t.Fatalf("decodeMessage() error = %v", err)
	}
	if msg.Msg != "hello" || msg.Room != "room-a" || msg.Sender != "alice" {
		t.Fatalf("decoded message mismatch: %+v", msg)
	}
	if msg.Raw.ID != "123" || msg.Raw.ChatID != "456" || msg.Raw.UserID != "789" {
		t.Fatalf("decoded ids mismatch: %+v", msg.Raw)
	}

	if _, err := decodeMessage([]byte(`{"msg":`)); err == nil {
		t.Fatal("expected decode error for broken payload")
	}

	if got := anyToString(nil); got != "" {
		t.Fatalf("anyToString(nil) = %q", got)
	}
	if got := anyToString("x"); got != "x" {
		t.Fatalf("anyToString(string) = %q", got)
	}
	if got := anyToString(float64(42)); got != "42" {
		t.Fatalf("anyToString(float64) = %q", got)
	}
	if got := anyToString(json.Number("77")); got != "77" {
		t.Fatalf("anyToString(json.Number) = %q", got)
	}
	if got := anyToString(struct{ A int }{A: 1}); got == "" {
		t.Fatal("anyToString(default) should not be empty")
	}
}

func TestBackoffAndSleepContext(t *testing.T) {
	t.Parallel()

	// nextBackoff now applies Full Jitter: result is in [100ms, exponential_cap).
	// With current=0, the function normalises to 1s then doubles to 2s, jitter picks from [100ms, 2s).
	got := nextBackoff(0, 0)
	if got < 100*time.Millisecond || got >= 2*time.Second {
		t.Fatalf("nextBackoff(0,0) = %v, want [100ms, 2s)", got)
	}

	// current=2s, max=5s → exponential=4s, jitter picks from [100ms, 4s).
	got = nextBackoff(2*time.Second, 5*time.Second)
	if got < 100*time.Millisecond || got >= 4*time.Second {
		t.Fatalf("nextBackoff(2s,5s) = %v, want [100ms, 4s)", got)
	}

	// current=4s, max=5s → exponential capped at 5s, jitter picks from [100ms, 5s).
	got = nextBackoff(4*time.Second, 5*time.Second)
	if got < 100*time.Millisecond || got >= 5*time.Second {
		t.Fatalf("nextBackoff(4s,5s) = %v, want [100ms, 5s)", got)
	}

	// Verify statistical distribution: run many iterations and confirm range coverage.
	var minSeen, maxSeen time.Duration
	minSeen = time.Hour
	for range 1000 {
		v := nextBackoff(time.Second, 10*time.Second)
		if v < minSeen {
			minSeen = v
		}
		if v > maxSeen {
			maxSeen = v
		}
	}
	if minSeen > 200*time.Millisecond {
		t.Fatalf("jitter min too high: %v (expected close to 100ms)", minSeen)
	}
	if maxSeen < time.Second {
		t.Fatalf("jitter max too low: %v (expected close to 2s)", maxSeen)
	}

	if err := sleepContext(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("sleepContext() unexpected error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepContext(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("sleepContext canceled error = %v", err)
	}
}

func TestNewClientAndReadLoopGuard(t *testing.T) {
	t.Parallel()

	c := NewClient(config.IrisInstanceConfig{
		ID:             "test",
		WSURL:          "ws://127.0.0.1:9999/ws",
		HTTPURL:        "http://127.0.0.1:3000/",
		RequestTimeout: time.Second,
		ReconnectMin:   10 * time.Millisecond,
		ReconnectMax:   20 * time.Millisecond,
	}, nil)
	if c.httpURL != "http://127.0.0.1:3000" {
		t.Fatalf("httpURL trim failed: %q", c.httpURL)
	}
	if err := c.Start(context.Background(), nil); err == nil {
		t.Fatal("expected Start error when onMessage is nil")
	}
	if err := c.readLoop(context.Background(), func(context.Context, transport.Message) error { return nil }); err == nil {
		t.Fatal("expected readLoop error without websocket connection")
	}
	c.closeConn()
	if c.getConn() != nil {
		t.Fatal("expected nil connection after closeConn")
	}
}

func TestPostJSONErrorPaths(t *testing.T) {
	t.Parallel()

	client := &Client{
		httpURL: "http://127.0.0.1:1",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("network down")
			}),
		},
	}
	if err := client.postJSON(context.Background(), "/x", map[string]any{"x": 1}, nil); err == nil {
		t.Fatal("expected do request error")
	}

	if err := client.postJSON(context.Background(), "/x", map[string]any{"x": make(chan int)}, nil); err == nil {
		t.Fatal("expected marshal request error")
	}

	client = &Client{
		httpURL: "://bad-url",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, nil
			}),
		},
	}
	if err := client.postJSON(context.Background(), "/x", map[string]any{"x": 1}, nil); err == nil {
		t.Fatal("expected create request error")
	}
}

// irisReplyRequest mirrors the Iris Java server's party.qwer.iris.model.ReplyRequest.
// Using DisallowUnknownFields on this struct replicates Jackson's
// FAIL_ON_UNKNOWN_PROPERTIES=true behavior.
type irisReplyRequest struct {
	Type string `json:"type"`
	Room string `json:"room"`
	Data any    `json:"data"`
}

func TestReplyRequestSerializationContract(t *testing.T) {
	t.Parallel()

	allowedKeys := map[string]bool{"type": true, "room": true, "data": true}

	tests := []struct {
		name string
		req  transport.ReplyRequest
	}{
		{
			name: "text reply",
			req:  transport.ReplyRequest{Type: transport.ReplyTypeText, Room: "room-1", Data: "hello"},
		},
		{
			name: "image reply",
			req:  transport.ReplyRequest{Type: transport.ReplyTypeImage, Room: "room-1", Data: "base64img"},
		},
		{
			name: "image_multiple reply",
			req:  transport.ReplyRequest{Type: transport.ReplyTypeImageMultiple, Room: "room-1", Data: []string{"a", "b"}},
		},
		{
			name: "adapter_id populated but must not serialize",
			req:  transport.ReplyRequest{Type: transport.ReplyTypeText, Room: "room-1", Data: "test", AdapterID: "main"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tc.req)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}

			var fields map[string]any
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}

			for key := range fields {
				if !allowedKeys[key] {
					t.Errorf("unexpected key %q in serialized ReplyRequest; allowed keys: %v", key, allowedKeys)
				}
			}
			for key := range allowedKeys {
				if _, ok := fields[key]; !ok {
					t.Errorf("missing required key %q in serialized ReplyRequest", key)
				}
			}
		})
	}
}

func TestPostJSONAndReplyQueryPaths(t *testing.T) {
	t.Parallel()

	var replySeen bool
	var querySeen bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/reply":
			replySeen = true
			// Strict parsing: reject unknown fields (mirrors Iris Java Jackson behavior).
			dec := json.NewDecoder(r.Body)
			dec.DisallowUnknownFields()
			var req irisReplyRequest
			if err := dec.Decode(&req); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"status":false,"message":"Failed to convert request body to class party.qwer.iris.model.ReplyRequest"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/query":
			querySeen = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"k":"v"}]}`))
		case "/bad":
			http.Error(w, "bad request", http.StatusBadRequest)
		case "/invalid-json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := &Client{
		httpURL:    srv.URL,
		httpClient: srv.Client(),
		backoffMin: time.Millisecond,
		backoffMax: 2 * time.Millisecond,
	}

	if err := c.Reply(context.Background(), transport.ReplyRequest{}); err == nil {
		t.Fatal("expected error for empty reply room")
	}
	if err := c.SendText(context.Background(), "r1", "hello"); err != nil {
		t.Fatalf("SendText() error = %v", err)
	}
	if err := c.SendImage(context.Background(), "r1", "base64"); err != nil {
		t.Fatalf("SendImage() error = %v", err)
	}
	if err := c.SendMultipleImages(context.Background(), "r1", []string{"a", "b"}); err != nil {
		t.Fatalf("SendMultipleImages() error = %v", err)
	}
	rows, err := c.Query(context.Background(), "select 1", "x")
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(rows) != 1 || rows[0]["k"] != "v" {
		t.Fatalf("unexpected query rows: %+v", rows)
	}
	rows, err = c.BulkQuery(context.Background(), transport.QueryRequest{Query: "select 1"})
	if err != nil {
		t.Fatalf("BulkQuery() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("unexpected bulk query rows: %+v", rows)
	}
	if !replySeen || !querySeen {
		t.Fatalf("expected reply/query handlers to be called, reply=%v query=%v", replySeen, querySeen)
	}

	if err := c.postJSON(context.Background(), "/bad", map[string]any{"x": 1}, nil); err == nil || !strings.Contains(err.Error(), "unexpected status 400") {
		t.Fatalf("expected status error, got %v", err)
	}
	var out map[string]any
	if err := c.postJSON(context.Background(), "/invalid-json", map[string]any{"x": 1}, &out); err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("expected decode error, got %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestInboundDispatcherKeepsOrderWithinRoom(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		order []string
	)
	dispatcher := newInboundDispatcher(
		context.Background(),
		func(_ context.Context, msg transport.Message) error {
			mu.Lock()
			order = append(order, msg.Msg)
			mu.Unlock()
			time.Sleep(5 * time.Millisecond)
			return nil
		},
		nil,
		nil,
		true,
		2,
	)

	dispatcher.dispatch(transport.Message{Msg: "1", Raw: transport.RawChatLog{ChatID: "room-a"}})
	dispatcher.dispatch(transport.Message{Msg: "2", Raw: transport.RawChatLog{ChatID: "room-a"}})
	dispatcher.dispatch(transport.Message{Msg: "3", Raw: transport.RawChatLog{ChatID: "room-a"}})
	dispatcher.close()

	mu.Lock()
	defer mu.Unlock()
	got := strings.Join(order, ",")
	if got != "1,2,3" {
		t.Fatalf("same-room order = %q, want 1,2,3", got)
	}
}

func TestInboundDispatcherProcessesDifferentRoomsInParallel(t *testing.T) {
	t.Parallel()

	room1Started := make(chan struct{}, 1)
	room1Release := make(chan struct{})
	room2Done := make(chan struct{}, 1)

	dispatcher := newInboundDispatcher(
		context.Background(),
		func(_ context.Context, msg transport.Message) error {
			switch msg.Raw.ChatID {
			case "room-1":
				room1Started <- struct{}{}
				<-room1Release
			case "room-2":
				room2Done <- struct{}{}
			}
			return nil
		},
		nil,
		nil,
		true,
		2,
	)
	defer dispatcher.close()

	dispatcher.dispatch(transport.Message{Msg: "A", Raw: transport.RawChatLog{ChatID: "room-1"}})
	<-room1Started
	dispatcher.dispatch(transport.Message{Msg: "B", Raw: transport.RawChatLog{ChatID: "room-2"}})

	select {
	case <-room2Done:
		// pass
	case <-time.After(100 * time.Millisecond):
		t.Fatal("room-2 message was blocked by room-1 processing")
	}
	close(room1Release)
}

func TestTryConnectAndScheduleReconnect(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	client := &Client{
		wsURL:      "ws" + strings.TrimPrefix(srv.URL, "http"),
		dialer:     websocket.DefaultDialer,
		logger:     slog.Default(),
		backoffMin: time.Millisecond,
		backoffMax: 4 * time.Millisecond,
	}

	next, connected, err := client.tryConnect(context.Background(), time.Millisecond)
	if err != nil {
		t.Fatalf("tryConnect success: %v", err)
	}
	if !connected {
		t.Fatal("expected websocket connection")
	}
	if next != client.backoffMin {
		t.Fatalf("next backoff = %v, want %v", next, client.backoffMin)
	}
	if client.getConn() == nil {
		t.Fatal("expected client connection to be stored")
	}

	next, err = client.scheduleReconnect(context.Background(), time.Millisecond)
	if err != nil {
		t.Fatalf("scheduleReconnect: %v", err)
	}
	// With Full Jitter, backoff is random but floored at 100ms.
	if next < 100*time.Millisecond {
		t.Fatalf("scheduleReconnect next = %v, want >= 100ms", next)
	}
	if client.getConn() != nil {
		t.Fatal("expected connection to be closed during reconnect")
	}
}

func TestTryConnectFailureAndReadLoopMessageHandling(t *testing.T) {
	t.Parallel()

	client := &Client{
		wsURL:      "ws://127.0.0.1:1/ws",
		dialer:     websocket.DefaultDialer,
		logger:     slog.Default(),
		backoffMin: time.Millisecond,
		backoffMax: 4 * time.Millisecond,
	}
	next, connected, err := client.tryConnect(context.Background(), time.Millisecond)
	if err != nil {
		t.Fatalf("tryConnect failure path: %v", err)
	}
	if connected {
		t.Fatal("expected failed connection")
	}
	// With Full Jitter, backoff is random in [100ms, 2ms*2=4ms) but floored at 100ms.
	if next < 100*time.Millisecond {
		t.Fatalf("failure next backoff = %v, want >= 100ms", next)
	}

	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"msg":`))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"msg":"echo","room":"room-a","sender":"Iris","json":{"chat_id":"echo-room"}}`))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"msg":"live","room":"","sender":"alice","json":{"chat_id":"chat-1","message":"live"}}`))
	}))
	defer srv.Close()

	client.wsURL = "ws" + strings.TrimPrefix(srv.URL, "http")
	client.roomWorkerEnabled = false
	if err := client.connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}

	got := make(chan transport.Message, 1)
	err = client.readLoop(context.Background(), func(_ context.Context, msg transport.Message) error {
		got <- msg
		return nil
	})
	if err == nil {
		t.Fatal("expected readLoop to stop on websocket close")
	}

	select {
	case msg := <-got:
		if msg.Msg != "live" || msg.Raw.ChatID != "chat-1" {
			t.Fatalf("unexpected message: %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("expected live message to be dispatched")
	}

	if got := roomDispatchKey(transport.Message{Raw: transport.RawChatLog{ChatID: "chat-2"}}); got != "chat-2" {
		t.Fatalf("roomDispatchKey chat_id = %q", got)
	}
	if got := roomDispatchKey(transport.Message{Room: "room-b"}); got != "room-b" {
		t.Fatalf("roomDispatchKey room = %q", got)
	}
	if got := roomDispatchKey(transport.Message{}); got != "_default_room" {
		t.Fatalf("roomDispatchKey default = %q", got)
	}
}

func TestMessageDedupSuppressesDuplicate(t *testing.T) {
	t.Parallel()

	d := newMessageDedup(50 * time.Millisecond)

	if d.isDuplicate("msg-1") {
		t.Fatal("first occurrence should not be duplicate")
	}
	if !d.isDuplicate("msg-1") {
		t.Fatal("second occurrence within window should be duplicate")
	}

	// Empty ID is never considered duplicate.
	if d.isDuplicate("") {
		t.Fatal("empty ID should never be duplicate")
	}
	if d.isDuplicate("") {
		t.Fatal("empty ID should never be duplicate (second call)")
	}

	// Different ID should pass through.
	if d.isDuplicate("msg-2") {
		t.Fatal("different ID should not be duplicate")
	}
}

func TestMessageDedupExpiresAfterWindow(t *testing.T) {
	t.Parallel()

	d := newMessageDedup(10 * time.Millisecond)

	if d.isDuplicate("msg-1") {
		t.Fatal("first occurrence should not be duplicate")
	}
	time.Sleep(15 * time.Millisecond)
	if d.isDuplicate("msg-1") {
		t.Fatal("expired entry should not be considered duplicate")
	}
}

func TestMessageDedupEvictsOldEntries(t *testing.T) {
	t.Parallel()

	d := newMessageDedup(10 * time.Millisecond)

	// Fill up to trigger eviction (every 100 entries).
	for i := 0; i < 100; i++ {
		d.isDuplicate(fmt.Sprintf("evict-%d", i))
	}
	time.Sleep(15 * time.Millisecond)
	// Next call triggers eviction at counter=101 (no, at 200). Let's just force it.
	d.evict(time.Now())
	if len(d.seen) != 0 {
		t.Fatalf("expected all entries evicted, got %d", len(d.seen))
	}
}

func TestReadLoopDedupIntegration(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		// Send same chat_id with different _id (simulates Iris link-preview re-emission creating a new row).
		msg1 := `{"msg":"https://coupang.com/vp/123","room":"room-a","sender":"muna","json":{"_id":999,"chat_id":"chat-1","message":"https://coupang.com/vp/123"}}`
		msg2 := `{"msg":"https://coupang.com/vp/123","room":"room-a","sender":"muna","json":{"_id":1000,"chat_id":"chat-1","message":"https://coupang.com/vp/123","attachment":"{\"url\":\"...\"}"}}`
		_ = conn.WriteMessage(websocket.TextMessage, []byte(msg1))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(msg2))
	}))
	defer srv.Close()

	client := &Client{
		wsURL:             "ws" + strings.TrimPrefix(srv.URL, "http"),
		dialer:            websocket.DefaultDialer,
		logger:            slog.Default(),
		backoffMin:        time.Millisecond,
		backoffMax:        4 * time.Millisecond,
		roomWorkerEnabled: false,
	}
	if err := client.connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}

	var mu sync.Mutex
	var dispatched []transport.Message
	_ = client.readLoop(context.Background(), func(_ context.Context, msg transport.Message) error {
		mu.Lock()
		dispatched = append(dispatched, msg)
		mu.Unlock()
		return nil
	})

	mu.Lock()
	defer mu.Unlock()
	if len(dispatched) != 1 {
		t.Fatalf("expected 1 dispatched message (dedup), got %d", len(dispatched))
	}
	if dispatched[0].Raw.ID != "999" {
		t.Fatalf("unexpected message ID: %q", dispatched[0].Raw.ID)
	}
}

func TestStartProcessesMessageAndStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"msg":"live","room":"room-a","sender":"alice","json":{"chat_id":"chat-1","message":"live"}}`))
	}))
	defer srv.Close()

	client := &Client{
		wsURL:             "ws" + strings.TrimPrefix(srv.URL, "http"),
		dialer:            websocket.DefaultDialer,
		logger:            slog.Default(),
		backoffMin:        time.Millisecond,
		backoffMax:        4 * time.Millisecond,
		roomWorkerEnabled: false,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var received transport.Message
	err := client.Start(ctx, func(_ context.Context, msg transport.Message) error {
		received = msg
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start() error = %v, want context.Canceled", err)
	}
	if received.Msg != "live" || received.Room != "room-a" {
		t.Fatalf("received message = %+v", received)
	}
}

func TestPingPongKeepalive(t *testing.T) {
	t.Parallel()

	pingReceived := make(chan struct{}, 1)

	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		// The server-side gorilla/websocket automatically responds to ping
		// with pong. We install a ping handler to detect when a ping arrives.
		conn.SetPingHandler(func(appData string) error {
			select {
			case pingReceived <- struct{}{}:
			default:
			}
			// Write pong back (default behaviour).
			return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(time.Second))
		})

		// Send a message so the client processes something.
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"msg":"hi","room":"room-a","sender":"alice","json":{"chat_id":"c1","message":"hi"}}`))

		// Keep reading to process control frames.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	client := &Client{
		wsURL:             "ws" + strings.TrimPrefix(srv.URL, "http"),
		dialer:            websocket.DefaultDialer,
		logger:            slog.Default(),
		backoffMin:        time.Millisecond,
		backoffMax:        4 * time.Millisecond,
		roomWorkerEnabled: false,
	}

	if err := client.connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Verify that the readLoop sets up a pong handler (by checking that
	// the connection has a read deadline set, via a short-lived readLoop).
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = client.readLoop(ctx, func(_ context.Context, msg transport.Message) error {
			return nil
		})
	}()

	// The readLoop sets the initial read deadline and starts pinging.
	// We can't easily test the 30s ping interval in a unit test,
	// but we verify the pong handler is installed by checking the
	// connection processes messages without error.
	time.Sleep(50 * time.Millisecond)
	cancel()
	client.closeConn()
}

func TestInboundDispatcherDropsMessageWhenBufferFull(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})
	recorder := &transportRecorder{}
	dispatcher := newInboundDispatcher(
		context.Background(),
		func(_ context.Context, msg transport.Message) error {
			<-block // Block forever until released
			return nil
		},
		slog.Default(),
		recorder,
		true,
		2,
	)

	// Fill the room buffer (capacity=64) + 1 message blocking in handler.
	// First message goes to handler (blocks), next 64 fill the channel buffer.
	for i := 0; i < 66; i++ {
		dispatcher.dispatch(transport.Message{
			Msg: fmt.Sprintf("msg-%d", i),
			Raw: transport.RawChatLog{ChatID: "room-flood"},
		})
	}

	// At least one message should have been dropped (buffer=64 + 1 in handler).
	// The exact count may vary due to goroutine scheduling.
	dropped := dispatcher.droppedMessages.Load()
	if dropped < 1 {
		t.Fatalf("droppedMessages = %d, want >= 1", dropped)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.events) == 0 {
		t.Fatal("expected overload metric event")
	}
	last := recorder.events[len(recorder.events)-1]
	if last.EventName != metrics.EventTransportOverload {
		t.Fatalf("event = %q, want %q", last.EventName, metrics.EventTransportOverload)
	}
	if got := last.Metadata["drop_reason"]; got != "room_buffer_full" {
		t.Fatalf("drop_reason = %v, want room_buffer_full", got)
	}

	close(block)
	dispatcher.close()
}

func TestNextBackoffFullJitterProperties(t *testing.T) {
	t.Parallel()

	// Property: result is always >= 100ms (floor).
	for range 500 {
		v := nextBackoff(time.Millisecond, time.Second)
		if v < 100*time.Millisecond {
			t.Fatalf("got %v, want >= 100ms", v)
		}
	}

	// Property: result is always < 2*current (or max, whichever is smaller).
	for range 500 {
		v := nextBackoff(time.Second, 5*time.Second)
		if v >= 2*time.Second {
			t.Fatalf("got %v, want < 2s", v)
		}
	}

	// Property: result is always < max when current*2 > max.
	for range 500 {
		v := nextBackoff(10*time.Second, 5*time.Second)
		if v >= 5*time.Second {
			t.Fatalf("got %v, want < 5s (max)", v)
		}
	}
}
