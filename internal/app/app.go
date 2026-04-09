package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/buildinfo"
	"github.com/shift-click/masterbot/internal/commandmeta"
	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/intent"
	"github.com/shift-click/masterbot/internal/metrics"
	"github.com/shift-click/masterbot/internal/transport"
)

type App struct {
	cfg       config.Config
	logger    *slog.Logger
	router    *bot.Router
	adapter   transport.RuntimeAdapter
	lifecycle *Lifecycle
	closers   []func() error
	recorder  metrics.Recorder
	composite *replyCompositeTracker
}

func (a *App) Run(ctx context.Context) error {
	a.addTransportLifecycle()
	runCtx, cancel := context.WithCancel(ctx)
	run := a.lifecycle.Start(runCtx)
	a.logStartup()
	err := waitLifecycle(ctx, cancel, run)
	a.shutdown()
	return err
}

// TransportAddr returns the actual listen address of the transport adapter,
// if the adapter supports it (e.g., httptest.Server on ":0"). Returns "" otherwise.
func (a *App) TransportAddr() string {
	type addrProvider interface {
		Addr() string
	}
	if ap, ok := a.adapter.(addrProvider); ok {
		return ap.Addr()
	}
	return ""
}

func (a *App) addTransportLifecycle() {
	if a.adapter == nil {
		return
	}
	a.lifecycle.Add("transport-adapter", func(inner context.Context) error {
		return a.adapter.Start(inner, func(replyCtx context.Context, msg transport.Message) error {
			return a.router.Dispatch(replyCtx, msg, func(sendCtx context.Context, reply bot.Reply) error {
				start := time.Now()
				req := transport.ReplyRequest{
					Type:      reply.Type,
					Room:      msg.Raw.ChatID,
					Data:      replyData(reply),
					AdapterID: msg.Origin.AdapterID,
				}
				a.logger.Info("sending reply",
					"type", req.Type, "room", req.Room, "adapter_id", req.AdapterID,
					"sender", msg.Sender, "chat_id", msg.Raw.ChatID, "msg_id", msg.Raw.ID)
				err := a.adapter.Reply(sendCtx, req)
				a.recordReplyMetric(sendCtx, msg, reply, start, err)
				return err
			})
		})
	})
}

func (a *App) recordReplyMetric(ctx context.Context, msg transport.Message, reply bot.Reply, start time.Time, replyErr error) {
	if a.recorder == nil {
		return
	}
	metadata := normalizeReplyMetadata(reply.Metadata)
	correlationID := metadataString(metadata, "request_correlation_id")
	if correlationID == "" {
		correlationID = "reply:" + strings.TrimSpace(msg.Raw.ID)
	}
	part := metadataString(metadata, "reply_part")
	if part == "" {
		part = string(reply.Type)
	}
	expected := metadataInt(metadata, "composite_expected")
	if expected <= 0 {
		expected = 1
	}
	finalPart := metadataBool(metadata, "composite_final_part")
	if expected <= 1 {
		finalPart = true
	}

	eventName := metrics.EventReplySent
	errorClass := ""
	partOutcome := "sent"
	if replyErr != nil {
		eventName = metrics.EventReplyFailed
		errorClass = classifyReplyError(replyErr)
		partOutcome = "failed"
	}
	metadata["request_correlation_id"] = correlationID
	metadata["reply_part"] = part
	metadata["reply_part_outcome"] = partOutcome
	metadata["composite_expected"] = expected
	metadata["composite_final_part"] = finalPart

	a.recorder.Record(ctx, metrics.Event{
		OccurredAt:     time.Now(),
		RequestID:      msg.Raw.ID,
		EventName:      eventName,
		RawRoomID:      msg.Raw.ChatID,
		RawTenantID:    msg.Raw.ChatID,
		RawScopeRoomID: msg.Raw.ChatID,
		RoomName:       msg.Room,
		RawUserID:      msg.Raw.UserID,
		Audience:       "customer",
		FeatureKey:     "reply",
		Attribution:    "transport_reply",
		ReplyType:      string(reply.Type),
		Latency:        time.Since(start),
		ErrorClass:     errorClass,
		Metadata:       metadata,
	})

	if a.composite == nil {
		return
	}
	outcome, done := a.composite.Observe(compositeObserveInput{
		CorrelationID: correlationID,
		Part:          part,
		ReplyType:     string(reply.Type),
		ExpectedParts: expected,
		FinalPart:     finalPart,
		Success:       replyErr == nil,
		Metadata:      metadata,
	})
	if !done {
		return
	}
	featureKey := "reply"
	commandID := ""
	attribution := "transport_reply"
	if metadataString(outcome.Metadata, "chart_decision") != "" {
		featureKey = "coupang"
		commandID = "쿠팡"
		attribution = "coupang_composite_reply"
	}
	a.recorder.Record(ctx, metrics.Event{
		OccurredAt:     time.Now(),
		RequestID:      msg.Raw.ID,
		EventName:      metrics.EventReplyCompositeOutcome,
		RawRoomID:      msg.Raw.ChatID,
		RawTenantID:    msg.Raw.ChatID,
		RawScopeRoomID: msg.Raw.ChatID,
		RoomName:       msg.Room,
		RawUserID:      msg.Raw.UserID,
		CommandID:      commandID,
		Audience:       "customer",
		FeatureKey:     featureKey,
		Attribution:    attribution,
		ReplyType:      string(reply.Type),
		ErrorClass:     outcome.ErrorClass,
		Metadata:       outcome.Metadata,
	})
}

