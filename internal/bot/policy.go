package bot

import (
	"context"
	"errors"

	"github.com/shift-click/masterbot/internal/transport"
)

type FallbackPolicy struct {
	access      *AccessController
	autoQueries *AutoQueryManager
}

func NewFallbackPolicy(access *AccessController, autoQueries *AutoQueryManager) *FallbackPolicy {
	return &FallbackPolicy{
		access:      access,
		autoQueries: autoQueries,
	}
}

func (p *FallbackPolicy) Policy(ctx context.Context, msg transport.Message) (AutoQueryPolicy, error) {
	return autoQueryPolicy(ctx, p.autoQueries, msg)
}

func (p *FallbackPolicy) AllowDeterministic(msg transport.Message, handlerID string) bool {
	return p.access == nil || p.access.CanExecute(msg, handlerID)
}

func (p *FallbackPolicy) AllowAutomatic(
	ctx context.Context,
	msg transport.Message,
	query string,
	handlerIDs []string,
) (bool, error) {
	if p.autoQueries == nil {
		return false, nil
	}
	return p.autoQueries.AllowAutomatic(ctx, msg, query, handlerIDs)
}

func (p *FallbackPolicy) EvaluateAutomatic(
	ctx context.Context,
	msg transport.Message,
	query string,
	handlerIDs []string,
) (AutoQueryDecision, error) {
	if p.autoQueries == nil {
		return AutoQueryDecision{Reason: "manager-disabled"}, nil
	}
	return p.autoQueries.EvaluateAutomatic(ctx, msg, query, handlerIDs)
}

func swallowHandled(err error) error {
	if errors.Is(err, ErrHandled) {
		return nil
	}
	return err
}
