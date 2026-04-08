package command

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/pkg/formatter"
)

// URLSummaryHandler handles web URL summarization via Gemini URL Context.
type URLSummaryHandler struct {
	descriptorSupport
	adapter transport.RuntimeAdapter
	gemini  *providers.GeminiClient
	logger  *slog.Logger
	exec    *SummaryExecutor
}

// NewURLSummaryHandler creates a URL summary handler.
func NewURLSummaryHandler(
	gemini *providers.GeminiClient,
	logger *slog.Logger,
) *URLSummaryHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &URLSummaryHandler{
		descriptorSupport: newDescriptorSupport("url-summary"),
		gemini:            gemini,
		logger:            logger.With("component", "url_summary_handler"),
		exec:              NewSummaryExecutor(logger, defaultMaxWorkers, 3*time.Minute),
	}
}

// SetAdapter sets the transport adapter for async responses.
func (h *URLSummaryHandler) SetAdapter(adapter transport.RuntimeAdapter) {
	h.adapter = adapter
}

func (h *URLSummaryHandler) SetExecutor(exec *SummaryExecutor) {
	if exec == nil {
		return
	}
	h.exec = exec
}

// Execute handles "/요약 <URL>" command input.
func (h *URLSummaryHandler) Execute(ctx context.Context, cmd bot.CommandContext) error {
	rawURL := strings.Join(cmd.Args, " ")
	if rawURL == "" {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "요약할 URL을 입력해주세요. 예: 요약 https://example.com/article",
		})
	}

	webURL := providers.ExtractWebURL(rawURL)
	if webURL == "" {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "유효한 URL이 아닙니다.",
		})
	}

	return h.startSummarize(ctx, cmd, webURL)
}

// HandleFallback detects HTTP(S) URLs in messages.
func (h *URLSummaryHandler) HandleFallback(ctx context.Context, cmd bot.CommandContext) error {
	attachmentInfo := transport.ParseAttachmentInfo(cmd.Message.Raw.Attachment)
	if cmd.Message.Raw.Attachment != "" && cmd.Message.Raw.Attachment != "{}" {
		h.logger.Debug("url summary attachment inspected",
			"attachment_kind", attachmentInfo.Kind,
			"attachment_url_source", attachmentInfo.URLSource,
		)
	}

	webURL := extractFallbackWebURL(cmd, attachmentInfo)
	if webURL == "" {
		return nil
	}

	if !isHTMLContent(ctx, webURL) {
		return nil
	}

	if err := h.startSummarize(ctx, cmd, webURL); err != nil {
		h.logger.Warn("url summary fallback failed", "error", err)
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "링크 요약에 실패했습니다.",
		})
	}
	return bot.ErrHandled
}

func (h *URLSummaryHandler) startSummarize(ctx context.Context, cmd bot.CommandContext, webURL string) error {
	if h.exec == nil {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "현재 요약 서비스를 사용할 수 없습니다.",
		})
	}

	room := cmd.Message.Raw.ChatID
	err := h.exec.Submit(ctx, "url-summary", func(jobCtx context.Context) {
		h.summarizeAsync(jobCtx, room, webURL)
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

func (h *URLSummaryHandler) summarizeAsync(ctx context.Context, room, webURL string) {
	if h.gemini == nil {
		h.sendError(ctx, room, "AI 요약 기능이 설정되지 않았습니다.")
		return
	}

	// X/Twitter: use fxtwitter API directly — x.com blocks HTML scraping
	// and Gemini URLContext receives only the error page.
	if providers.ClassifyAutoSummaryURL(webURL) == providers.SummaryURLKindX {
		h.summarizeTwitterAsync(ctx, room, webURL)
		return
	}

	// Strategy: self-fetch first, URLContext fallback.
	// Most URLs (especially Korean sites) serve SSR HTML, making self-fetch
	// faster and cheaper than URLContext. URLContext is reserved for JS-heavy
	// sites where self-fetch yields insufficient text.

	title, body, fetchErr := providers.FetchAndExtractText(ctx, webURL)
	if fetchErr == nil && body != "" {
		summary, err := h.gemini.SummarizeText(ctx, title, body)
		if err != nil {
			h.logger.Warn("gemini text summarization failed", "error", err, "url", webURL)
			h.sendError(ctx, room, "AI 요약 생성에 실패했습니다.")
			return
		}
		_ = h.adapter.Reply(ctx, transport.ReplyRequest{
			Type: transport.ReplyTypeText,
			Room: room,
			Data: formatter.FormatURLSummary(summary),
		})
		return
	}

	// Self-fetch failed or body empty — fall back to Gemini URLContext.
	if fetchErr != nil {
		h.logger.Info("self-fetch failed, trying gemini url context", "error", fetchErr, "url", webURL)
	} else {
		h.logger.Info("self-fetch returned empty body, trying gemini url context", "url", webURL)
	}

	result, err := h.gemini.SummarizeURL(ctx, webURL)
	if err != nil {
		h.logger.Warn("gemini url context failed", "error", err, "url", webURL)
		h.sendError(ctx, room, "이 링크는 요약할 수 없습니다.")
		return
	}

	// Check structured URLRetrievalStatus first, then text heuristic as backup.
	if result.IsRetrievalFailure() || isBrowseFailure(result.Summary) {
		h.logger.Warn("gemini url context retrieval failure",
			"status", result.RetrievalStatus,
			"hasStatus", result.HasRetrievalStatus,
			"url", webURL)
		h.sendError(ctx, room, "이 링크는 요약할 수 없습니다.")
		return
	}

	_ = h.adapter.Reply(ctx, transport.ReplyRequest{
		Type: transport.ReplyTypeText,
		Room: room,
		Data: formatter.FormatURLSummary(result.Summary),
	})
}

// summarizeTwitterAsync fetches a tweet via fxtwitter API and sends the result.
// Short tweets are formatted directly; longer content goes through Gemini.
func (h *URLSummaryHandler) summarizeTwitterAsync(ctx context.Context, room, webURL string) {
	tweet, err := providers.FetchTweet(ctx, webURL)
	if err != nil {
		h.logger.Warn("fxtwitter fetch failed", "error", err, "url", webURL)
		h.sendError(ctx, room, "이 트윗은 요약할 수 없습니다.")
		return
	}

	var text string
	if formatter.TweetNeedsAISummary(tweet.Text) {
		summary, sumErr := h.gemini.SummarizeText(ctx, "@"+tweet.AuthorScreenName, tweet.Text)
		if sumErr != nil {
			h.logger.Warn("gemini tweet summarization failed", "error", sumErr, "url", webURL)
			// Fall back to direct format on Gemini error.
			text = formatter.FormatTweet(tweet)
		} else {
			text = formatter.FormatURLSummary(summary)
		}
	} else {
		text = formatter.FormatTweet(tweet)
	}

	_ = h.adapter.Reply(ctx, transport.ReplyRequest{
		Type: transport.ReplyTypeText,
		Room: room,
		Data: text,
	})
}

func (h *URLSummaryHandler) sendError(ctx context.Context, room, msg string) {
	_ = h.adapter.Reply(ctx, transport.ReplyRequest{
		Type: transport.ReplyTypeText,
		Room: room,
		Data: fmt.Sprintf("❌ %s", msg),
	})
}

// isHTMLContent checks if the URL serves HTML content via a HEAD request.
// Returns true on timeout or error (optimistic).
func isHTMLContent(ctx context.Context, rawURL string) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return true
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; JucoBot/2.0)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return true
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	return ct == "" || strings.Contains(ct, "text/html") || strings.Contains(ct, "text/plain")
}

