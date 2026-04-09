package bot

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/shift-click/masterbot/internal/metrics"
	"github.com/shift-click/masterbot/internal/transport"
)

func (r *Router) Dispatch(ctx context.Context, msg transport.Message, reply ReplyFunc) error {
	r.recordEvent(ctx, msg, metrics.Event{
		OccurredAt: time.Now(),
		EventName:  metrics.EventMessageReceived,
	})

	plan := r.buildDispatchPlan(msg)
	switch plan.kind {
	case dispatchPlanIgnore:
		if plan.recordUnmatched {
			r.recordUnmatched(ctx, msg)
		}
		return nil
	case dispatchPlanExplicit:
		return r.dispatchHandler(ctx, msg, reply, plan.entry, plan.handler, plan.args, metrics.CommandSourceExplicit)
	default:
		return r.dispatchPrefixless(ctx, msg, reply)
	}
}

func (r *Router) buildDispatchPlan(msg transport.Message) dispatchPlan {
	switch {
	case isSlashCommand(msg.Msg, r.prefix):
		return dispatchPlan{kind: dispatchPlanIgnore, recordUnmatched: true}
	case isJamoOnly(msg.Msg):
		return dispatchPlan{kind: dispatchPlanIgnore, recordUnmatched: true}
	}
	if entry, handler, args, ok := r.parseExplicit(msg.Msg); ok {
		return dispatchPlan{
			kind:    dispatchPlanExplicit,
			entry:   entry,
			handler: handler,
			args:    args,
		}
	}
	return dispatchPlan{kind: dispatchPlanPrefixless}
}

func (r *Router) dispatchPrefixless(ctx context.Context, msg transport.Message, reply ReplyFunc) error {
	if handled, err := r.dispatchThemeQuery(ctx, msg, reply); err != nil {
		return err
	} else if handled {
		return nil
	}

	if handled, err := r.dispatchBareQuery(ctx, msg, reply); err != nil {
		return err
	} else if handled {
		return nil
	}

	fallbacks := r.Registry().Fallbacks()
	policy := NewFallbackPolicy(r.access, r.autoQueries)

	handled, err := r.dispatchDeterministicFallbacks(ctx, msg, reply, filterFallbacks(fallbacks, fallbackScopeDeterministic), policy)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}

	autoFallbacks := filterFallbacks(fallbacks, fallbackScopeAuto)
	if len(autoFallbacks) == 0 {
		r.recordUnmatched(ctx, msg)
		return nil
	}

	autoPolicy, err := policy.Policy(ctx, msg)
	if err != nil {
		return err
	}
	return r.dispatchAutomaticFallbacks(ctx, msg, reply, autoFallbacks, autoPolicy, policy)
}

func (r *Router) dispatchDeterministicFallbacks(
	ctx context.Context,
	msg transport.Message,
	reply ReplyFunc,
	fallbacks []fallbackEntry,
	policy *FallbackPolicy,
) (bool, error) {
	if len(fallbacks) == 0 {
		return false, nil
	}
	result := fallbackDispatchResult{}
	for _, fb := range fallbacks {
		if !fb.aclExempt && !policy.AllowDeterministic(msg, fb.id) {
			result.denied = true
			r.recordNonExecutionEvent(ctx, msg, metrics.EventAccessDenied, routerEventOptions{
				commandID:     fb.id,
				commandSource: commandSourceFromScope(fallbackScopeDeterministic),
				denied:        true,
			})
			continue
		}
		handled, err := r.executeFallbackEntry(ctx, msg, reply, fb, fallbackScopeDeterministic)
		if err != nil {
			return true, swallowHandled(err)
		}
		if handled {
			return true, nil
		}
	}
	return result.denied, nil
}

