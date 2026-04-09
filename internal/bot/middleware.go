package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/shift-click/masterbot/internal/metrics"
	"github.com/shift-click/masterbot/internal/transport"
	"golang.org/x/time/rate"
)

// AlertNotifier is the interface for inline error alerting.
type AlertNotifier interface {
	Notify(command, errorClass, errorMsg string)
}

type loggingMiddleware struct {
	logger   *slog.Logger
	recorder metrics.Recorder
	notifier AlertNotifier
}

type rateLimitMiddleware struct {
	limit    int
	window   time.Duration
	limiters sync.Map // map[string]*userLimiter
	recorder metrics.Recorder
}

type userLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type errorMiddleware struct {
	logger *slog.Logger
}

type commandEventOptions struct {
	success     *bool
	errorClass  string
	latency     time.Duration
	rateLimited bool
}

func NewLoggingMiddleware(logger *slog.Logger, recorder metrics.Recorder) Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return loggingMiddleware{logger: logger.With("middleware", "logging"), recorder: recorder}
}

// NewLoggingMiddlewareWithNotifier creates a logging middleware with inline alert notifier.
func NewLoggingMiddlewareWithNotifier(logger *slog.Logger, recorder metrics.Recorder, notifier AlertNotifier) Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return loggingMiddleware{logger: logger.With("middleware", "logging"), recorder: recorder, notifier: notifier}
}

// RateLimiter exposes lifecycle control for the rate limit middleware.
type RateLimiter interface {
	Middleware
	// StartCleanup launches a background goroutine that periodically evicts
	// idle per-user limiters. It exits when ctx is cancelled.
	StartCleanup(ctx context.Context)
}

func NewRateLimitMiddleware(limit int, window time.Duration, recorder metrics.Recorder) RateLimiter {
	if limit <= 0 {
		limit = 3
	}
	if window <= 0 {
		window = time.Second
	}
	return &rateLimitMiddleware{
		limit:    limit,
		window:   window,
		recorder: recorder,
	}
}

func NewErrorMiddleware(logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return errorMiddleware{logger: logger.With("middleware", "error")}
}

func (m loggingMiddleware) Wrap(next HandlerFunc) HandlerFunc {
	return func(ctx context.Context, cmd CommandContext) error {
		start := time.Now()
		err := next(ctx, cmd)
		elapsed := time.Since(start)
		if m.recorder != nil {
			errorClass := m.recordCommandOutcome(ctx, cmd, err, elapsed)
			m.recordCommandFollowup(ctx, cmd, err, errorClass)
			if err != nil && m.notifier != nil && handledFailureShouldAlert(err) {
				m.notifier.Notify(cmd.Command, errorClass, err.Error())
			}
		}
		m.logger.Info(
			"command executed",
			"command", cmd.Command,
			"room", cmd.Message.Room,
			"sender", cmd.Message.Sender,
			"elapsed", elapsed,
			"error", err,
		)
		return err
	}
}

func (m loggingMiddleware) recordCommandOutcome(ctx context.Context, cmd CommandContext, err error, elapsed time.Duration) string {
	eventName := metrics.EventCommandSucceeded
	success := true
	errorClass := ""
	if err != nil {
		eventName = metrics.EventCommandFailed
		success = false
		errorClass = classifyError(err)
	}
	m.recorder.Record(ctx, newCommandMetricEvent(time.Now(), cmd, eventName, commandEventOptions{
		success:    &success,
		errorClass: errorClass,
		latency:    elapsed,
	}))
	return errorClass
}

func (m loggingMiddleware) recordCommandFollowup(ctx context.Context, cmd CommandContext, err error, errorClass string) {
	if err != nil {
		m.recordCommandEvent(ctx, cmd, metrics.EventChurnSignal, errorClass)
		return
	}
	m.recordCommandEvent(ctx, cmd, metrics.EventEngagement, "")
	m.recordCommandEvent(ctx, cmd, metrics.EventConversion, "")
	if cmd.Source == string(metrics.CommandSourceAuto) || cmd.Source == string(metrics.CommandSourceDeterministic) {
		m.recordCommandEvent(ctx, cmd, metrics.EventRetentionReturn, "")
	}
}

