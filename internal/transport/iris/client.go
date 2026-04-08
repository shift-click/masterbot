package iris

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/metrics"
	"github.com/shift-click/masterbot/internal/transport"
)

type Client struct {
	wsURL             string
	httpURL           string
	httpClient        *http.Client
	dialer            *websocket.Dialer
	logger            *slog.Logger
	recorder          metrics.Recorder
	backoffMin        time.Duration
	backoffMax        time.Duration
	roomWorkerEnabled bool
	roomWorkerCount   int

	mu   sync.RWMutex
	conn *websocket.Conn
}

// rawIncomingMessage matches the Iris WebSocket/HTTP event format.
type rawIncomingMessage struct {
	Msg    string         `json:"msg"`
	Room   string         `json:"room"`
	Sender string         `json:"sender"`
	JSON   rawChatLogJSON `json:"json"`
}

// rawChatLogJSON represents the raw chat_logs table row from Iris.
type rawChatLogJSON struct {
	ID         any    `json:"_id"`
	ChatID     any    `json:"chat_id"`
	UserID     any    `json:"user_id"`
	Message    string `json:"message"`
	Attachment string `json:"attachment"`
	V          string `json:"v"`
}

// NewClient creates an Iris client from an instance config.
func NewClient(cfg config.IrisInstanceConfig, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}

	return &Client{
		wsURL:   cfg.WSURL,
		httpURL: strings.TrimRight(cfg.HTTPURL, "/"),
		httpClient: &http.Client{
			Timeout: cfg.RequestTimeout,
			Transport: &http.Transport{
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   10,
				MaxConnsPerHost:       20,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   5 * time.Second,
				ResponseHeaderTimeout: 10 * time.Second,
				ForceAttemptHTTP2:     true,
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
			},
		},
		dialer:            websocket.DefaultDialer,
		logger:            logger.With("component", "iris", "instance", cfg.ID),
		backoffMin:        cfg.ReconnectMin,
		backoffMax:        cfg.ReconnectMax,
		roomWorkerEnabled: cfg.RoomWorkerEnabled,
		roomWorkerCount:   cfg.RoomWorkerCount,
	}
}

func (c *Client) Start(ctx context.Context, onMessage func(context.Context, transport.Message) error) error {
	if onMessage == nil {
		return errors.New("onMessage handler is required")
	}

	backoff := c.backoffMin
	if backoff <= 0 {
		backoff = time.Second
	}

	for ctx.Err() == nil {
		next, connected, err := c.tryConnect(ctx, backoff)
		if err != nil {
			return err
		}
		if !connected {
			backoff = next
			continue
		}
		backoff = c.backoffMin
		if err := c.readLoop(ctx, onMessage); err != nil && ctx.Err() == nil {
			c.logger.Warn("websocket disconnected", "error", err)
		}
		next, err = c.scheduleReconnect(ctx, backoff)
		if err != nil {
			return err
		}
		backoff = next
	}
	return ctx.Err()
}

func (c *Client) tryConnect(ctx context.Context, backoff time.Duration) (time.Duration, bool, error) {
	if err := c.connect(ctx); err != nil {
		c.logger.Warn("websocket connect failed", "error", err, "retry_in", backoff)
		if sleepErr := sleepContext(ctx, backoff); sleepErr != nil {
			return backoff, false, sleepErr
		}
		return nextBackoff(backoff, c.backoffMax), false, nil
	}
	return c.backoffMin, true, nil
}

func (c *Client) scheduleReconnect(ctx context.Context, backoff time.Duration) (time.Duration, error) {
	c.closeConn()
	if ctx.Err() != nil {
		return backoff, ctx.Err()
	}
	if err := sleepContext(ctx, backoff); err != nil {
		return backoff, err
	}
	return nextBackoff(backoff, c.backoffMax), nil
}

func (c *Client) Reply(ctx context.Context, req transport.ReplyRequest) error {
	if req.Room == "" {
		return errors.New("reply room is required")
	}

	return c.postJSON(ctx, "/reply", req, nil)
}

func (c *Client) SendText(ctx context.Context, room, text string) error {
	return c.Reply(ctx, transport.ReplyRequest{
		Type: transport.ReplyTypeText,
		Room: room,
		Data: text,
	})
}

func (c *Client) SendImage(ctx context.Context, room, base64Image string) error {
	return c.Reply(ctx, transport.ReplyRequest{
		Type: transport.ReplyTypeImage,
		Room: room,
		Data: base64Image,
	})
}

func (c *Client) SendMultipleImages(ctx context.Context, room string, base64Images []string) error {
	return c.Reply(ctx, transport.ReplyRequest{
		Type: transport.ReplyTypeImageMultiple,
		Room: room,
		Data: base64Images,
	})
}

func (c *Client) Query(ctx context.Context, query string, bind ...string) ([]map[string]any, error) {
	req := transport.QueryRequest{
		Query: query,
		Bind:  bind,
	}

	var res transport.QueryResponse
	if err := c.postJSON(ctx, "/query", req, &res); err != nil {
		return nil, err
	}

	return res.Data, nil
}

