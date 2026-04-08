package app

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

type testStringer string

func (s testStringer) String() string { return string(s) }

func TestNormalizeCompositeObserveInputDefaults(t *testing.T) {
	t.Parallel()

	normalized := normalizeCompositeObserveInput(compositeObserveInput{}, time.Unix(10, 5))
	if !strings.HasPrefix(normalized.CorrelationID, "reply:") {
		t.Fatalf("correlation id = %q", normalized.CorrelationID)
	}
	if normalized.Part != "unknown" {
		t.Fatalf("part = %q", normalized.Part)
	}
	if normalized.ExpectedParts != 1 {
		t.Fatalf("expected parts = %d", normalized.ExpectedParts)
	}
}

func TestReplyCompositeTrackerFullSuccess(t *testing.T) {
	t.Parallel()

	tracker := newReplyCompositeTracker()
	if _, done := tracker.Observe(compositeObserveInput{
		CorrelationID: "c1",
		Part:          "image",
		ExpectedParts: 2,
		Success:       true,
		Metadata: map[string]any{
			"chart_decision_reason": "eligible",
		},
	}); done {
		t.Fatal("first part should not finalize")
	}

	out, done := tracker.Observe(compositeObserveInput{
		CorrelationID: "c1",
		Part:          "text",
		ExpectedParts: 2,
		FinalPart:     true,
		Success:       true,
	})
	if !done {
		t.Fatal("final part should finalize")
	}
	if got := metadataString(out.Metadata, "composite_delivery_outcome"); got != "full_success" {
		t.Fatalf("delivery outcome = %q, want full_success", got)
	}
	if out.ErrorClass != "" {
		t.Fatalf("unexpected error class: %s", out.ErrorClass)
	}
}

func TestReplyCompositeTrackerPartialSuccess(t *testing.T) {
	t.Parallel()

	tracker := newReplyCompositeTracker()
	_, _ = tracker.Observe(compositeObserveInput{
		CorrelationID: "c2",
		Part:          "image",
		ExpectedParts: 2,
		Success:       false,
	})
	out, done := tracker.Observe(compositeObserveInput{
		CorrelationID: "c2",
		Part:          "text",
		ExpectedParts: 2,
		FinalPart:     true,
		Success:       true,
	})
	if !done {
		t.Fatal("expected finalized partial result")
	}
	if got := metadataString(out.Metadata, "composite_delivery_outcome"); got != "partial_success" {
		t.Fatalf("delivery outcome = %q, want partial_success", got)
	}
	if out.ErrorClass != "reply_composite_partial_success" {
		t.Fatalf("error class = %q", out.ErrorClass)
	}
}

func TestReplyCompositeTrackerFailed(t *testing.T) {
	t.Parallel()

	tracker := newReplyCompositeTracker()
	out, done := tracker.Observe(compositeObserveInput{
		CorrelationID: "c3",
		Part:          "text",
		ExpectedParts: 1,
		FinalPart:     true,
		Success:       false,
	})
	if !done {
		t.Fatal("single failed part should finalize")
	}
	if got := metadataString(out.Metadata, "composite_delivery_outcome"); got != "failed" {
		t.Fatalf("delivery outcome = %q, want failed", got)
	}
	if out.ErrorClass != "reply_composite_failed" {
		t.Fatalf("error class = %q", out.ErrorClass)
	}
}

func TestReplyCompositeTrackerFinalizeOnExpectedPartCount(t *testing.T) {
	t.Parallel()

	tracker := newReplyCompositeTracker()
	if _, done := tracker.Observe(compositeObserveInput{
		CorrelationID: "c4",
		Part:          "image",
		ExpectedParts: 2,
		Success:       true,
	}); done {
		t.Fatal("first part should not finalize")
	}

	out, done := tracker.Observe(compositeObserveInput{
		CorrelationID: "c4",
		Part:          "text",
		ExpectedParts: 2,
		Success:       true,
		Metadata: map[string]any{
			"chart_decision": "rendered",
		},
	})
	if !done {
		t.Fatal("expected finalize once expected part count is met")
	}
	if got := metadataString(out.Metadata, "composite_delivery_outcome"); got != "full_success" {
		t.Fatalf("delivery outcome = %q, want full_success", got)
	}
	if got := metadataString(out.Metadata, "chart_decision"); got != "rendered" {
		t.Fatalf("merged metadata = %q", got)
	}
}

