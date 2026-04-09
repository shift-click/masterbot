package command

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/coupang"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/pkg/chart"
	"github.com/shift-click/masterbot/pkg/formatter"
)

type coupangLookupService interface {
	Lookup(ctx context.Context, rawURL string, opts ...coupang.LookupOption) (*coupang.CoupangLookupResult, error)
}

type CoupangHandler struct {
	descriptorSupport
	tracker coupangLookupService
	logger  *slog.Logger
}

type chartDecision string

const (
	chartDecisionRender chartDecision = "render"
	chartDecisionSkip   chartDecision = "skip"
)

type chartDecisionReason string

const (
	chartDecisionReasonEligible                  chartDecisionReason = "eligible"
	chartDecisionReasonInsufficientPoints        chartDecisionReason = "insufficient_points"
	chartDecisionReasonFlatWithoutReferenceDelta chartDecisionReason = "flat_without_reference_delta"
	chartDecisionReasonRenderFailed              chartDecisionReason = "render_failed"
	chartDecisionReasonDeliveryFailed            chartDecisionReason = "delivery_failed"
)

type chartRenderResult string

const (
	chartRenderNotAttempted chartRenderResult = "not_attempted"
	chartRenderSucceeded    chartRenderResult = "succeeded"
	chartRenderFailed       chartRenderResult = "failed"
)

type chartDeliveryResult string

const (
	chartDeliveryNotAttempted chartDeliveryResult = "not_attempted"
	chartDeliverySucceeded    chartDeliveryResult = "succeeded"
	chartDeliveryFailed       chartDeliveryResult = "failed"
)

type coupangFinalMode string

const (
	coupangFinalModeWithChart       coupangFinalMode = "with_chart"
	coupangFinalModeTextOnlySkipped coupangFinalMode = "text_only_skipped"
	coupangFinalModeTextOnlyPartial coupangFinalMode = "text_only_partial"
	coupangFinalModeError           coupangFinalMode = "error"
)

type chartPipelineOutcome struct {
	Decision  chartDecision
	Reason    chartDecisionReason
	Render    chartRenderResult
	Delivery  chartDeliveryResult
	FinalMode coupangFinalMode
}

func NewCoupangHandler(tracker coupangLookupService, logger *slog.Logger) *CoupangHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &CoupangHandler{
		descriptorSupport: newDescriptorSupport("coupang"),
		tracker:           tracker,
		logger:            logger.With("component", "coupang_handler"),
	}
}

func (h *CoupangHandler) Execute(ctx context.Context, cmd bot.CommandContext) error {
	if len(cmd.Args) == 0 {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "쿠팡 상품 URL을 입력해주세요.\n예: 쿠팡 https://link.coupang.com/a/...",
		})
	}

	rawURL := strings.Join(cmd.Args, " ")
	if extracted := providers.ExtractCoupangURL(rawURL); extracted != "" {
		rawURL = extracted
	}
	return h.lookup(ctx, cmd, rawURL)
}

func (h *CoupangHandler) HandleFallback(ctx context.Context, cmd bot.CommandContext) error {
	msg := strings.TrimSpace(cmd.Message.Msg)
	rawAttachment := cmd.Message.Raw.Attachment
	attachmentInfo := transport.ParseAttachmentInfo(rawAttachment)
	if rawAttachment != "" && rawAttachment != "{}" {
		h.logger.Debug("coupang attachment inspected",
			"attachment_kind", attachmentInfo.Kind,
			"attachment_url_source", attachmentInfo.URLSource,
		)
	}
	rawURL := providers.ExtractCoupangURL(msg)
	if rawURL == "" {
		if attachmentInfo.Kind != transport.AttachmentKindLinkPreview || !providers.IsCoupangURL(attachmentInfo.URL) {
			return nil
		}
		rawURL = providers.ExtractCoupangURL(attachmentInfo.URL)
		if rawURL == "" {
			return nil
		}
	}

	// Extract link preview title from KakaoTalk attachment for Fallcent keyword.
	var lookupOpts []coupang.LookupOption
	title := attachmentInfo.Title
	h.logger.Debug("coupang fallback attachment debug",
		"attachment_kind", attachmentInfo.Kind,
		"attachment_url_source", attachmentInfo.URLSource,
		"parsed_title", title,
		"msg", msg,
		"raw_url", rawURL,
	)
	if title != "" {
		lookupOpts = append(lookupOpts, coupang.WithAttachmentTitle(title))
	}

	if err := h.lookup(ctx, cmd, rawURL, lookupOpts...); err != nil {
		h.logger.Warn("fallback coupang lookup failed", "error", err, "message", msg)
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "쿠팡 상품 데이터를 불러오지 못했습니다.",
		})
	}
	return bot.ErrHandled
}

