package bot

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/shift-click/masterbot/internal/intent"
	"github.com/shift-click/masterbot/internal/metrics"
	"github.com/shift-click/masterbot/internal/transport"
)

func (r *Router) parseSlash(content string) (intent.Entry, Handler, []string, bool) {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, r.prefix) {
		return intent.Entry{}, nil, nil, false
	}
	fields := strings.Fields(strings.TrimPrefix(content, r.prefix))
	if len(fields) == 0 {
		return intent.Entry{}, nil, nil, false
	}
	entry, handler, ok := r.registry.ResolveSlash(fields[0])
	if !ok {
		return intent.Entry{}, nil, nil, false
	}
	return entry, handler, fields[1:], true
}

func isSlashCommand(content, prefix string) bool {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, prefix) {
		return false
	}
	return len(strings.Fields(strings.TrimPrefix(content, prefix))) > 0
}

func (r *Router) parseExplicit(content string) (intent.Entry, Handler, []string, bool) {
	content = strings.TrimSpace(content)
	if content == "" || strings.HasPrefix(content, r.prefix) {
		return intent.Entry{}, nil, nil, false
	}

	fields := strings.Fields(content)
	if len(fields) == 0 {
		return intent.Entry{}, nil, nil, false
	}

	entry, ok := r.registry.Catalog().Resolve(fields[0])
	if !ok {
		return intent.Entry{}, nil, nil, false
	}
	if shouldSkipPrefixlessExplicit(entry.ID) {
		return intent.Entry{}, nil, nil, false
	}

	handler, ok := r.registry.Handler(entry.ID)
	if !ok {
		return intent.Entry{}, nil, nil, false
	}

	return entry, handler, fields[1:], true
}

func shouldSkipPrefixlessExplicit(intentID string) bool {
	switch intentID {
	case "chart":
		// "차트 ..." is frequently used as ordinary prose, so treat it as an
		// implicit candidate only. Slash commands remain available via "/차트".
		return true
	default:
		return false
	}
}

func (r *Router) dispatchHandler(
	ctx context.Context,
	msg transport.Message,
	reply ReplyFunc,
	entry intent.Entry,
	handler Handler,
	args []string,
	commandSource metrics.CommandSource,
) error {
	if handler == nil {
		return reply(ctx, Reply{
			Type: transport.ReplyTypeText,
			Text: unknownCommandReplyText,
		})
	}
	if commandSource == metrics.CommandSourceSlash && !supportsSlashCommands(handler) {
		return reply(ctx, Reply{
			Type: transport.ReplyTypeText,
			Text: unknownCommandReplyText,
		})
	}

	if r.access != nil && !r.access.CanExecute(msg, entry.ID) {
		r.recordNonExecutionEvent(ctx, msg, metrics.EventAccessDenied, routerEventOptions{
			commandID:     entry.ID,
			commandSource: commandSource,
			denied:        true,
		})
		return reply(ctx, Reply{
			Type: transport.ReplyTypeText,
			Text: DeniedIntentMessage,
		})
	}

	commandCtx := CommandContext{
		Message: msg,
		Command: entry.Name,
		Source:  string(commandSource),
		Args:    args,
		Reply:   reply,
		Now:     time.Now,
	}

	r.recordCommandEvent(ctx, msg, metrics.EventCommandDispatched, routerEventOptions{
		commandID:     entry.ID,
		commandSource: commandSource,
	})
	r.recordCommandEvent(ctx, msg, metrics.EventActivation, routerEventOptions{
		commandID:     entry.ID,
		commandSource: commandSource,
	})

	executor := r.chain(handler)
	return executor(ctx, commandCtx)
}

func (r *Router) dispatchFallbacks(
	ctx context.Context,
	msg transport.Message,
	reply ReplyFunc,
	fallbacks []fallbackEntry,
	scope fallbackScope,
	autoPolicy AutoQueryPolicy,
	policy *FallbackPolicy,
) (fallbackDispatchResult, error) {
	result := fallbackDispatchResult{}
	for _, fb := range fallbacks {
		if !autoPolicy.AllowsHandler(fb.id) {
			continue
		}
		if !fb.aclExempt && !policy.AllowDeterministic(msg, fb.id) {
			result.denied = true
			r.recordNonExecutionEvent(ctx, msg, metrics.EventAccessDenied, routerEventOptions{
				commandID:     fb.id,
				commandSource: commandSourceFromScope(scope),
				denied:        true,
			})
			continue
		}

		handled, err := r.executeFallbackEntry(ctx, msg, reply, fb, scope)
		if err != nil {
			return result, err
		}
		if handled {
			return result, nil
		}
	}
	return result, nil
}

func (r *Router) executeFallbackEntry(
	ctx context.Context,
	msg transport.Message,
	reply ReplyFunc,
	fb fallbackEntry,
	scope fallbackScope,
) (bool, error) {
	fbCtx := CommandContext{
		Message: msg,
		Command: fallbackCommandName(fb.handler, fb.id),
		Source:  string(commandSourceFromScope(scope)),
		Reply:   reply,
		Now:     time.Now,
	}

	start := time.Now()
	if err := fb.handler.HandleFallback(ctx, fbCtx); err != nil {
		r.recordFallbackOutcome(ctx, msg, fb.id, scope, start, err)
		return true, err
	}
	return false, nil
}

func autoQueryPolicy(ctx context.Context, manager *AutoQueryManager, msg transport.Message) (AutoQueryPolicy, error) {
	if manager == nil {
		return DefaultAutoQueryPolicy(intent.DefaultCatalog()), nil
	}
	return manager.Policy(ctx, msg)
}

func filterFallbacks(entries []fallbackEntry, scope fallbackScope) []fallbackEntry {
	filtered := make([]fallbackEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.scope == scope {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func fallbackIDs(entries []fallbackEntry) []string {
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.id)
	}
	return ids
}

func commandSourceFromScope(scope fallbackScope) metrics.CommandSource {
	switch scope {
	case fallbackScopeDeterministic:
		return metrics.CommandSourceDeterministic
	default:
		return metrics.CommandSourceAuto
	}
}

func classifyError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if class, ok := handledFailureClass(err); ok && class != "" {
		return class
	}
	if errors.Is(err, ErrHandledWithFailure) {
		return "fetch_error"
	}
	return "handler_error"
}

func supportsSlashCommands(handler Handler) bool {
	mode, ok := handler.(SlashCommandMode)
	if !ok {
		return true
	}
	return mode.SupportsSlashCommands()
}