func TestReplyCompositeHelpers(t *testing.T) {
	t.Parallel()

	tracker := newReplyCompositeTracker()
	now := time.Unix(20, 0)
	state := tracker.loadCompositeState("c5", 2, now)
	updateCompositeState(state, compositeObserveInput{
		Part:    "image",
		Success: true,
		Metadata: map[string]any{
			"chart_final_mode": "text_only_skipped",
		},
	}, now)
	updateCompositeState(state, compositeObserveInput{
		Part:    "text",
		Success: false,
		Metadata: map[string]any{
			"chart_decision_reason": "unknown",
		},
	}, now)

	if !shouldFinalizeComposite(compositeObserveInput{FinalPart: true}, state) {
		t.Fatal("expected final part to force finalize")
	}

	summary := summarizeCompositeState(state)
	if summary.deliveryOutcome != "partial_success" {
		t.Fatalf("delivery outcome = %q", summary.deliveryOutcome)
	}
	if summary.errorClass != "reply_composite_partial_success" {
		t.Fatalf("error class = %q", summary.errorClass)
	}

	out := finalizeCompositeState("c5", state, summary)
	if got := metadataString(out.Metadata, "request_correlation_id"); got != "c5" {
		t.Fatalf("correlation id = %q", got)
	}
	if got := metadataInt(out.Metadata, "composite_failed_parts"); got != 1 {
		t.Fatalf("failed parts = %d", got)
	}
}

func TestReplyCompositeMetadataHelpers(t *testing.T) {
	t.Parallel()

	metadata := map[string]any{
		"string":   "  hello  ",
		"stringer": testStringer(" world "),
		"bool":     true,
		"boolText": " true ",
		"int":      3,
		"int64":    int64(4),
		"float":    float64(5),
		"textInt":  " 6 ",
		"badBool":  "nope",
		"badInt":   "oops",
		"other":    fmt.Sprintf("v-%d", 7),
	}

	if got := metadataString(nil, "missing"); got != "" {
		t.Fatalf("metadataString(nil) = %q", got)
	}
	if got := metadataString(metadata, "string"); got != "hello" {
		t.Fatalf("metadataString string = %q", got)
	}
	if got := metadataString(metadata, "stringer"); got != "world" {
		t.Fatalf("metadataString stringer = %q", got)
	}
	if got := metadataString(metadata, "other"); got != "v-7" {
		t.Fatalf("metadataString other = %q", got)
	}

	if metadataBool(nil, "missing") {
		t.Fatal("metadataBool(nil) should be false")
	}
	if !metadataBool(metadata, "bool") {
		t.Fatal("metadataBool(bool) should be true")
	}
	if !metadataBool(metadata, "boolText") {
		t.Fatal("metadataBool(string) should be true")
	}
	if !metadataBool(metadata, "int") || !metadataBool(metadata, "int64") || !metadataBool(metadata, "float") {
		t.Fatal("metadataBool numeric non-zero values should be true")
	}
	if metadataBool(metadata, "badBool") {
		t.Fatal("metadataBool invalid string should be false")
	}

	if got := metadataInt(nil, "missing"); got != 0 {
		t.Fatalf("metadataInt(nil) = %d", got)
	}
	if got := metadataInt(metadata, "int"); got != 3 {
		t.Fatalf("metadataInt(int) = %d", got)
	}
	if got := metadataInt(metadata, "int64"); got != 4 {
		t.Fatalf("metadataInt(int64) = %d", got)
	}
	if got := metadataInt(metadata, "float"); got != 5 {
		t.Fatalf("metadataInt(float64) = %d", got)
	}
	if got := metadataInt(metadata, "textInt"); got != 6 {
		t.Fatalf("metadataInt(string) = %d", got)
	}
	if got := metadataInt(metadata, "badInt"); got != 0 {
		t.Fatalf("metadataInt(bad string) = %d", got)
	}
}