func (h *CoupangHandler) lookup(ctx context.Context, cmd bot.CommandContext, rawURL string, opts ...coupang.LookupOption) error {
	result, err := h.lookupResult(ctx, cmd, rawURL, opts...)
	if err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	if result.RegistrationDeferred && result.Product.TrackID == "" {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "⏳ 가격 이력 보강 중",
		})
	}

	sparklines := make([]int, 0, len(result.History))
	timestamps := make([]time.Time, 0, len(result.History))
	for _, point := range result.History {
		sparklines = append(sparklines, point.Price)
		timestamps = append(timestamps, point.FetchedAt)
	}

	data := h.buildFormatterData(result, sparklines)
	chartLowest, chartHighest := chartReferencePrices(data)
	chartData := buildCoupangChartData(result, sparklines, timestamps, chartLowest, chartHighest, data.AvgPrice)
	correlationID := buildCoupangCorrelationID(cmd.Message.Raw.ID)
	pipeline := h.executeChartPipeline(ctx, cmd, chartData, correlationID)
	chartSent := pipeline.Delivery == chartDeliverySucceeded
	data.HasChart = chartSent

	text := formatter.FormatCoupangPrice(data)
	expectedParts := 1
	if pipeline.Decision == chartDecisionRender {
		expectedParts = 2
	}
	pipeline.FinalMode = finalizeCoupangFinalMode(pipeline, true)
	textReply := bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
		Metadata: map[string]any{
			"request_correlation_id": correlationID,
			"reply_part":             string(transport.ReplyTypeText),
			"composite_expected":     expectedParts,
			"composite_final_part":   true,
			"chart_decision":         string(pipeline.Decision),
			"chart_decision_reason":  string(pipeline.Reason),
			"chart_render_result":    string(pipeline.Render),
			"chart_delivery_result":  string(pipeline.Delivery),
			"chart_final_mode":       string(pipeline.FinalMode),
			"chart_sent":             chartSent,
		},
	}
	textErr := cmd.Reply(ctx, textReply)
	if textErr != nil {
		pipeline.FinalMode = finalizeCoupangFinalMode(pipeline, false)
		h.logger.Warn("coupang text reply failed",
			"product_id", result.Product.ProductID,
			"request_correlation_id", correlationID,
			"chart_decision", pipeline.Decision,
			"chart_decision_reason", pipeline.Reason,
			"chart_render_result", pipeline.Render,
			"chart_delivery_result", pipeline.Delivery,
			"final_mode", pipeline.FinalMode,
			"error", textErr,
		)
		return textErr
	}

	h.logger.Info("coupang reply prepared",
		"product_id", result.Product.ProductID,
		"stale", result.IsStale,
		"sample_count", result.SampleCount,
		"registration_stage", result.RegistrationStage,
		"response_mode", result.ResponseMode,
		"registration_deferred", result.RegistrationDeferred,
		"read_refresh", result.ReadRefresh,
		"refresh_requested", result.RefreshRequested,
		"request_correlation_id", correlationID,
		"chart_decision", pipeline.Decision,
		"chart_decision_reason", pipeline.Reason,
		"chart_render_result", pipeline.Render,
		"chart_delivery_result", pipeline.Delivery,
		"final_mode", pipeline.FinalMode,
		"chart_sent", chartSent,
	)
	return nil
}