// extractFallbackWebURL extracts a web URL from a message, checking both
// the message text and the KakaoTalk link preview attachment.
func extractFallbackWebURL(cmd bot.CommandContext, attachmentInfo transport.AttachmentInfo) string {
	msg := strings.TrimSpace(cmd.Message.Msg)
	if providers.IsWebURL(msg) {
		if u := providers.ExtractWebURL(msg); u != "" && providers.IsAutoSummaryURL(u) {
			return u
		}
	}
	if attachmentInfo.Kind == transport.AttachmentKindLinkPreview && providers.IsWebURL(attachmentInfo.URL) {
		if u := providers.ExtractWebURL(attachmentInfo.URL); u != "" && providers.IsAutoSummaryURL(u) {
			return u
		}
	}
	return ""
}

// isBrowseFailure detects Gemini URLContext responses that indicate the URL
// could not be fetched. Uses keyword matching plus a short-response heuristic
// to catch novel phrasings.
func isBrowseFailure(summary string) bool {
	lower := strings.ToLower(summary)
	for _, ind := range browseFailureIndicators {
		if strings.Contains(lower, ind) {
			return true
		}
	}
	// Heuristic: short response (< 200 runes) containing negative keywords
	// is likely a browse failure with novel phrasing. Gemini failure messages
	// can be up to ~190 runes when they include the full URL.
	if len([]rune(summary)) < 200 {
		for _, kw := range browseFailureNegativeKeywords {
			if strings.Contains(lower, kw) {
				return true
			}
		}
	}
	return false
}

var browseFailureIndicators = []string{
	// English
	"could not be browsed",
	"cannot be browsed",
	"unable to access",
	"unable to browse",
	"failed to retrieve",
	"failed to fetch",
	"couldn't access",
	"cannot access the url",
	"could not access",
	"couldn't retrieve",
	"cannot retrieve",
	"i can't access",
	"i cannot access",
	"page is not accessible",
	"website is not accessible",
	// Korean
	"url을 열 수 없",
	"접근할 수 없",
	"접속할 수 없",
	"접속하여 내용을 가져올 수 없",
	"페이지를 불러올 수 없",
	"불러오는 데 실패",
	"내용을 확인할 수 없",
	"내용을 가져올 수 없",
	"요약할 수 없",
	"요약해 드릴 수 없",
	"접속이 불가",
	"차단되었거나",
	"웹 페이지에 접근",
	"존재하지 않을 수 있",
}

// browseFailureNegativeKeywords are checked only for short (< 200 rune) responses.
var browseFailureNegativeKeywords = []string{
	"죄송합니다",
	"실패",
	"불가능",
	"error",
	"sorry",
}

var (
	_ bot.Handler         = (*URLSummaryHandler)(nil)
	_ bot.FallbackHandler = (*URLSummaryHandler)(nil)
)
