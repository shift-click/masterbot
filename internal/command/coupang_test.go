package command

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/coupang"
	"github.com/shift-click/masterbot/internal/scraper"
	"github.com/shift-click/masterbot/internal/store"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/pkg/chart"
)

type stubCoupangLookupService struct {
	result  *scraper.CoupangLookupResult
	err     error
	lastURL string
}

func (s *stubCoupangLookupService) Lookup(_ context.Context, rawURL string, _ ...coupang.LookupOption) (*scraper.CoupangLookupResult, error) {
	s.lastURL = rawURL
	return s.result, s.err
}

func TestCoupangHandlerExecuteFormatsMinimalPriceResponse(t *testing.T) {
	t.Parallel()

	lookup := &stubCoupangLookupService{
		result: &scraper.CoupangLookupResult{
			Product: store.CoupangProductRecord{
				ProductID: "p1",
				Name:      "테스트 상품",
				Snapshot: store.CoupangSnapshot{
					TrackID:    "p1",
					Price:      21900,
					LastSeenAt: time.Now().Add(-3 * time.Hour),
				},
			},
			History: []store.PricePoint{
				{Price: 25000, FetchedAt: time.Now().Add(-2 * 24 * time.Hour)},
				{Price: 23000, FetchedAt: time.Now().Add(-24 * time.Hour)},
				{Price: 21900, FetchedAt: time.Now().Add(-3 * time.Hour)},
			},
			Stats: &store.PriceStats{
				MinPrice: 21900,
				MinDate:  time.Date(2026, 3, 12, 0, 0, 0, 0, time.UTC),
				MaxPrice: 25000,
				MaxDate:  time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC),
				AvgPrice: 23300,
			},
			SampleCount:     3,
			DistinctDays:    3,
			HistorySpanDays: 3,
			StatsEligible:   true,
		},
	}
	handler := NewCoupangHandler(lookup, nil)

	var reply bot.Reply
	err := handler.Execute(context.Background(), bot.CommandContext{
		Command: "쿠팡",
		Args:    []string{"https://www.coupang.com/vp/products/1"},
		Reply: func(_ context.Context, r bot.Reply) error {
			reply = r
			return nil
		},
		Message: transport.Message{},
		Now:     time.Now,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if reply.Type != transport.ReplyTypeText {
		t.Fatalf("reply type = %s, want text", reply.Type)
	}
	if reply.Text == "" {
		t.Fatal("expected text reply")
	}
	if strings.Contains(reply.Text, "마지막 관측") {
		t.Fatalf("reply text %q should not contain stale observed label", reply.Text)
	}
	if strings.Contains(reply.Text, "stale") {
		t.Fatalf("reply text %q should not contain stale marker", reply.Text)
	}
	if want := "현재가 21,900원"; !strings.Contains(reply.Text, want) {
		t.Fatalf("reply text %q does not contain %q", reply.Text, want)
	}
	if want := "최저가 21,900원"; !strings.Contains(reply.Text, want) {
		t.Fatalf("reply text %q does not contain %q", reply.Text, want)
	}
	if want := "0.0%"; !strings.Contains(reply.Text, want) {
		t.Fatalf("reply text %q does not contain %q", reply.Text, want)
	}
	unexpected := []string{
		"로컬 표본",
		"최근 관측 기준 최신 응답입니다",
		"폴센트",
	}
	for _, s := range unexpected {
		if strings.Contains(reply.Text, s) {
			t.Fatalf("reply text %q should not contain %q", reply.Text, s)
		}
	}
}

func TestCoupangHandlerExecuteRequiresURL(t *testing.T) {
	t.Parallel()

	handler := NewCoupangHandler(&stubCoupangLookupService{}, nil)

	var reply bot.Reply
	err := handler.Execute(context.Background(), bot.CommandContext{
		Command: "쿠팡",
		Reply: func(_ context.Context, r bot.Reply) error {
			reply = r
			return nil
		},
		Now: time.Now,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(reply.Text, "쿠팡 상품 URL") {
		t.Fatalf("reply text %q does not ask for URL", reply.Text)
	}
}

func TestCoupangHandlerFallbackRepliesOnLookupError(t *testing.T) {
	t.Parallel()

	handler := NewCoupangHandler(&stubCoupangLookupService{err: errors.New("lookup failed")}, nil)

	var reply bot.Reply
	err := handler.HandleFallback(context.Background(), bot.CommandContext{
		Message: transport.Message{
			Msg: "https://www.coupang.com/vp/products/1",
		},
		Reply: func(_ context.Context, r bot.Reply) error {
			reply = r
			return nil
		},
		Now: time.Now,
	})
	if err != nil {
		t.Fatalf("handle fallback: %v", err)
	}
	if !strings.Contains(reply.Text, "쿠팡 상품 데이터를 불러오지 못했습니다") {
		t.Fatalf("reply text %q does not contain fallback failure message", reply.Text)
	}
}

func TestCoupangHandlerFallbackUsesAttachmentURL(t *testing.T) {
	t.Parallel()

	lookup := &stubCoupangLookupService{
		result: &scraper.CoupangLookupResult{
			Product: store.CoupangProductRecord{
				ProductID: "p-attachment",
				Name:      "attachment only 상품",
				Snapshot: store.CoupangSnapshot{
					TrackID:    "p-attachment",
					Price:      14900,
					LastSeenAt: time.Now().Add(-time.Hour),
				},
			},
			History: []store.PricePoint{
				{Price: 15900, FetchedAt: time.Now().Add(-48 * time.Hour)},
				{Price: 14900, FetchedAt: time.Now().Add(-time.Hour)},
			},
			SampleCount:     2,
			DistinctDays:    2,
			HistorySpanDays: 2,
			StatsEligible:   false,
		},
	}
	handler := NewCoupangHandler(lookup, nil)

	var reply bot.Reply
	err := handler.HandleFallback(context.Background(), bot.CommandContext{
		Message: transport.Message{
			Msg: "",
			Raw: transport.RawChatLog{
				Attachment: `{"urls":["https://link.coupang.com/a/d7PdNX"],"universalScrapData":"{\"requested_url\":\"https://link.coupang.com/a/d7PdNX\",\"title\":\"attachment only 상품\",\"description\":\"쿠팡 상품\"}"}`,
			},
		},
		Reply: func(_ context.Context, r bot.Reply) error {
			reply = r
			return nil
		},
		Now: time.Now,
	})
	if !errors.Is(err, bot.ErrHandled) {
		t.Fatalf("handle fallback err = %v, want bot.ErrHandled", err)
	}
	if lookup.lastURL != "https://link.coupang.com/a/d7PdNX" {
		t.Fatalf("lookup url = %q", lookup.lastURL)
	}
	if reply.Type != transport.ReplyTypeText {
		t.Fatalf("reply type = %s, want text", reply.Type)
	}
	if !strings.Contains(reply.Text, "현재가 14,900원") {
		t.Fatalf("reply text %q does not contain expected price", reply.Text)
	}
}

func TestCoupangHandlerFallbackIgnoresMediaAttachment(t *testing.T) {
	t.Parallel()

	handler := NewCoupangHandler(&stubCoupangLookupService{}, nil)
	err := handler.HandleFallback(context.Background(), bot.CommandContext{
		Message: transport.Message{
			Msg: "",
			Raw: transport.RawChatLog{
				Attachment: `{"mt":"image/png","thumbnailUrl":"https://talk.kakaocdn.net/dn/thumb.png","url":"https://talk.kakaocdn.net/dn/full.png"}`,
			},
		},
		Reply: func(_ context.Context, _ bot.Reply) error {
			t.Fatal("media attachment should not trigger coupang reply")
			return nil
		},
		Now: time.Now,
	})
	if err != nil {
		t.Fatalf("handle fallback: %v", err)
	}
}

func TestDecideChartReasonCodes(t *testing.T) {
	t.Parallel()

	decision, reason := decideChart([]int{10000}, 0, 0)
	if decision != chartDecisionSkip || reason != chartDecisionReasonInsufficientPoints {
		t.Fatalf("single-point decision=%s reason=%s", decision, reason)
	}

	decision, reason = decideChart([]int{10000, 10000, 10000}, 10000, 10000)
	if decision != chartDecisionSkip || reason != chartDecisionReasonFlatWithoutReferenceDelta {
		t.Fatalf("flat decision=%s reason=%s", decision, reason)
	}

	decision, reason = decideChart([]int{10000, 11000, 10500}, 0, 0)
	if decision != chartDecisionRender || reason != chartDecisionReasonEligible {
		t.Fatalf("varying decision=%s reason=%s", decision, reason)
	}
}

func TestFinalizeCoupangFinalMode(t *testing.T) {
	t.Parallel()

	mode := finalizeCoupangFinalMode(chartPipelineOutcome{
		Decision: chartDecisionRender,
		Delivery: chartDeliverySucceeded,
	}, true)
	if mode != coupangFinalModeWithChart {
		t.Fatalf("mode with chart = %s", mode)
	}

	mode = finalizeCoupangFinalMode(chartPipelineOutcome{
		Decision: chartDecisionSkip,
		Delivery: chartDeliveryNotAttempted,
	}, true)
	if mode != coupangFinalModeTextOnlySkipped {
		t.Fatalf("mode skipped = %s", mode)
	}

	mode = finalizeCoupangFinalMode(chartPipelineOutcome{
		Decision: chartDecisionRender,
		Delivery: chartDeliveryFailed,
	}, true)
	if mode != coupangFinalModeTextOnlyPartial {
		t.Fatalf("mode partial = %s", mode)
	}

	mode = finalizeCoupangFinalMode(chartPipelineOutcome{
		Decision: chartDecisionRender,
		Delivery: chartDeliverySucceeded,
	}, false)
	if mode != coupangFinalModeError {
		t.Fatalf("mode error = %s", mode)
	}
}

func TestCoupangHandlerLookupResultRegistrationLimited(t *testing.T) {
	t.Parallel()

	handler := NewCoupangHandler(&stubCoupangLookupService{err: coupang.ErrCoupangRegistrationLimited}, nil)

	t.Run("command reply", func(t *testing.T) {
		t.Parallel()

		var reply bot.Reply
		result, err := handler.lookupResult(context.Background(), bot.CommandContext{
			Command: "쿠팡",
			Reply: func(_ context.Context, r bot.Reply) error {
				reply = r
				return nil
			},
		}, "https://www.coupang.com/vp/products/1")
		if err != nil {
			t.Fatalf("lookupResult: %v", err)
		}
		if result != nil {
			t.Fatalf("result = %#v, want nil", result)
		}
		if !strings.Contains(reply.Text, "신규 쿠팡 상품 등록이 많아서") {
			t.Fatalf("reply text %q does not contain registration limit notice", reply.Text)
		}
	})

	t.Run("fallback noop", func(t *testing.T) {
		t.Parallel()

		replied := false
		result, err := handler.lookupResult(context.Background(), bot.CommandContext{
			Reply: func(_ context.Context, r bot.Reply) error {
				replied = true
				return nil
			},
		}, "https://www.coupang.com/vp/products/1")
		if err != nil {
			t.Fatalf("lookupResult: %v", err)
		}
		if result != nil {
			t.Fatalf("result = %#v, want nil", result)
		}
		if replied {
			t.Fatal("expected fallback path to skip reply")
		}
	})
}

func TestCoupangHandlerLookupDeferredRegistrationReply(t *testing.T) {
	t.Parallel()

	handler := NewCoupangHandler(&stubCoupangLookupService{
		result: &scraper.CoupangLookupResult{
			Product:              store.CoupangProductRecord{Name: "지연 등록 상품"},
			RegistrationDeferred: true,
		},
	}, nil)

	var reply bot.Reply
	err := handler.lookup(context.Background(), bot.CommandContext{
		Command: "쿠팡",
		Reply: func(_ context.Context, r bot.Reply) error {
			reply = r
			return nil
		},
	}, "https://www.coupang.com/vp/products/1")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if reply.Type != transport.ReplyTypeText {
		t.Fatalf("reply type = %s, want text", reply.Type)
	}
	if reply.Text != "⏳ 가격 이력 보강 중" {
		t.Fatalf("reply text = %q", reply.Text)
	}
}

func TestCoupangHandlerHandleFallbackSkipsNonCoupangAttachment(t *testing.T) {
	t.Parallel()

	handler := NewCoupangHandler(&stubCoupangLookupService{}, nil)
	err := handler.HandleFallback(context.Background(), bot.CommandContext{
		Message: transport.Message{
			Raw: transport.RawChatLog{
				Attachment: `{"title":"news","url":"https://example.com/article"}`,
			},
		},
		Reply: func(_ context.Context, r bot.Reply) error {
			t.Fatalf("unexpected reply: %#v", r)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("handle fallback: %v", err)
	}
}

func TestExecuteChartPipelineDeliveryFailure(t *testing.T) {
	t.Parallel()

	handler := NewCoupangHandler(&stubCoupangLookupService{}, nil)
	outcome := handler.executeChartPipeline(context.Background(), bot.CommandContext{
		Reply: func(_ context.Context, r bot.Reply) error {
			if r.Type == transport.ReplyTypeImage {
				return errors.New("image send failed")
			}
			return nil
		},
	}, chart.PriceChartData{
		Prices:       []int{10000, 11000, 10500},
		Timestamps:   []time.Time{time.Now().Add(-2 * time.Hour), time.Now().Add(-time.Hour), time.Now()},
		CurrentPrice: 10500,
		ProductName:  "차트 상품",
		PeriodLabel:  "1주",
	}, "corr-1")

	if outcome.Decision != chartDecisionRender {
		t.Fatalf("decision = %s, want render", outcome.Decision)
	}
	if outcome.Render != chartRenderSucceeded {
		t.Fatalf("render = %s, want succeeded", outcome.Render)
	}
	if outcome.Delivery != chartDeliveryFailed {
		t.Fatalf("delivery = %s, want failed", outcome.Delivery)
	}
	if outcome.Reason != chartDecisionReasonDeliveryFailed {
		t.Fatalf("reason = %s, want delivery_failed", outcome.Reason)
	}
}

func TestCoupangFormattingHelpers(t *testing.T) {
	t.Parallel()

	if got := replyIfCommandHelperText(t); got != "명령 응답" {
		t.Fatalf("replyIfCommand helper text = %q", got)
	}
	if got := formatPeriodLabel(0); got != "" {
		t.Fatalf("formatPeriodLabel(0) = %q", got)
	}
	if got := formatPeriodLabel(7); got != "1주" {
		t.Fatalf("formatPeriodLabel(7) = %q", got)
	}
	if got := formatPeriodLabel(30); got != "1개월" {
		t.Fatalf("formatPeriodLabel(30) = %q", got)
	}
	if got := formatPeriodLabel(90); got != "3개월" {
		t.Fatalf("formatPeriodLabel(90) = %q", got)
	}
	if got := formatPeriodLabel(180); got != "6개월" {
		t.Fatalf("formatPeriodLabel(180) = %q", got)
	}
	if got := formatPeriodLabel(181); got != "1년+" {
		t.Fatalf("formatPeriodLabel(181) = %q", got)
	}

	if !shouldShowChart([]int{100, 100}, 90, 0) {
		t.Fatal("expected lowest reference delta to render chart")
	}
	if !shouldShowChart([]int{100, 100}, 0, 110) {
		t.Fatal("expected highest reference delta to render chart")
	}
	if shouldShowChart([]int{100, 100}, 100, 100) {
		t.Fatal("expected flat prices without reference delta to skip chart")
	}

	productURL := formatProductURL(coupang.CoupangProductRecord{
		ProductID:    "123",
		ItemID:       "456",
		VendorItemID: "789",
	})
	if productURL != "coupang.com/vp/products/123?itemId=456&vendorItemId=789" {
		t.Fatalf("formatProductURL = %q", productURL)
	}
}

func replyIfCommandHelperText(t *testing.T) string {
	t.Helper()

	handler := NewCoupangHandler(&stubCoupangLookupService{}, nil)
	var reply bot.Reply
	if err := handler.replyIfCommand(context.Background(), bot.CommandContext{
		Command: "쿠팡",
		Reply: func(_ context.Context, r bot.Reply) error {
			reply = r
			return nil
		},
	}, "명령 응답"); err != nil {
		t.Fatalf("replyIfCommand command: %v", err)
	}
	if err := handler.replyIfCommand(context.Background(), bot.CommandContext{}, "무시"); err != nil {
		t.Fatalf("replyIfCommand fallback: %v", err)
	}
	return reply.Text
}