func (r *Router) dispatchAutomaticFallbacks(
	ctx context.Context,
	msg transport.Message,
	reply ReplyFunc,
	autoFallbacks []fallbackEntry,
	autoPolicy AutoQueryPolicy,
	policy *FallbackPolicy,
) error {
	if !looksLikeLocalAutoCandidate(strings.TrimSpace(msg.Msg)) {
		r.recordUnmatched(ctx, msg)
		return nil
	}

	autoCandidates := r.matchAutoFallbackCandidates(ctx, autoFallbacks, msg.Msg)
	if len(autoCandidates) == 0 {
		r.recordUnmatched(ctx, msg)
		return nil
	}
	if autoPolicy.Mode != AutoQueryModeLocalAuto {
		r.recordPolicySkip(ctx, msg, autoCandidates, "room-policy")
		return nil
	}
	decision, err := policy.EvaluateAutomatic(ctx, msg, msg.Msg, fallbackIDs(autoCandidates))
	if err != nil {
		return err
	}
	if !decision.Allowed {
		r.recordPolicySkip(ctx, msg, autoCandidates, decision.Reason)
		return nil
	}
	autoResult, err := r.dispatchFallbacks(ctx, msg, reply, autoCandidates, fallbackScopeAuto, autoPolicy, policy)
	if err != nil {
		return swallowHandled(err)
	}
	if autoResult.denied {
		return nil
	}
	r.recordUnmatched(ctx, msg)
	return nil
}

func (r *Router) Handlers() []Handler {
	ids := r.IntentIDs()
	handlers := make([]Handler, 0, len(ids))
	for _, id := range ids {
		if handler, ok := r.Registry().Handler(id); ok {
			handlers = append(handlers, handler)
		}
	}
	sort.Slice(handlers, func(i, j int) bool {
		return handlers[i].Name() < handlers[j].Name()
	})
	return handlers
}

func (r *Router) chain(handler Handler) HandlerFunc {
	r.mu.RLock()
	middlewares := append([]Middleware(nil), r.middlewares...)
	r.mu.RUnlock()

	executor := HandlerFunc(handler.Execute)
	for i := len(middlewares) - 1; i >= 0; i-- {
		executor = middlewares[i].Wrap(executor)
	}
	return executor
}

func (r *Router) dispatchBareQuery(ctx context.Context, msg transport.Message, reply ReplyFunc) (bool, error) {
	content := strings.TrimSpace(msg.Msg)
	if !looksLikeExplicitBareQuery(content, r.prefix) {
		return false, nil
	}

	registry := r.Registry()
	for _, intentID := range registry.OrderedIntentIDs() {
		handler, ok := registry.Handler(intentID)
		if !ok {
			continue
		}
		matcher, ok := handler.(BareQueryMatcher)
		if !ok {
			continue
		}
		args, matched := matcher.MatchBareQuery(ctx, content)
		if !matched {
			continue
		}
		entry, ok := registry.Catalog().Entry(intentID)
		if !ok {
			continue
		}
		if err := r.dispatchHandler(ctx, msg, reply, entry, handler, args, metrics.CommandSourceExplicit); err != nil {
			return true, err
		}
		return true, nil
	}

	return false, nil
}

func (r *Router) dispatchThemeQuery(ctx context.Context, msg transport.Message, reply ReplyFunc) (bool, error) {
	content := strings.TrimSpace(msg.Msg)
	if !looksLikeThemeShapedBareQuery(content, r.prefix) {
		return false, nil
	}

	registry := r.Registry()
	entry, ok := registry.Catalog().Entry("stock")
	if !ok {
		return false, nil
	}
	handler, ok := registry.Handler(entry.ID)
	if !ok {
		return false, nil
	}

	if err := r.dispatchHandler(ctx, msg, reply, entry, handler, []string{content}, metrics.CommandSourceExplicit); err != nil {
		return true, err
	}
	return true, nil
}

func (r *Router) matchAutoFallbackCandidates(ctx context.Context, fallbacks []fallbackEntry, content string) []fallbackEntry {
	matches := make([]fallbackEntry, 0, len(fallbacks))
	for _, fb := range fallbacks {
		matcher, ok := fb.handler.(AutoQueryCandidateMatcher)
		if !ok || !matcher.MatchAutoQueryCandidate(ctx, content) {
			continue
		}
		matches = append(matches, fb)
	}
	return matches
}