func (m loggingMiddleware) recordCommandEvent(ctx context.Context, cmd CommandContext, eventName metrics.EventName, errorClass string) {
	m.recorder.Record(ctx, newCommandMetricEvent(time.Now(), cmd, eventName, commandEventOptions{
		errorClass: errorClass,
	}))
}

func (m *rateLimitMiddleware) Wrap(next HandlerFunc) HandlerFunc {
	return func(ctx context.Context, cmd CommandContext) error {
		key := fmt.Sprintf("%s:%s", cmd.Message.Raw.UserID, cmd.Command)

		ul, _ := m.limiters.LoadOrStore(key, &userLimiter{
			limiter:  rate.NewLimiter(rate.Every(m.window/time.Duration(m.limit)), m.limit),
			lastSeen: time.Now(),
		})
		entry := ul.(*userLimiter)
		entry.lastSeen = time.Now()

		if !entry.limiter.Allow() {
			if m.recorder != nil {
				m.recorder.Record(ctx, newCommandMetricEvent(cmd.Now(), cmd, metrics.EventRateLimited, commandEventOptions{
					rateLimited: true,
				}))
			}
			return cmd.Reply(ctx, Reply{
				Type: transport.ReplyTypeText,
				Text: "잠시 후 다시 시도해주세요.",
			})
		}

		return next(ctx, cmd)
	}
}

func (m *rateLimitMiddleware) StartCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				now := time.Now()
				m.limiters.Range(func(key, value any) bool {
					entry := value.(*userLimiter)
					if now.Sub(entry.lastSeen) > 1*time.Hour {
						m.limiters.Delete(key)
					}
					return true
				})
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (m errorMiddleware) Wrap(next HandlerFunc) HandlerFunc {
	return func(ctx context.Context, cmd CommandContext) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				m.logger.Error("panic recovered", "command", cmd.Command, "panic", recovered)
				err = cmd.Reply(ctx, Reply{
					Type: transport.ReplyTypeText,
					Text: "요청을 처리하는 중 문제가 발생했습니다.",
				})
			}
		}()

		if err := next(ctx, cmd); err != nil {
			// ErrHandledWithFailure: handler already replied to user.
			// Pass through so loggingMiddleware records it as failure.
			if errors.Is(err, ErrHandledWithFailure) {
				return err
			}
			m.logger.Error("handler returned error", "command", cmd.Command, "error", err)
			return cmd.Reply(ctx, Reply{
				Type: transport.ReplyTypeText,
				Text: "요청을 처리하는 중 문제가 발생했습니다.",
			})
		}

		return nil
	}
}

// pruneWindow removes timestamps older than the given window from a sorted
// hit slice.  It is used by AutoQueryManager for room-level budget tracking.
func pruneWindow(hits []time.Time, now time.Time, window time.Duration) []time.Time {
	cutoff := now.Add(-window)
	filtered := hits[:0]
	for _, hit := range hits {
		if hit.After(cutoff) {
			filtered = append(filtered, hit)
		}
	}
	return filtered
}

func newCommandMetricEvent(
	occurredAt time.Time,
	cmd CommandContext,
	eventName metrics.EventName,
	opts commandEventOptions,
) metrics.Event {
	return metrics.Event{
		OccurredAt:     occurredAt,
		RequestID:      cmd.Message.Raw.ID,
		EventName:      eventName,
		RawRoomID:      cmd.Message.Raw.ChatID,
		RawTenantID:    cmd.Message.Raw.ChatID,
		RawScopeRoomID: cmd.Message.Raw.ChatID,
		RoomName:       cmd.Message.Room,
		RawUserID:      cmd.Message.Raw.UserID,
		CommandID:      cmd.Command,
		CommandSource:  metrics.CommandSource(cmd.Source),
		Audience:       "customer",
		FeatureKey:     cmd.Command,
		Attribution:    cmd.Source,
		Success:        opts.success,
		ErrorClass:     opts.errorClass,
		Latency:        opts.latency,
		RateLimited:    opts.rateLimited,
	}
}
