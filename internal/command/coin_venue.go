package command

import (
	"context"
	"strings"

	"github.com/shift-click/masterbot/internal/scraper/providers"
)

func (h *CoinHandler) selectQuoteVenue(result providers.CoinSearchResult) providers.CoinQuoteVenue {
	try := func(venue providers.CoinQuoteVenue) bool {
		switch venue {
		case providers.CoinQuoteVenueCEX:
			return h.hasCEXQuote(result)
		case providers.CoinQuoteVenueCoinGecko:
			return h.resolveCoinGeckoID(result) != ""
		case providers.CoinQuoteVenueDEX:
			return result.HasDEXCapability()
		default:
			return false
		}
	}

	if pref := result.PreferredQuoteVenue; pref != providers.CoinQuoteVenueUnknown && try(pref) {
		return pref
	}

	for _, venue := range []providers.CoinQuoteVenue{
		providers.CoinQuoteVenueCEX,
		providers.CoinQuoteVenueCoinGecko,
		providers.CoinQuoteVenueDEX,
	} {
		if try(venue) {
			return venue
		}
	}
	return providers.CoinQuoteVenueUnknown
}

func (h *CoinHandler) hasCEXQuote(result providers.CoinSearchResult) bool {
	if !result.HasCEXCapability() {
		return false
	}
	quote := h.cache.GetCEX(result.Symbol)
	return quote != nil && (quote.USDPrice > 0 || quote.KRWPrice > 0)
}

func (h *CoinHandler) resolveCoinGeckoID(result providers.CoinSearchResult) string {
	if result.CoinGeckoID != "" {
		return result.CoinGeckoID
	}
	if h.coinGecko == nil {
		return ""
	}
	if id, ok := h.coinGecko.LookupID(result.Symbol); ok {
		return id
	}
	return ""
}

func (h *CoinHandler) lookupByVenue(ctx context.Context, result providers.CoinSearchResult) (string, error) {
	if isReferenceOnlyCoin(result) {
		return "", providers.ErrCoinQuoteUnavailable
	}
	switch h.selectQuoteVenue(result) {
	case providers.CoinQuoteVenueCEX:
		return h.lookupCEX(ctx, result)
	case providers.CoinQuoteVenueCoinGecko:
		return h.lookupCoinGecko(ctx, result)
	case providers.CoinQuoteVenueDEX:
		return h.lookupDEX(ctx, result)
	default:
		return "", nil
	}
}

func isReferenceOnlyCoin(result providers.CoinSearchResult) bool {
	return strings.HasPrefix(strings.TrimSpace(result.CoinGeckoID), "UNSUPPORTED:")
}