func (a *App) logStartup() {
	info := buildinfo.FromEnv()
	a.logger.Info(
		"jucobot started",
		"name", a.cfg.Bot.Name,
		"version", info.Version,
		"revision", info.Revision,
		"build_time", info.BuildTime,
		"default_policy", a.cfg.Access.DefaultPolicy,
		"prefix", a.cfg.Bot.CommandPrefix,
		"kakao_adapter_enabled", a.adapter != nil,
	)
}

func (a *App) shutdown() {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.Bot.ShutdownTimeout)
	defer cancel()
	if err := closeAll(shutdownCtx, a.closers); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		a.logger.Warn("close components", "error", err)
	}
	a.logger.Info("jucobot stopped")
}

func waitLifecycle(ctx context.Context, cancel context.CancelFunc, run *lifecycleRun) error {
	if run == nil {
		return nil
	}
	for {
		select {
		case err, ok := <-run.errCh:
			if !ok {
				cancel()
				return nil
			}
			if err != nil {
				cancel()
				<-run.doneCh
				if drained := firstLifecycleError(run.errCh); drained != nil {
					return drained
				}
				return err
			}
		case <-run.doneCh:
			cancel()
			return firstLifecycleError(run.errCh)
		case <-ctx.Done():
			cancel()
			<-run.doneCh
			return firstLifecycleError(run.errCh)
		}
	}
}

