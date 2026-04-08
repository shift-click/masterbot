package command

import (
	"context"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/pkg/formatter"
)

// ForexHandler handles multi-currency exchange rate lookups.
type ForexHandler struct {
	descriptorSupport
	forex *providers.DunamuForex
}

// NewForexHandler creates a new forex handler.
func NewForexHandler(forex *providers.DunamuForex) *ForexHandler {
	return &ForexHandler{
		descriptorSupport: newDescriptorSupport("finance"),
		forex:             forex,
	}
}

func (h *ForexHandler) Execute(ctx context.Context, cmd bot.CommandContext) error {
	rates := h.forex.Rates()
	if len(rates.Rates) == 0 {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "환율 데이터를 아직 가져오지 못했습니다.",
		})
	}

	text := formatter.FormatForexRates(rates.Rates, providers.ForexDisplayOrder())
	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
	})
}

func (h *ForexHandler) MatchBareQuery(_ context.Context, content string) ([]string, bool) {
	return nil, false
}
