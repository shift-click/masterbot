package command

import (
	"context"
	"fmt"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/pkg/formatter"
)

func (h *CoinHandler) executeQuantity(ctx context.Context, cmd bot.CommandContext, query string, qty float64) error {
	result, ok := h.resolveQuantityTarget(ctx, query)
	if !ok {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: coinNotFoundText,
		})
	}

	quote, err := h.quantityQuoteFromResult(ctx, result)
	if err != nil {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: formatter.Error(err),
		})
	}
	if quote.name == "" {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: coinNotFoundText,
		})
	}

	text := formatter.FormatCoinQuantity(formatter.CoinQuantityData{
		Name:     quote.name,
		Symbol:   quote.symbol,
		Quantity: qty,
		USDTotal: quote.usdPrice * qty,
		KRWTotal: quote.krwPrice * qty,
	})
	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
	})
}

func (h *CoinHandler) resolveQuantityTarget(ctx context.Context, query string) (providers.CoinSearchResult, bool) {
	if result, ok := h.resolver.ResolveLocalOnly(query); ok {
		return result, true
	}
	result, ok := h.resolver.Resolve(ctx, query)
	return result, ok
}

type coinQuantityQuote struct {
	name     string
	symbol   string
	usdPrice float64
	krwPrice float64
}

func (h *CoinHandler) quantityQuoteFromResult(ctx context.Context, result providers.CoinSearchResult) (coinQuantityQuote, error) {
	switch h.selectQuoteVenue(result) {
	case providers.CoinQuoteVenueCEX:
		return h.quantityQuoteFromCEX(result), nil
	case providers.CoinQuoteVenueCoinGecko:
		return h.quantityQuoteFromCoinGecko(ctx, result)
	case providers.CoinQuoteVenueDEX:
		return h.quantityQuoteFromDEX(ctx, result)
	default:
		return coinQuantityQuote{}, nil
	}
}

func (h *CoinHandler) quantityQuoteFromCEX(result providers.CoinSearchResult) coinQuantityQuote {
	quote := h.cache.GetCEX(result.Symbol)
	if quote == nil {
		return coinQuantityQuote{}
	}
	krwPrice := quote.KRWPrice
	if krwPrice == 0 && quote.USDPrice > 0 {
		rate := h.cache.ForexRate()
		if rate > 0 {
			krwPrice = quote.USDPrice * rate
		}
	}
	return coinQuantityQuote{
		name:     quote.Name,
		symbol:   quote.Symbol,
		usdPrice: quote.USDPrice,
		krwPrice: krwPrice,
	}
}

func (h *CoinHandler) quantityQuoteFromCoinGecko(ctx context.Context, result providers.CoinSearchResult) (coinQuantityQuote, error) {
	id := h.resolveCoinGeckoID(result)
	if id == "" {
		return coinQuantityQuote{}, nil
	}
	quote, err := h.coinGecko.FetchPrice(ctx, id)
	if err != nil {
		return coinQuantityQuote{}, err
	}
	krwPrice := 0.0
	if rate := h.cache.ForexRate(); rate > 0 {
		krwPrice = quote.USDPrice * rate
	}
	return coinQuantityQuote{
		name:     quote.Name,
		symbol:   quote.Symbol,
		usdPrice: quote.USDPrice,
		krwPrice: krwPrice,
	}, nil
}

func (h *CoinHandler) quantityQuoteFromDEX(ctx context.Context, result providers.CoinSearchResult) (coinQuantityQuote, error) {
	quote, err := h.fetchDEXQuote(ctx, result)
	if err != nil {
		return coinQuantityQuote{}, err
	}
	if quote == nil {
		return coinQuantityQuote{}, fmt.Errorf("dex lookup unavailable")
	}
	krwPrice := 0.0
	if rate := h.cache.ForexRate(); rate > 0 {
		krwPrice = quote.USDPrice * rate
	}
	return coinQuantityQuote{
		name:     quote.Name,
		symbol:   quote.Symbol,
		usdPrice: quote.USDPrice,
		krwPrice: krwPrice,
	}, nil
}

func (h *CoinHandler) fetchDEXQuote(ctx context.Context, result providers.CoinSearchResult) (*providers.DEXQuote, error) {
	if h.dexScreener == nil {
		return nil, nil
	}
	if result.ContractAddress != "" {
		return h.dexScreener.FetchByAddress(ctx, result.ContractAddress)
	}
	return h.dexScreener.Search(ctx, result.Symbol)
}