func firstLifecycleError(errCh <-chan error) error {
	var firstErr error
	for err := range errCh {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func closeAll(ctx context.Context, closers []func() error) error {
	if len(closers) == 0 {
		return nil
	}

	done := make(chan error, 1)
	go func() {
		var firstErr error
		for i := len(closers) - 1; i >= 0; i-- {
			closer := closers[i]
			if err := closer(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		done <- firstErr
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func replyData(reply bot.Reply) any {
	switch reply.Type {
	case transport.ReplyTypeText:
		return reply.Text
	case transport.ReplyTypeImage:
		return reply.ImageBase64
	case transport.ReplyTypeImageMultiple:
		return reply.Images
	default:
		return reply.Text
	}
}

func registerFeatureRuntimes(router *bot.Router, runtimes []featureRuntime) error {
	for _, runtime := range runtimes {
		for _, handler := range runtime.handlers {
			if err := registerHandler(router, handler); err != nil {
				return fmt.Errorf("register handler for feature %q: %w", runtime.name, err)
			}
		}
		for _, handler := range runtime.fallbacks {
			if err := registerFallbackOnly(router, handler); err != nil {
				return fmt.Errorf("register fallback for feature %q: %w", runtime.name, err)
			}
		}
	}
	return nil
}

func registerHandler(router *bot.Router, handler bot.Handler) error {
	if isNilHandler(handler) {
		return nil
	}
	if err := router.Register(handler); err != nil {
		return err
	}
	return registerFallbackByScope(router, handler)
}

func registerFallbackOnly(router *bot.Router, handler bot.FallbackHandler) error {
	if isNilFallbackHandler(handler) {
		return nil
	}
	return registerFallbackByScope(router, handler)
}

func registerFallbackByScope(router *bot.Router, handler any) error {
	fb, ok := handler.(bot.FallbackHandler)
	if !ok {
		return nil
	}

	entry, ok, err := handlerCatalogEntry(router, handler)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("fallback handler %T is not present in intent catalog", handler)
	}

	switch entry.FallbackScope {
	case commandmeta.FallbackScopeDeterministic:
		return router.AddDeterministicFallback(fb)
	case commandmeta.FallbackScopeAuto:
		return router.AddAutoFallback(fb)
	case "":
		return fmt.Errorf("fallback handler %T intent %q is missing fallback scope metadata", handler, entry.ID)
	default:
		return fmt.Errorf("fallback handler %T intent %q has unsupported fallback scope %q", handler, entry.ID, entry.FallbackScope)
	}
}

func handlerCatalogEntry(router *bot.Router, handler any) (intent.Entry, bool, error) {
	if described, ok := handler.(interface{ Descriptor() commandmeta.Descriptor }); ok {
		descriptor := described.Descriptor()
		entry, found := router.Catalog().Entry(descriptor.ID)
		if !found {
			return intent.Entry{}, false, nil
		}
		if err := validateFallbackDescriptor(descriptor, entry); err != nil {
			return intent.Entry{}, false, err
		}
		return entry, true, nil
	}
	if named, ok := handler.(interface{ Name() string }); ok {
		if id, found := router.Catalog().Normalize(named.Name()); found {
			entry, entryFound := router.Catalog().Entry(id)
			return entry, entryFound, nil
		}
	}
	return intent.Entry{}, false, nil
}

func validateFallbackDescriptor(descriptor commandmeta.Descriptor, entry intent.Entry) error {
	switch {
	case descriptor.Name != entry.Name:
		return fmt.Errorf("fallback descriptor %q name %q != catalog %q", descriptor.ID, descriptor.Name, entry.Name)
	case descriptor.Description != entry.Description:
		return fmt.Errorf("fallback descriptor %q description %q != catalog %q", descriptor.ID, descriptor.Description, entry.Description)
	case !reflect.DeepEqual(normalizeMetadataValues(descriptor.SlashAliases), normalizeMetadataValues(entry.SlashAliases)):
		return fmt.Errorf("fallback descriptor %q slash aliases do not match catalog", descriptor.ID)
	case !reflect.DeepEqual(normalizeMetadataValues(descriptor.ExplicitAliases), normalizeMetadataValues(entry.ExplicitAliases)):
		return fmt.Errorf("fallback descriptor %q explicit aliases do not match catalog", descriptor.ID)
	case !reflect.DeepEqual(normalizeMetadataValues(descriptor.NormalizeKeys), normalizeMetadataValues(entry.NormalizeKeys)):
		return fmt.Errorf("fallback descriptor %q normalize keys do not match catalog", descriptor.ID)
	case descriptor.FallbackScope != entry.FallbackScope:
		return fmt.Errorf("fallback descriptor %q scope %q != catalog %q", descriptor.ID, descriptor.FallbackScope, entry.FallbackScope)
	case descriptor.AllowAutoQuery != entry.AllowAutoQuery:
		return fmt.Errorf("fallback descriptor %q allow_auto_query does not match catalog", descriptor.ID)
	}
	return nil
}

func isNilHandler(handler bot.Handler) bool {
	if handler == nil {
		return true
	}
	v := reflect.ValueOf(handler)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func isNilFallbackHandler(handler bot.FallbackHandler) bool {
	if handler == nil {
		return true
	}
	v := reflect.ValueOf(handler)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func normalizeMetadataValues(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	sort.Strings(normalized)
	return normalized
}

func classifyReplyError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return "reply_error"
}

func ratio(numerator, denominator int64) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}
