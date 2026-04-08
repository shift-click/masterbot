package httptest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/transport"
)

// MessageRequest is the JSON body for POST /message.
type MessageRequest struct {
	Msg    string `json:"msg"`
	Room   string `json:"room"`
	Sender string `json:"sender"`
	ChatID string `json:"chat_id"`
	// Attachment allows e2e/integration tests to inject the same raw attachment
	// payload shape that Iris would deliver in production.
	Attachment string `json:"attachment,omitempty"`
}

// MessageResponse is the JSON response for POST /message.
type MessageResponse struct {
	Replies []ReplyEntry `json:"replies"`
}

// ReplyEntry represents a single reply from a handler.
type ReplyEntry struct {
	Type transport.ReplyType `json:"type"`
	Data any                 `json:"data"`
}

// pendingRequest tracks replies for one in-flight message.
type pendingRequest struct {
	ch   chan ReplyEntry
	done chan struct{}
}

// Server implements transport.RuntimeAdapter over HTTP for testing.
type Server struct {
	addr         string
	replyTimeout time.Duration
	logger       *slog.Logger

	stateMu   sync.RWMutex
	server    *http.Server
	listener  net.Listener
	onMessage func(context.Context, transport.Message) error

	mu      sync.Mutex
	pending map[uint64]*pendingRequest
	reqSeq  atomic.Uint64
}

// NewServer creates an HTTP test transport adapter.
func NewServer(cfg config.HTTPTestConfig, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		addr:         cfg.Addr,
		replyTimeout: cfg.ReplyTimeout,
		logger:       logger.With("component", "httptest-transport"),
		pending:      make(map[uint64]*pendingRequest),
	}
}

// Start implements transport.RuntimeAdapter. It starts the HTTP server and
// blocks until the context is cancelled.
func (s *Server) Start(ctx context.Context, onMessage func(context.Context, transport.Message) error) error {
	s.stateMu.Lock()
	s.onMessage = onMessage
	s.stateMu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /message", s.handleMessage)

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("httptest listen: %w", err)
	}

	server := &http.Server{Handler: mux}
	s.stateMu.Lock()
	s.listener = ln
	s.server = server
	s.stateMu.Unlock()
	s.logger.Info("httptest transport started", "addr", ln.Addr().String())

	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()

	if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("httptest serve: %w", err)
	}
	return nil
}

// Reply implements transport.RuntimeAdapter. It sends the reply to the pending
// request's channel, identified by the room field (which carries the request ID).
func (s *Server) Reply(_ context.Context, req transport.ReplyRequest) error {
	var reqID uint64
	if err := json.Unmarshal([]byte(req.Room), &reqID); err != nil {
		return fmt.Errorf("httptest reply: invalid request id in room field: %w", err)
	}

	s.mu.Lock()
	pr, ok := s.pending[reqID]
	s.mu.Unlock()
	if !ok {
		return nil
	}

	select {
	case pr.ch <- ReplyEntry{Type: req.Type, Data: req.Data}:
	case <-pr.done:
	}
	return nil
}

// Addr returns the actual listen address after Start. Returns "" before Start.
func (s *Server) Addr() string {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return ""
}

// Close implements transport.RuntimeAdapter.
func (s *Server) Close() error {
	s.stateMu.RLock()
	server := s.server
	s.stateMu.RUnlock()
	if server != nil {
		return server.Close()
	}
	return nil
}

func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request) {
	s.stateMu.RLock()
	onMessage := s.onMessage
	s.stateMu.RUnlock()

	var req MessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	reqID := s.reqSeq.Add(1)
	reqIDStr := fmt.Sprintf("%d", reqID)

	pr := &pendingRequest{
		ch:   make(chan ReplyEntry, 16),
		done: make(chan struct{}),
	}
	s.mu.Lock()
	s.pending[reqID] = pr
	s.mu.Unlock()

	defer func() {
		close(pr.done)
		s.mu.Lock()
		delete(s.pending, reqID)
		s.mu.Unlock()
	}()

	room := req.Room
	if room == "" {
		room = "test-room"
	}
	sender := req.Sender
	if sender == "" {
		sender = "tester"
	}
	chatID := req.ChatID
	if chatID == "" {
		chatID = "test-chat-1"
	}

	msg := transport.Message{
		Msg:    req.Msg,
		Room:   room,
		Sender: sender,
		Raw: transport.RawChatLog{
			ID:         reqIDStr,
			ChatID:     reqIDStr, // Use reqID so Reply() can correlate
			UserID:     "test-user",
			Message:    req.Msg,
			Attachment: req.Attachment,
		},
	}

	dispatchDone := make(chan error, 1)
	go func() {
		dispatchDone <- onMessage(r.Context(), msg)
	}()

	timer := time.NewTimer(s.replyTimeout)
	defer timer.Stop()

	var replies []ReplyEntry

	for {
		select {
		case entry := <-pr.ch:
			replies = append(replies, entry)
		case err := <-dispatchDone:
			// Drain any remaining replies that arrived before dispatch returned.
			for {
				select {
				case entry := <-pr.ch:
					replies = append(replies, entry)
				default:
					goto respond
				}
			}
		respond:
			if err != nil {
				s.logger.Warn("dispatch error", "msg", req.Msg, "error", err)
			}
			writeJSON(w, MessageResponse{Replies: replies})
			return
		case <-timer.C:
			writeJSON(w, MessageResponse{Replies: replies})
			return
		case <-r.Context().Done():
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
