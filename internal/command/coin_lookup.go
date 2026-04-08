package command

import (
	"context"

	"github.com/shift-click/masterbot/internal/scraper"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/pkg/formatter"
)

// lookup resolves a query and returns formatted coin data.
func (h *CoinHandler) lookup(ctx context.Context, query string, localOnly bool) (string, error) {
	var (
		result providers.CoinSearchResult
		ok     bool
	)
	if localOnly {
		result, ok = h.resolver.ResolveLocalOnly(query)
	} else {
		result, ok = h.resolver.Resolve(ctx, query)
	}
	if !ok {
		return "", nil
	}

	return h.lookupByVenue(ctx, result)
}

// lookupCEX looks up a CEX coin from the real-time cache.
func (h *CoinHandler) lookupCEX(ctx context.Context, result providers.CoinSearchResult) (string, error) {
	quote := h.cache.GetCEX(result.Symbol)
	if quote == nil {
		h.logger.Debug("CEX coin not in cache, trying CoinGecko fallback", "symbol", result.Symbol)
		if id := h.resolveCoinGeckoID(result); id != "" {
			return h.lookupCoinGecko(ctx, providers.CoinSearchResult{
				Symbol:      result.Symbol,
				Name:        result.Name,
				CoinGeckoID: id,
				Tier:        providers.CoinTierCoinGecko,
			})
		}
		return "", nil
	}

	mcap := ""
	if quote.MarketCap > 0 {
		rate := h.cache.ForexRate()
		mcap = scraper.FormatMarketCapKRW(quote.MarketCap, rate)
	}

	text := formatter.FormatCEXCoinQuote(formatter.CEXCoinData{
		Name:          quote.Name,
		Symbol:        quote.Symbol,
		MarketCap:     mcap,
		USDPrice:      quote.USDPrice,
		USDChangePct:  quote.USDChangePct,
		USDPrevClose:  quote.USDPrevClose,
		USDChange:     quote.USDChange,
		KRWPrice:      quote.KRWPrice,
		KRWChangePct:  quote.KRWChangePct,
		KRWPrevClose:  quote.KRWPrevClose,
		KRWChange:     quote.KRWChange,
		KimchiPremium: quote.KimchiPremium,
		HasKimchi:     quote.USDPrice > 0 && quote.KRWPrice > 0 && h.cache.ForexRate() > 0,
	})
	return text, nil
}

// lookupCoinGecko looks up a Tier 2 coin via CoinGecko.
func (h *CoinHandler) lookupCoinGecko(ctx context.Context, result providers.CoinSearchResult) (string, error) {
	if quote := h.cache.GetCEX(result.Symbol); quote != nil && (quote.USDPrice > 0 || quote.KRWPrice > 0) {
		return h.lookupCEX(ctx, providers.CoinSearchResult{Symbol: result.Symbol, Tier: providers.CoinTierCEX})
	}

	id := h.resolveCoinGeckoID(result)
	if id == "" {
		return "", nil
	}
	quote, err := h.coinGecko.FetchPrice(ctx, id)
	if err != nil {
		return "", err
	}

	rate := h.cache.ForexRate()
	krwPrice := 0.0
	if rate > 0 {
		krwPrice = quote.USDPrice * rate
	}

	mcap := ""
	if quote.MarketCap > 0 && rate > 0 {
		mcap = scraper.FormatMarketCapKRW(quote.MarketCap, rate)
	}

	text := formatter.FormatCEXCoinQuote(formatter.CEXCoinData{
		Name:         quote.Name,
		Symbol:       quote.Symbol,
		MarketCap:    mcap,
		USDPrice:     quote.USDPrice,
		USDChangePct: quote.USDChangePct,
		USDPrevClose: quote.USDPrevClose,
		USDChange:    quote.USDChange,
		KRWPrice:     krwPrice,
		KRWChangePct: quote.USDChangePct,
		HasKimchi:    false,
	})
	return text, nil
}

// lookupDEX looks up a DEX token via DexScreener.
func (h *CoinHandler) lookupDEX(ctx context.Context, result providers.CoinSearchResult) (string, error) {
	addr := result.ContractAddress
	if addr != "" {
		if h.dexHotList != nil {
			if cached := h.dexHotList.Get(addr); cached != nil {
				return h.formatDEX(cached), nil
			}
		}
	}

	if h.dexScreener == nil {
		return "", nil
	}

	var (
		quote *providers.DEXQuote
		err   error
	)
	if addr != "" {
		quote, err = h.dexScreener.FetchByAddress(ctx, addr)
	} else {
		quote, err = h.dexScreener.Search(ctx, result.Symbol)
	}
	if err != nil {
		return "", err
	}

	rate := h.cache.ForexRate()
	if rate > 0 && quote.USDPrice > 0 {
		quote.KRWPrice = quote.USDPrice * rate
		quote.KRWChangePct24h = quote.USDChangePct24h
	}

	h.cache.SetDEX(quote)
	if h.dexHotList != nil {
		h.dexHotList.Register(quote)
	}
	return h.formatDEX(quote), nil
}

func (h *CoinHandler) formatDEX(q *providers.DEXQuote) string {
	mcap := ""
	rate := h.cache.ForexRate()
	if q.MarketCap > 0 && rate > 0 {
		mcap = scraper.FormatMarketCapKRW(q.MarketCap, rate)
	} else if q.FDV > 0 && rate > 0 {
		mcap = scraper.FormatMarketCapKRW(q.FDV, rate)
	}

	return formatter.FormatDEXCoinQuote(formatter.DEXCoinData{
		Name:            q.Name,
		Symbol:          q.Symbol,
		ChainID:         q.ChainID,
		DEXName:         q.DEXName,
		USDPrice:        q.USDPrice,
		USDChangePct24h: q.USDChangePct24h,
		Volume24h:       q.Volume24h,
		Liquidity:       q.Liquidity,
		MarketCap:       mcap,
		KRWPrice:        q.KRWPrice,
		KRWChangePct:    q.KRWChangePct24h,
	})
}
