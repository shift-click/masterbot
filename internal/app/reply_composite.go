package app

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

type compositeObserveInput struct {
	CorrelationID string
	Part          string
	ReplyType     string
	ExpectedParts int
	FinalPart     bool
	Success       bool
	Metadata      map[string]any
}

type compositeObserveOutput struct {
	ErrorClass string
	Metadata   map[string]any
}

type replyCompositeTracker struct {
	mu     sync.Mutex
	states map[string]*replyCompositeState
}

type replyCompositeState struct {
	expected int
	parts    map[string]bool
	metadata map[string]any
	lastSeen time.Time
}

type compositeDeliverySummary struct {
	successCount    int
	failureCount    int
	deliveryOutcome string
	errorClass      string
}

func newReplyCompositeTracker() *replyCompositeTracker {
	return &replyCompositeTracker{
		states: make(map[string]*replyCompositeState),
	}
}

func (t *replyCompositeTracker) Observe(input compositeObserveInput) (compositeObserveOutput, bool) {
	if t == nil {
		return compositeObserveOutput{}, false
	}

	now := time.Now()
	normalized := normalizeCompositeObserveInput(input, now)

	t.mu.Lock()
	defer t.mu.Unlock()
	t.prune(now)

	state := t.loadCompositeState(normalized.CorrelationID, normalized.ExpectedParts, now)
	updateCompositeState(state, normalized, now)
	if !shouldFinalizeComposite(normalized, state) {
		return compositeObserveOutput{}, false
	}

	summary := summarizeCompositeState(state)
	out := finalizeCompositeState(normalized.CorrelationID, state, summary)
	delete(t.states, normalized.CorrelationID)
	return out, true
}

func (t *replyCompositeTracker) prune(now time.Time) {
	if len(t.states) == 0 {
		return
	}
	cutoff := now.Add(-2 * time.Minute)
	for correlationID, state := range t.states {
		if state.lastSeen.Before(cutoff) {
			delete(t.states, correlationID)
		}
	}
}

func normalizeCompositeObserveInput(input compositeObserveInput, now time.Time) compositeObserveInput {
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	if input.CorrelationID == "" {
		input.CorrelationID = fmt.Sprintf("reply:%d", now.UnixNano())
	}
	input.Part = strings.TrimSpace(input.Part)
	if input.Part == "" {
		input.Part = "unknown"
	}
	if input.ExpectedParts <= 0 {
		input.ExpectedParts = 1
	}
	return input
}

func (t *replyCompositeTracker) loadCompositeState(correlationID string, expected int, now time.Time) *replyCompositeState {
	state := t.states[correlationID]
	if state == nil {
		state = &replyCompositeState{
			expected: expected,
			parts:    make(map[string]bool),
			metadata: make(map[string]any),
			lastSeen: now,
		}
		t.states[correlationID] = state
	}
	if expected > state.expected {
		state.expected = expected
	}
	return state
}

func updateCompositeState(state *replyCompositeState, input compositeObserveInput, now time.Time) {
	state.parts[input.Part] = input.Success
	state.lastSeen = now
	mergeCompositeMetadata(state.metadata, input.Metadata)
}

func shouldFinalizeComposite(input compositeObserveInput, state *replyCompositeState) bool {
	return input.FinalPart || len(state.parts) >= state.expected
}

func summarizeCompositeState(state *replyCompositeState) compositeDeliverySummary {
	summary := compositeDeliverySummary{deliveryOutcome: "failed"}
	for _, success := range state.parts {
		if success {
			summary.successCount++
			continue
		}
		summary.failureCount++
	}
	switch {
	case summary.successCount > 0 && summary.failureCount == 0:
		summary.deliveryOutcome = "full_success"
	case summary.successCount > 0 && summary.failureCount > 0:
		summary.deliveryOutcome = "partial_success"
	}
	if summary.deliveryOutcome != "full_success" {
		summary.errorClass = "reply_composite_" + summary.deliveryOutcome
	}
	return summary
}

func finalizeCompositeState(
	correlationID string,
	state *replyCompositeState,
	summary compositeDeliverySummary,
) compositeObserveOutput {
	out := cloneMetadata(state.metadata)
	out["request_correlation_id"] = correlationID
	out["composite_delivery_outcome"] = summary.deliveryOutcome
	out["composite_part_count"] = len(state.parts)
	out["composite_success_parts"] = summary.successCount
	out["composite_failed_parts"] = summary.failureCount
	out["composite_expected"] = state.expected
	return compositeObserveOutput{
		ErrorClass: summary.errorClass,
		Metadata:   out,
	}
}

func normalizeReplyMetadata(in map[string]any) map[string]any {
	if len(in) == 0 {
		return make(map[string]any)
	}
	return cloneMetadata(in)
}

func cloneMetadata(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func mergeCompositeMetadata(dst, src map[string]any) {
	for key, value := range src {
		if strings.TrimSpace(key) == "" {
			continue
		}
		dst[key] = value
	}
}

func metadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func metadataBool(metadata map[string]any, key string) bool {
	if len(metadata) == 0 {
		return false
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return false
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(v))
		return err == nil && parsed
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	default:
		return false
	}
}

func metadataInt(metadata map[string]any, key string) int {
	if len(metadata) == 0 {
		return 0
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return 0
	}
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}