func (h *CoupangHandler) lookupResult(ctx context.Context, cmd bot.CommandContext, rawURL string, opts ...coupang.LookupOption) (*coupang.CoupangLookupResult, error) {
	result, err := h.tracker.Lookup(ctx, rawURL, opts...)
	if err == nil {
		return result, nil
	}
	if errors.Is(err, coupang.ErrCoupangRegistrationLimited) {
		return nil, h.replyIfCommand(ctx, cmd, "지금은 신규 쿠팡 상품 등록이 많아서 잠시 후 다시 시도해주세요.")
	}
	if cmd.Command != "" {
		return nil, cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "쿠팡 상품 데이터를 불러오지 못했습니다.",
		})
	}
	return nil, err
}

func (h *CoupangHandler) buildFormatterData(result *coupang.CoupangLookupResult, sparklines []int) formatter.CoupangPriceData {
	data := formatter.CoupangPriceData{
		Name:                result.Product.Name,
		CurrentPrice:        result.Product.Snapshot.Price,
		Prices:              sparklines,
		HistorySpanDays:     result.HistorySpanDays,
		SampleCount:         result.SampleCount,
		ProductURL:          formatProductURL(result.Product),
		ComparativeMinPrice: result.Product.SourceMapping.ComparativeMinPrice,
		StatsEligible:       result.StatsEligible,
		IsStale:             result.IsStale,
		LastObservedAt:      result.LastObservedAt,
		RefreshStatus:       string(result.ReadRefresh),
		RefreshRequested:    result.RefreshRequested,
		DeferredNotice:      result.RegistrationDeferred || result.RescueDeferred,
	}
	if stats := result.Stats; stats != nil {
		data.MinPrice = stats.MinPrice
		data.MinDate = stats.MinDate
		data.MaxPrice = stats.MaxPrice
		data.MaxDate = stats.MaxDate
		data.AvgPrice = stats.AvgPrice
	}
	return data
}

func chartReferencePrices(data formatter.CoupangPriceData) (int, int) {
	chartLowest := data.ComparativeMinPrice
	if data.StatsEligible && data.MinPrice > 0 {
		chartLowest = data.MinPrice
	}
	chartHighest := 0
	if data.StatsEligible && data.MaxPrice > 0 {
		chartHighest = data.MaxPrice
	}
	return chartLowest, chartHighest
}

func buildCoupangChartData(result *coupang.CoupangLookupResult, sparklines []int, timestamps []time.Time, chartLowest, chartHighest, avgPrice int) chart.PriceChartData {
	return chart.PriceChartData{
		Prices:       sparklines,
		Timestamps:   timestamps,
		CurrentPrice: result.Product.Snapshot.Price,
		LowestPrice:  chartLowest,
		HighestPrice: chartHighest,
		MeanPrice:    avgPrice,
		ProductName:  result.Product.Name,
		PeriodLabel:  formatPeriodLabel(result.HistorySpanDays),
	}
}

