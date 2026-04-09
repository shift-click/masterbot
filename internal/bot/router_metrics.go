package bot

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shift-click/masterbot/internal/intent"
	"github.com/shift-click/masterbot/internal/metrics"
	"github.com/shift-click/masterbot/internal/transport"
)

func (r *Router) recordFallbackOutcome(
	ctx context.Context,
	msg transport.Message,
	fallbackID string,
	scope fallbackScope,
	start time.Time,
	err error,
) {
	commandSource := commandSourceFromScope(scope)
	r.recordCommandEvent(ctx, msg, metrics.EventCommandDispatched, routerEventOptions{
		commandID:     fallbackID,
		commandSource: commandSource,
	}, start)
	switch {
	case errors.Is(err, ErrHandled):
		success := true
		r.recordCommandEvent(ctx, msg, metrics.EventCommandSucceeded, routerEventOptions{
			commandID:     fallbackID,
			commandSource: commandSource,
			success:       &success,
			latency:       time.Since(start),
		})
	case errors.Is(err, ErrHandledWithFailure) || r.recorder != nil:
		success := false
		r.recordCommandEvent(ctx, msg, metrics.EventCommandFailed, routerEventOptions{
			commandID:     fallbackID,
			commandSource: commandSource,
			success:       &success,
			errorClass:    classifyError(err),
			latency:       time.Since(start),
		})
	}
}

func (r *Router) recordCommandEvent(
	ctx context.Context,
	msg transport.Message,
	eventName metrics.EventName,
	opts routerEventOptions,
	occurredAt ...time.Time,
) {
	eventTime := time.Now()
	if len(occurredAt) > 0 && !occurredAt[0].IsZero() {
		eventTime = occurredAt[0]
	}
	r.recordEvent(ctx, msg, newRouterMetricEvent(eventTime, eventName, opts))
}

func (r *Router) recordNonExecutionEvent(
	ctx context.Context,
	msg transport.Message,
	eventName metrics.EventName,
	opts routerEventOptions,
) {
	r.recordEvent(ctx, msg, newRouterMetricEvent(time.Now(), eventName, opts))
}

func newRouterMetricEvent(
	occurredAt time.Time,
	eventName metrics.EventName,
	opts routerEventOptions,
) metrics.Event {
	event := metrics.Event{
		OccurredAt:    occurredAt,
		EventName:     eventName,
		CommandID:     strings.TrimSpace(opts.commandID),
		CommandSource: opts.commandSource,
		Audience:      "customer",
		FeatureKey:    strings.TrimSpace(opts.featureKey),
		Attribution:   strings.TrimSpace(opts.attribution),
		Success:       opts.success,
		ErrorClass:    opts.errorClass,
		Latency:       opts.latency,
		Denied:        opts.denied,
		Metadata:      opts.metadata,
	}
	if event.FeatureKey == "" {
		event.FeatureKey = event.CommandID
	}
	if event.Attribution == "" && event.CommandSource != "" {
		event.Attribution = string(event.CommandSource)
	}
	return event
}

func fallbackHandlerID(catalog *intent.Catalog, handler FallbackHandler) string {
	if described, ok := handler.(DescribedHandler); ok {
		return described.Descriptor().ID
	}
	type named interface{ Name() string }
	if n, ok := handler.(named); ok {
		if id, normalized := catalog.Normalize(n.Name()); normalized {
			return id
		}
	}
	return ""
}

func fallbackCommandName(handler FallbackHandler, fallbackID string) string {
	if described, ok := handler.(DescribedHandler); ok {
		return described.Descriptor().Name
	}
	type named interface{ Name() string }
	if n, ok := handler.(named); ok {
		return n.Name()
	}
	return fallbackID
}

func (r *Router) recordUnmatched(ctx context.Context, msg transport.Message) {
	r.debugLogUnmatched(ctx, msg)
	r.recordNonExecutionEvent(ctx, msg, metrics.EventUnmatchedMessage, routerEventOptions{})
}

func (r *Router) recordPolicySkip(ctx context.Context, msg transport.Message, fallbacks []fallbackEntry, reason string) {
	r.recordNonExecutionEvent(ctx, msg, metrics.EventPolicySkip, routerEventOptions{
		commandSource: metrics.CommandSourceAuto,
		attribution:   string(metrics.CommandSourceAuto),
		metadata: map[string]any{
			"reason":        strings.TrimSpace(reason),
			"candidate_ids": fallbackIDs(fallbacks),
		},
	})
}

func (r *Router) recordEvent(ctx context.Context, msg transport.Message, event metrics.Event) {
	if r.recorder == nil {
		return
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now()
	}
	event.RequestID = msg.Raw.ID
	event.RawRoomID = msg.Raw.ChatID
	if strings.TrimSpace(event.RawTenantID) == "" {
		event.RawTenantID = msg.Raw.ChatID
	}
	if strings.TrimSpace(event.RawScopeRoomID) == "" {
		event.RawScopeRoomID = msg.Raw.ChatID
	}
	event.RoomName = msg.Room
	event.RawUserID = msg.Raw.UserID
	if strings.TrimSpace(event.Audience) == "" {
		event.Audience = "customer"
	}
	if strings.TrimSpace(event.FeatureKey) == "" {
		event.FeatureKey = strings.TrimSpace(event.CommandID)
	}
	r.recorder.Record(ctx, event)
}

func (r *Router) debugLogUnmatched(ctx context.Context, msg transport.Message) {
	if r.logger == nil || !r.logger.Enabled(ctx, slog.LevelDebug) {
		return
	}

	content := msg.Msg
	trimmed := strings.TrimSpace(content)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > 64 {
		return
	}

	r.logger.Debug(
		"router unmatched message",
		"msg", strconv.QuoteToASCII(content),
		"trimmed", strconv.QuoteToASCII(trimmed),
		"runes", utf8.RuneCountInString(content),
		"trimmed_runes", utf8.RuneCountInString(trimmed),
		"fields", len(strings.Fields(trimmed)),
		"explicit_bare_candidate", looksLikeExplicitBareQuery(trimmed, r.prefix),
		"local_auto_candidate", looksLikeLocalAutoCandidate(trimmed),
		"contains_http_url", containsHTTPURL(trimmed),
		"origin", msg.Origin.AdapterID,
		"chat_id", msg.Raw.ChatID,
		"user_id", msg.Raw.UserID,
	)
}
