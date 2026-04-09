package command

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/pkg/formatter"
)

// CoinHandler handles coin quote lookups.
// Works as a bare-query handler (비트) and as a local-auto fallback handler.
type CoinHandler struct {
	descriptorSupport
	resolver    *providers.CoinResolver
	cache       *scraper.CoinCache
	coinGecko   *providers.CoinGecko
	dexScreener *providers.DexScreener
	dexHotList  *scraper.CoinHotList
	logger      *slog.Logger
}

const coinNotFoundText = "해당 코인을 찾을 수 없습니다."

// NewCoinHandler creates a new coin handler.
func NewCoinHandler(
	resolver *providers.CoinResolver,
	cache *scraper.CoinCache,
	coinGecko *providers.CoinGecko,
	dexScreener *providers.DexScreener,
	dexHotList *scraper.CoinHotList,
	logger *slog.Logger,
) *CoinHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &CoinHandler{
		descriptorSupport: newDescriptorSupport("coin"),
		resolver:          resolver,
		cache:             cache,
		coinGecko:         coinGecko,
		dexScreener:       dexScreener,
		dexHotList:        dexHotList,
		logger:            logger.With("component", "coin_handler"),
	}
}

func (h *CoinHandler) SupportsSlashCommands() bool { return false }

func (h *CoinHandler) MatchAutoQueryCandidate(ctx context.Context, content string) bool {
	query, ok := extractAutoCandidate(content, 3)
	if !ok || len([]rune(query)) > 20 {
		return false
	}
	_, matched := h.resolver.ResolveLocalOnly(query)
	return matched
}

func (h *CoinHandler) Execute(ctx context.Context, cmd bot.CommandContext) error {
	if len(cmd.Args) == 0 {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "코인명을 입력해주세요. (예: 비트코인)",
		})
	}

	query := strings.Join(cmd.Args, " ")

	// Check for quantity multiplier: "솔 * 2"
	baseQuery, qty, hasQty := parseQuantifiedQuery(query)
	if hasQty && qty != 1 {
		return h.executeQuantity(ctx, cmd, baseQuery, qty)
	}

	text, err := h.lookup(ctx, query, false)
	if err != nil {
		if errors.Is(err, providers.ErrCoinQuoteUnavailable) {
			return cmd.Reply(ctx, bot.Reply{
				Type: transport.ReplyTypeText,
				Text: formatter.Prefix("⚠️", "현재 공급자에서 지원하지 않는 코인입니다."),
			})
		}
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: formatter.Error(err),
		})
	}
	if text == "" {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: coinNotFoundText,
		})
	}

	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
	})
}

func (h *CoinHandler) MatchBareQuery(ctx context.Context, content string) ([]string, bool) {
	query := strings.TrimSpace(content)
	if query == "" || len([]rune(query)) > 48 {
		return nil, false
	}

	// Try quantity pattern first: "솔 * 2", "솔*2"
	baseQuery, _, qOk := parseQuantifiedQuery(query)
	if qOk {
		baseQuery = strings.TrimSpace(baseQuery)
	} else {
		baseQuery = query
	}

	// Reject multi-word non-quantity queries.
	if !qOk && strings.Contains(query, " ") {
		return nil, false
	}

	// Bare query uses local alias only to avoid false positives.
	if _, ok := h.resolver.ResolveLocalOnly(baseQuery); !ok {
		return nil, false
	}
	return []string{query}, true
}

// HandleFallback handles local-auto coin candidates such as "비트" or "오늘 비트".
func (h *CoinHandler) HandleFallback(ctx context.Context, cmd bot.CommandContext) error {
	query, ok := extractAutoCandidate(cmd.Message.Msg, 3)
	if !ok || len([]rune(query)) > 20 {
		return nil
	}

	text, err := h.lookup(ctx, query, true)
	if err != nil {
		if replyErr := cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: formatter.Error(err),
		}); replyErr != nil {
			return replyErr
		}
		return bot.ErrHandledWithFailure
	}
	if text == "" {
		// Not a coin — return nil to let next fallback handler try.
		return nil
	}

	if err := cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
	}); err != nil {
		return err
	}
	return bot.ErrHandled
}