func (h *CoupangHandler) executeChartPipeline(ctx context.Context, cmd bot.CommandContext, chartData chart.PriceChartData, correlationID string) chartPipelineOutcome {
	decision, reason := decideChart(chartData.Prices, chartData.LowestPrice, chartData.HighestPrice)
	outcome := chartPipelineOutcome{
		Decision: decision,
		Reason:   reason,
		Render:   chartRenderNotAttempted,
		Delivery: chartDeliveryNotAttempted,
	}
	if decision != chartDecisionRender {
		return outcome
	}

	b64, chartErr := chart.DrawPriceChartBase64(chartData, chart.DefaultConfig())
	if chartErr != nil {
		outcome.Render = chartRenderFailed
		outcome.Delivery = chartDeliveryFailed
		outcome.Reason = chartDecisionReasonRenderFailed
		h.logger.Debug("chart generation failed", "error", chartErr, "request_correlation_id", correlationID)
		return outcome
	}

	outcome.Render = chartRenderSucceeded
	imageReply := bot.Reply{
		Type:        transport.ReplyTypeImage,
		ImageBase64: b64,
		Metadata: map[string]any{
			"request_correlation_id": correlationID,
			"reply_part":             string(transport.ReplyTypeImage),
			"composite_expected":     2,
			"composite_final_part":   false,
			"chart_decision":         string(outcome.Decision),
			"chart_decision_reason":  string(outcome.Reason),
			"chart_render_result":    string(outcome.Render),
		},
	}
	if replyErr := cmd.Reply(ctx, imageReply); replyErr != nil {
		outcome.Delivery = chartDeliveryFailed
		outcome.Reason = chartDecisionReasonDeliveryFailed
		h.logger.Warn("chart image reply failed", "error", replyErr, "request_correlation_id", correlationID)
		return outcome
	}

	outcome.Delivery = chartDeliverySucceeded
	return outcome
}

func decideChart(prices []int, lowest, highest int) (chartDecision, chartDecisionReason) {
	if len(prices) < 2 {
		return chartDecisionSkip, chartDecisionReasonInsufficientPoints
	}
	if shouldShowChart(prices, lowest, highest) {
		return chartDecisionRender, chartDecisionReasonEligible
	}
	return chartDecisionSkip, chartDecisionReasonFlatWithoutReferenceDelta
}

func finalizeCoupangFinalMode(outcome chartPipelineOutcome, textDelivered bool) coupangFinalMode {
	if !textDelivered {
		return coupangFinalModeError
	}
	switch outcome.Decision {
	case chartDecisionSkip:
		return coupangFinalModeTextOnlySkipped
	case chartDecisionRender:
		if outcome.Delivery == chartDeliverySucceeded {
			return coupangFinalModeWithChart
		}
		return coupangFinalModeTextOnlyPartial
	default:
		return coupangFinalModeError
	}
}

func buildCoupangCorrelationID(requestID string) string {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return fmt.Sprintf("coupang:%d", time.Now().UnixNano())
	}
	return "coupang:" + requestID
}

// shouldShowChart returns true if there's enough meaningful data to show a chart.
// Shows chart when prices vary, or when reference prices (lowest/highest) differ from current.
func shouldShowChart(prices []int, lowest, highest int) bool {
	if len(prices) < 2 {
		return false
	}
	// Check if sparkline prices themselves vary
	first := prices[0]
	for _, p := range prices[1:] {
		if p != first {
			return true
		}
	}
	// Even if flat, show chart if reference prices add context
	current := prices[len(prices)-1]
	if lowest > 0 && lowest != current {
		return true
	}
	if highest > 0 && highest != current {
		return true
	}
	return false
}

func formatPeriodLabel(spanDays int) string {
	switch {
	case spanDays <= 0:
		return ""
	case spanDays <= 7:
		return "1주"
	case spanDays <= 30:
		return "1개월"
	case spanDays <= 90:
		return "3개월"
	case spanDays <= 180:
		return "6개월"
	default:
		return "1년+"
	}
}

func formatProductURL(product coupang.CoupangProductRecord) string {
	rawURL := fmt.Sprintf("coupang.com/vp/products/%s", product.ProductID)
	query := make([]string, 0, 2)
	if product.ItemID != "" {
		query = append(query, "itemId="+product.ItemID)
	}
	if product.VendorItemID != "" {
		query = append(query, "vendorItemId="+product.VendorItemID)
	}
	if len(query) == 0 {
		return rawURL
	}
	return rawURL + "?" + strings.Join(query, "&")
}

func (h *CoupangHandler) replyIfCommand(ctx context.Context, cmd bot.CommandContext, text string) error {
	if cmd.Command == "" {
		return nil
	}
	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
	})
}