func (c *Client) BulkQuery(ctx context.Context, queries ...transport.QueryRequest) ([]map[string]any, error) {
	req := transport.BulkQueryRequest{
		Queries: queries,
	}

	var res transport.QueryResponse
	if err := c.postJSON(ctx, "/query", req, &res); err != nil {
		return nil, err
	}

	return res.Data, nil
}

func (c *Client) Close() error {
	c.closeConn()
	return nil
}

func (c *Client) SetMetricsRecorder(recorder metrics.Recorder) {
	if c == nil {
		return
	}
	c.recorder = recorder
}

func (c *Client) connect(ctx context.Context) error {
	conn, _, err := c.dialer.DialContext(ctx, c.wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial websocket: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	c.logger.Info("websocket connected", "url", c.wsURL)
	return nil
}

func (c *Client) readLoop(ctx context.Context, onMessage func(context.Context, transport.Message) error) error {
	conn := c.getConn()
	if conn == nil {
		return errors.New("websocket connection is not established")
	}

	// Set initial read deadline: pingInterval + pongGrace.
	const pingInterval = 30 * time.Second
	const pongGrace = 10 * time.Second
	_ = conn.SetReadDeadline(time.Now().Add(pingInterval + pongGrace))

	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pingInterval + pongGrace))
	})

	// Start a ping ticker goroutine. WriteControl is safe to call
	// concurrently with other writes per gorilla/websocket docs.
	pingDone := make(chan struct{})
	go func() {
		defer close(pingDone)
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := conn.WriteControl(
					websocket.PingMessage,
					nil,
					time.Now().Add(pongGrace),
				); err != nil {
					c.logger.Warn("websocket ping failed", "error", err)
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	dispatcher := newInboundDispatcher(ctx, onMessage, c.logger, c.recorder, c.roomWorkerEnabled, c.roomWorkerCount)
	defer dispatcher.close()

	dedup := newMessageDedup(10 * time.Second)

	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		msg, err := decodeMessage(payload)
		if err != nil {
			c.logger.Warn("failed to decode incoming message", "error", err)
			continue
		}
		c.logger.Info("ws message received",
			"id", msg.Raw.ID, "chat_id", msg.Raw.ChatID, "room", msg.Room,
			"sender", msg.Sender, "msg", msg.Msg)
		if shouldIgnoreEchoMessage(msg) {
			c.logger.Info("ignoring echoed bot message", "room", msg.Room, "sender", msg.Sender, "chat_id", msg.Raw.ChatID)
			continue
		}
		dedupKey := msg.Raw.ChatID + ":" + msg.Raw.Message
		if dedup.isDuplicate(dedupKey) {
			c.logger.Info("ignoring duplicate message", "id", msg.Raw.ID, "chat_id", msg.Raw.ChatID, "room", msg.Room, "sender", msg.Sender)
			continue
		}
		dispatcher.dispatch(msg)
	}
}

func shouldIgnoreEchoMessage(msg transport.Message) bool {
	sender := strings.TrimSpace(msg.Sender)
	room := strings.TrimSpace(msg.Room)
	return strings.EqualFold(sender, "Iris") || strings.EqualFold(room, "Iris")
}

