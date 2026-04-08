package command

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
)

const defaultMaxWorkers = 3

// YouTubeHandler handles YouTube video summarization.
type YouTubeHandler struct {
	descriptorSupport
	adapter transport.RuntimeAdapter
	gemini  *providers.GeminiClient
	logger  *slog.Logger
	exec    *SummaryExecutor
}

// NewYouTubeHandler creates a YouTube summary handler.
// The adapter can be nil at creation time and set later via SetAdapter.
func NewYouTubeHandler(
	gemini *providers.GeminiClient,
	logger *slog.Logger,
) *YouTubeHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &YouTubeHandler{
		descriptorSupport: newDescriptorSupport("youtube"),
		gemini:            gemini,
		logger:            logger,
		exec:              NewSummaryExecutor(logger, defaultMaxWorkers, 3*time.Minute),
	}
}

// SetAdapter sets the transport adapter for async responses.
// Must be called before the handler processes any messages.
func (h *YouTubeHandler) SetAdapter(adapter transport.RuntimeAdapter) {
	h.adapter = adapter
}

func (h *YouTubeHandler) SetExecutor(exec *SummaryExecutor) {
	if exec == nil {
		return
	}
	h.exec = exec
}

// Execute handles "유튜브 <URL>" command input.
func (h *YouTubeHandler) Execute(ctx context.Context, cmd bot.CommandContext) error {
	rawURL := strings.Join(cmd.Args, " ")
	if rawURL == "" {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "유튜브 URL을 입력해주세요. 예: 유튜브 https://youtu.be/xxxxx",
		})
	}

	videoURL := providers.ExtractYouTubeURL(rawURL)
	if videoURL == "" {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "유효한 유튜브 URL이 아닙니다.",
		})
	}

	return h.startSummarize(ctx, cmd, videoURL)
}

// HandleFallback detects YouTube URLs in messages.
func (h *YouTubeHandler) HandleFallback(ctx context.Context, cmd bot.CommandContext) error {
	attachmentInfo := transport.ParseAttachmentInfo(cmd.Message.Raw.Attachment)
	if cmd.Message.Raw.Attachment != "" && cmd.Message.Raw.Attachment != "{}" {
		h.logger.Debug("youtube attachment inspected",
			"attachment_kind", attachmentInfo.Kind,
			"attachment_url_source", attachmentInfo.URLSource,
		)
	}

	videoURL := extractFallbackYouTubeURL(cmd, attachmentInfo)
	if videoURL == "" {
		return nil
	}

	if err := h.startSummarize(ctx, cmd, videoURL); err != nil {
		h.logger.Warn("youtube fallback failed", "error", err)
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "영상 요약에 실패했습니다.",
		})
	}
	return bot.ErrHandled
}

func (h *YouTubeHandler) startSummarize(ctx context.Context, cmd bot.CommandContext, videoURL string) error {
	if h.exec == nil {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "현재 요약 서비스를 사용할 수 없습니다.",
		})
	}

	room := cmd.Message.Raw.ChatID
	err := h.exec.Submit(ctx, "youtube", func(jobCtx context.Context) {
		h.summarizeAsync(jobCtx, room, videoURL)
	})
	if errors.Is(err, ErrSummaryExecutorBusy) {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "현재 요약 처리가 많습니다. 잠시 후 다시 시도해주세요.",
		})
	}
	if errors.Is(err, ErrSummaryExecutorUnavailable) || errors.Is(err, context.Canceled) {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "요약 작업이 종료 중입니다. 잠시 후 다시 시도해주세요.",
		})
	}
	return err
}

func (h *YouTubeHandler) summarizeAsync(ctx context.Context, room, videoURL string) {
	if h.gemini == nil {
		h.sendError(ctx, room, "AI 요약 기능이 설정되지 않았습니다.")
		return
	}

	summary, err := h.gemini.SummarizeYouTube(ctx, videoURL)
	if err != nil {
		h.logger.Warn("gemini summarization failed", "error", err, "url", videoURL)
		h.sendError(ctx, room, "AI 요약 생성에 실패했습니다.")
		return
	}

	_ = h.adapter.Reply(ctx, transport.ReplyRequest{
		Type: transport.ReplyTypeText,
		Room: room,
		Data: summary,
	})
}

func (h *YouTubeHandler) sendError(ctx context.Context, room, msg string) {
	_ = h.adapter.Reply(ctx, transport.ReplyRequest{
		Type: transport.ReplyTypeText,
		Room: room,
		Data: fmt.Sprintf("❌ %s", msg),
	})
}

// extractFallbackYouTubeURL extracts a YouTube URL from a message, checking
// both the message text and the KakaoTalk link preview attachment.
func extractFallbackYouTubeURL(cmd bot.CommandContext, attachmentInfo transport.AttachmentInfo) string {
	msg := strings.TrimSpace(cmd.Message.Msg)
	if providers.IsYouTubeURL(msg) {
		if u := providers.ExtractYouTubeURL(msg); u != "" {
			return u
		}
	}
	if attachmentInfo.Kind == transport.AttachmentKindLinkPreview && providers.IsYouTubeURL(attachmentInfo.URL) {
		return providers.ExtractYouTubeURL(attachmentInfo.URL)
	}
	return ""
}

var (
	_ bot.Handler         = (*YouTubeHandler)(nil)
	_ bot.FallbackHandler = (*YouTubeHandler)(nil)
)
