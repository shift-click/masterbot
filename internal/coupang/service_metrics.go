package coupang

import (
	"context"
	"strings"
	"time"

	"github.com/shift-click/masterbot/internal/metrics"
)

func (t *CoupangTracker) recordCoalescingSignal(ctx context.Context, trackID, operation string, leader bool, joinWait time.Duration, joinTimeout bool) {
	if t == nil || t.recorder == nil {
		return
	}
	metadata := map[string]any{
		"track_id":         trackID,
		"operation":        operation,
		"coalesced_hit":    !leader,
		"coalesced_leader": leader,
		"join_wait_ms":     joinWait.Milliseconds(),
		"join_timeout":     joinTimeout,
	}
	t.recorder.Record(ctx, metrics.Event{
		OccurredAt:    time.Now(),
		RequestID:     trackID,
		EventName:     metrics.EventCoupangLookupCoalesced,
		CommandID:     "쿠팡",
		CommandSource: metrics.CommandSourceSystem,
		Audience:      "operator",
		FeatureKey:    "coupang",
		Attribution:   operation,
		Metadata:      metadata,
	})
}

func registrationStageFromResolved(resolved *resolvedProduct) CoupangRegistrationStage {
	if resolved == nil {
		return CoupangRegistrationStageDeferred
	}
	if strings.HasPrefix(resolved.RefreshSource, "fallcent_direct_") {
		return CoupangRegistrationStageDirect
	}
	return CoupangRegistrationStageRescue
}

func (t *CoupangTracker) recordRegistrationPathEvent(ctx context.Context, trackID string, summary registrationSummary, attribution string) {
	if t == nil || t.recorder == nil || summary.Stage == CoupangRegistrationStageNone {
		return
	}
	t.recorder.Record(ctx, metrics.Event{
		OccurredAt:    time.Now(),
		RequestID:     trackID,
		EventName:     metrics.EventCoupangRegistrationPath,
		CommandID:     "쿠팡",
		CommandSource: metrics.CommandSourceSystem,
		Audience:      "operator",
		FeatureKey:    "coupang",
		Attribution:   attribution,
		Metadata: map[string]any{
			"track_id":              trackID,
			"registration_stage":    string(summary.Stage),
			"response_mode":         string(summary.ResponseMode),
			"budget_exhausted":      summary.BudgetExhausted,
			"seed_deferred":         summary.SeedDeferred,
			"rescue_deferred":       summary.RescueDeferred,
			"registration_deferred": summary.RegistrationDeferred,
			"latency_budget_ms":     t.registrationLatencyBudget().Milliseconds(),
		},
	})
}

func (t *CoupangTracker) recordRefreshSuccess(ctx context.Context, trackID, reason string, refreshed *resolvedProduct) {
	metadata := map[string]any{"reason": reason}
	if refreshed != nil {
		metadata["source"] = refreshed.RefreshSource
		metadata["price"] = refreshed.Price
	}
	t.recordRefreshEvent(ctx, metrics.EventCoupangRefreshSucceeded, trackID, reason, "", metadata)
}

func (t *CoupangTracker) recordRefreshFailure(ctx context.Context, trackID, reason string, err error) {
	metadata := map[string]any{"reason": reason}
	if err != nil {
		metadata["error"] = err.Error()
	}
	t.recordRefreshEvent(ctx, metrics.EventCoupangRefreshFailed, trackID, reason, "", metadata)
}

func (t *CoupangTracker) recordRefreshEvent(ctx context.Context, eventName metrics.EventName, trackID, reason, errorClass string, metadata map[string]any) {
	if t == nil || t.recorder == nil {
		return
	}
	if metadata == nil {
		metadata = make(map[string]any)
	}
	if _, exists := metadata["track_id"]; !exists {
		metadata["track_id"] = trackID
	}
	if _, exists := metadata["reason"]; !exists {
		metadata["reason"] = reason
	}
	t.recorder.Record(ctx, metrics.Event{
		OccurredAt:    time.Now(),
		RequestID:     trackID,
		EventName:     eventName,
		CommandID:     "쿠팡",
		CommandSource: metrics.CommandSourceSystem,
		Audience:      "operator",
		FeatureKey:    "coupang",
		Attribution:   reason,
		ErrorClass:    errorClass,
		Metadata:      metadata,
	})
}