func (c *Client) postJSON(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.httpURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= http.StatusBadRequest {
		responseBody, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return fmt.Errorf("unexpected status %d: %s", res.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

func (c *Client) getConn() *websocket.Conn {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn
}

func (c *Client) closeConn() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

func decodeMessage(payload []byte) (transport.Message, error) {
	var raw rawIncomingMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return transport.Message{}, fmt.Errorf("unmarshal payload: %w", err)
	}

	return transport.Message{
		Msg:    raw.Msg,
		Room:   raw.Room,
		Sender: raw.Sender,
		Raw: transport.RawChatLog{
			ID:         anyToString(raw.JSON.ID),
			ChatID:     anyToString(raw.JSON.ChatID),
			UserID:     anyToString(raw.JSON.UserID),
			Message:    raw.JSON.Message,
			Attachment: raw.JSON.Attachment,
			V:          raw.JSON.V,
		},
	}, nil
}

// anyToString converts various JSON number/string types to string.
// Iris may send IDs as numbers or strings depending on the field.
func anyToString(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case float64:
		return fmt.Sprintf("%.0f", val)
	case json.Number:
		return val.String()
	default:
		return fmt.Sprintf("%v", val)
	}
}

// nextBackoff computes the next reconnection delay using exponential backoff
// with Full Jitter (sleep = random_between(0, min(cap, base*2^n))).
// See: https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/
func nextBackoff(current, max time.Duration) time.Duration {
	if max <= 0 {
		max = 30 * time.Second
	}
	if current <= 0 {
		current = time.Second
	}

	// Exponential component.
	next := current * 2
	if next > max {
		next = max
	}

	// Full Jitter: uniform random in [0, next).
	if next > 0 {
		next = time.Duration(rand.Int64N(int64(next)))
	}

	// Floor to avoid busy-loop.
	const minBackoff = 100 * time.Millisecond
	if next < minBackoff {
		next = minBackoff
	}

	return next
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type inboundDispatcher struct {
	ctx           context.Context
	onMessage     func(context.Context, transport.Message) error
	logger        *slog.Logger
	recorder      metrics.Recorder
	parallel      bool
	maxRoomWorker int

	mu              sync.Mutex
	rooms           map[string]chan transport.Message
	closed          bool
	wg              sync.WaitGroup
	sem             chan struct{}
	droppedMessages atomic.Int64
}

func newInboundDispatcher(
	ctx context.Context,
	onMessage func(context.Context, transport.Message) error,
	logger *slog.Logger,
	recorder metrics.Recorder,
	parallel bool,
	maxRoomWorker int,
) *inboundDispatcher {
	if maxRoomWorker <= 0 {
		maxRoomWorker = 1
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &inboundDispatcher{
		ctx:           ctx,
		onMessage:     onMessage,
		logger:        logger,
		recorder:      recorder,
		parallel:      parallel,
		maxRoomWorker: maxRoomWorker,
		rooms:         make(map[string]chan transport.Message),
		sem:           make(chan struct{}, maxRoomWorker),
	}
}

func (d *inboundDispatcher) dispatch(msg transport.Message) {
	if !d.parallel {
		if err := d.onMessage(d.ctx, msg); err != nil {
			d.logger.Warn("message handler returned error", "error", err, "room", msg.Room, "sender", msg.Sender)
		}
		return
	}

	roomKey := roomDispatchKey(msg)
	ch := d.ensureRoomWorker(roomKey)
	if ch == nil {
		return
	}
	select {
	case ch <- msg:
	default:
		// Room buffer full — drop message to prevent blocking the read loop.
		d.droppedMessages.Add(1)
		d.logger.Warn("room buffer full, dropping message",
			"room", roomKey,
			"queue_len", len(ch),
			"total_dropped", d.droppedMessages.Load(),
		)
		if d.recorder != nil {
			d.recorder.Record(d.ctx, metrics.Event{
				OccurredAt:     time.Now(),
				RequestID:      strings.TrimSpace(msg.Raw.ID),
				EventName:      metrics.EventTransportOverload,
				RawRoomID:      msg.Raw.ChatID,
				RawTenantID:    msg.Raw.ChatID,
				RawScopeRoomID: msg.Raw.ChatID,
				RoomName:       msg.Room,
				RawUserID:      msg.Raw.UserID,
				Audience:       "customer",
				FeatureKey:     "transport",
				Attribution:    "iris_room_dispatcher",
				ErrorClass:     "room_buffer_full",
				Metadata: map[string]any{
					"drop_reason":       "room_buffer_full",
					"queue_len":         len(ch),
					"room_dispatch_key": roomKey,
				},
			})
		}
	}
}

func (d *inboundDispatcher) ensureRoomWorker(roomKey string) chan transport.Message {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	if ch, ok := d.rooms[roomKey]; ok {
		return ch
	}

	ch := make(chan transport.Message, 64)
	d.rooms[roomKey] = ch
	d.wg.Add(1)
	go d.runRoomWorker(roomKey, ch)
	return ch
}

func (d *inboundDispatcher) runRoomWorker(roomKey string, ch chan transport.Message) {
	defer d.wg.Done()
	d.sem <- struct{}{}
	defer func() { <-d.sem }()
	defer func() {
		d.mu.Lock()
		delete(d.rooms, roomKey)
		d.mu.Unlock()
	}()

	for {
		select {
		case <-d.ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if err := d.onMessage(d.ctx, msg); err != nil {
				d.logger.Warn("message handler returned error", "error", err, "room", msg.Room, "sender", msg.Sender)
			}
		}
	}
}

func (d *inboundDispatcher) close() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	channels := make([]chan transport.Message, 0, len(d.rooms))
	for _, ch := range d.rooms {
		channels = append(channels, ch)
	}
	d.mu.Unlock()

	for _, ch := range channels {
		close(ch)
	}
	d.wg.Wait()
}

// messageDedup tracks recently seen message IDs to suppress duplicates
// caused by Iris re-emitting the same chat_logs row (e.g. link-preview update).
// Safe for single-goroutine use only (readLoop).
type messageDedup struct {
	seen    map[string]time.Time
	window  time.Duration
	counter int
}

func newMessageDedup(window time.Duration) *messageDedup {
	return &messageDedup{
		seen:   make(map[string]time.Time),
		window: window,
	}
}

func (d *messageDedup) isDuplicate(id string) bool {
	if id == "" {
		return false
	}
	now := time.Now()
	if t, ok := d.seen[id]; ok && now.Sub(t) < d.window {
		return true
	}
	d.seen[id] = now
	d.counter++
	if d.counter%100 == 0 {
		d.evict(now)
	}
	return false
}

func (d *messageDedup) evict(now time.Time) {
	for id, t := range d.seen {
		if now.Sub(t) >= d.window {
			delete(d.seen, id)
		}
	}
}

func roomDispatchKey(msg transport.Message) string {
	if room := strings.TrimSpace(msg.Raw.ChatID); room != "" {
		return room
	}
	if room := strings.TrimSpace(msg.Room); room != "" {
		return room
	}
	return "_default_room"
}
