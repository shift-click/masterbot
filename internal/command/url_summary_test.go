package command

import (
	"context"
	"log/slog"
	"testing"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/transport"
)

func TestURLSummaryHandler_Execute_NoArgs(t *testing.T) {
	t.Parallel()
	h := NewURLSummaryHandler(nil, slog.Default())
	h.SetAdapter(newRuntimeAdapterStub())

	var got bot.Reply
	err := h.Execute(context.Background(), bot.CommandContext{
		Args: nil,
		Reply: func(_ context.Context, r bot.Reply) error {
			got = r
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if got.Text == "" {
		t.Fatal("expected non-empty reply for no-args case")
	}
}

func TestURLSummaryHandler_Execute_InvalidURL(t *testing.T) {
	t.Parallel()
	h := NewURLSummaryHandler(nil, slog.Default())
	h.SetAdapter(newRuntimeAdapterStub())

	var got bot.Reply
	err := h.Execute(context.Background(), bot.CommandContext{
		Args: []string{"not-a-url"},
		Reply: func(_ context.Context, r bot.Reply) error {
			got = r
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if got.Text == "" {
		t.Fatal("expected non-empty reply for invalid-url case")
	}
}

func TestURLSummaryHandler_Execute_AllowsExplicitNonWhitelistedURL(t *testing.T) {
	t.Parallel()
	h := NewURLSummaryHandler(nil, slog.Default())
	h.SetAdapter(newRuntimeAdapterStub())

	replyCalled := false
	err := h.Execute(context.Background(), bot.CommandContext{
		Args:    []string{"https://example.com/article"},
		Message: transport.Message{Raw: transport.RawChatLog{ChatID: "room-explicit"}},
		Reply: func(_ context.Context, _ bot.Reply) error {
			replyCalled = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if replyCalled {
		t.Fatal("expected explicit command to skip immediate ack reply")
	}
}

func TestURLSummaryHandler_HandleFallback_NoURL(t *testing.T) {
	t.Parallel()
	h := NewURLSummaryHandler(nil, slog.Default())

	err := h.HandleFallback(context.Background(), bot.CommandContext{
		Message: transport.Message{Msg: "just text"},
		Reply: func(_ context.Context, r bot.Reply) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("HandleFallback should return nil for non-URL message, got: %v", err)
	}
}

func TestURLSummaryHandler_HandleFallback_EmptyMessage(t *testing.T) {
	t.Parallel()
	h := NewURLSummaryHandler(nil, slog.Default())

	err := h.HandleFallback(context.Background(), bot.CommandContext{
		Message: transport.Message{Msg: ""},
		Reply: func(_ context.Context, r bot.Reply) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("HandleFallback should return nil for empty message, got: %v", err)
	}
}

func TestURLSummaryHandler_SemaphoreFull(t *testing.T) {
	t.Parallel()
	h := NewURLSummaryHandler(nil, slog.Default())
	stub := newRuntimeAdapterStub()
	h.SetAdapter(stub)

	// Fill semaphore.
	for range defaultMaxWorkers {
		h.exec.sem <- struct{}{}
	}

	var got bot.Reply
	err := h.Execute(context.Background(), bot.CommandContext{
		Args: []string{"https://example.com/article"},
		Reply: func(_ context.Context, r bot.Reply) error {
			got = r
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if got.Text == "" {
		t.Fatal("expected queue-full message")
	}

	// Drain semaphore.
	for range defaultMaxWorkers {
		<-h.exec.sem
	}
}

func TestURLSummaryHandler_HandleFallback_AttachmentURL(t *testing.T) {
	t.Parallel()
	h := NewURLSummaryHandler(nil, slog.Default())
	stub := newRuntimeAdapterStub()
	h.SetAdapter(stub)

	replyCalled := false
	err := h.HandleFallback(context.Background(), bot.CommandContext{
		Message: transport.Message{
			Msg: "",
			Raw: transport.RawChatLog{
				ChatID:     "room-1",
				Attachment: `{"urls":["https://m.blog.naver.com/fontoylab/224098503131"],"universalScrapData":"{\"requested_url\":\"https://m.blog.naver.com/fontoylab/224098503131\",\"title\":\"네이버 블로그\"}"}`,
			},
		},
		Reply: func(_ context.Context, r bot.Reply) error {
			replyCalled = true
			return nil
		},
	})
	if err != bot.ErrHandled {
		t.Fatalf("HandleFallback should return ErrHandled for attachment URL, got: %v", err)
	}
	if replyCalled {
		t.Fatal("expected no immediate ack reply for attachment URL")
	}
}

func TestURLSummaryHandler_HandleFallback_DisallowedURL(t *testing.T) {
	t.Parallel()
	h := NewURLSummaryHandler(nil, slog.Default())

	err := h.HandleFallback(context.Background(), bot.CommandContext{
		Message: transport.Message{Msg: "https://example.com/article"},
		Reply: func(_ context.Context, _ bot.Reply) error {
			t.Fatal("disallowed URL should not trigger reply")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("HandleFallback should ignore disallowed URL, got: %v", err)
	}
}

func TestURLSummaryHandler_HandleFallback_MediaAttachmentIgnored(t *testing.T) {
	t.Parallel()
	h := NewURLSummaryHandler(nil, slog.Default())

	err := h.HandleFallback(context.Background(), bot.CommandContext{
		Message: transport.Message{
			Msg: "",
			Raw: transport.RawChatLog{
				Attachment: `{"mt":"image/png","thumbnailUrl":"https://talk.kakaocdn.net/dn/thumb.png","url":"https://talk.kakaocdn.net/dn/full.png"}`,
			},
		},
		Reply: func(_ context.Context, _ bot.Reply) error {
			t.Fatal("media attachment should not trigger reply")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("HandleFallback should return nil for media attachment, got: %v", err)
	}
}

func TestURLSummaryHandler_HandleFallback_EmptyMsgAndAttachment(t *testing.T) {
	t.Parallel()
	h := NewURLSummaryHandler(nil, slog.Default())

	err := h.HandleFallback(context.Background(), bot.CommandContext{
		Message: transport.Message{Msg: "", Raw: transport.RawChatLog{Attachment: "{}"}},
		Reply: func(_ context.Context, r bot.Reply) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("HandleFallback should return nil for empty msg+attachment, got: %v", err)
	}
}

func TestExtractFallbackYouTubeURL_FromAttachment(t *testing.T) {
	t.Parallel()
	cmd := bot.CommandContext{
		Message: transport.Message{
			Msg: "",
			Raw: transport.RawChatLog{
				Attachment: `{"urls":["https://www.youtube.com/watch?v=dQw4w9WgXcQ"],"universalScrapData":"{\"requested_url\":\"https://www.youtube.com/watch?v=dQw4w9WgXcQ\",\"title\":\"video\"}"}`,
			},
		},
	}
	got := extractFallbackYouTubeURL(cmd, transport.ParseAttachmentInfo(cmd.Message.Raw.Attachment))
	if got == "" {
		t.Fatal("expected YouTube URL from attachment")
	}
}

func TestURLSummaryHandler_DescriptorID(t *testing.T) {
	t.Parallel()
	h := NewURLSummaryHandler(nil, slog.Default())
	if h.Descriptor().ID != "url-summary" {
		t.Fatalf("expected descriptor ID 'url-summary', got %q", h.Descriptor().ID)
	}
}
