package providers

import (
	"context"
	"log/slog"
	"strings"
)

// CoinResolver resolves user input to a CoinSearchResult via 3-tier lookup.
type CoinResolver struct {
	aliases     *CoinAliases
	coinGecko   *CoinGecko
	dexScreener *DexScreener
	logger      *slog.Logger
}

// NewCoinResolver creates a new CoinResolver.
func NewCoinResolver(
	aliases *CoinAliases,
	coinGecko *CoinGecko,
	dexScreener *DexScreener,
	logger *slog.Logger,
) *CoinResolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &CoinResolver{
		aliases:     aliases,
		coinGecko:   coinGecko,
		dexScreener: dexScreener,
		logger:      logger.With("component", "coin_resolver"),
	}
}

// Resolve converts user input into a CoinSearchResult.
// Returns (result, true) if resolved, (zero, false) if not a coin.
func (r *CoinResolver) Resolve(ctx context.Context, input string) (CoinSearchResult, bool) {
	if result, ok := r.ResolveLocalOnly(input); ok {
		if enriched, ok := r.enrichLocalDEX(ctx, result); ok {
			return enriched, true
		}
		return result, true
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return CoinSearchResult{}, false
	}

	// Tier 2: CoinGecko ID list.
	upper := strings.ToUpper(input)
	if r.coinGecko != nil {
		if id, ok := r.coinGecko.LookupID(upper); ok {
			r.logger.Debug("coin resolved via coingecko", "input", input, "id", id)
			return CoinSearchResult{
				Symbol:      upper,
				CoinGeckoID: id,
				Tier:        CoinTierCoinGecko,
			}, true
		}
	}

	// Tier 3: DexScreener search.
	if r.dexScreener == nil {
		return CoinSearchResult{}, false
	}
	quote, err := r.dexScreener.Search(ctx, input)
	if err != nil {
		r.logger.Debug("coin not found via dexscreener", "input", input, "error", err)
		return CoinSearchResult{}, false
	}

	r.logger.Debug("coin resolved via dexscreener", "input", input, "symbol", quote.Symbol, "chain", quote.ChainID)
	return CoinSearchResult{
		Symbol:          quote.Symbol,
		Name:            quote.Name,
		ContractAddress: quote.ContractAddress,
		PairAddress:     quote.PairAddress,
		ChainID:         quote.ChainID,
		Tier:            CoinTierDEX,
	}, true
}

// ResolveLocalOnly resolves user input without performing any network requests.
func (r *CoinResolver) ResolveLocalOnly(input string) (CoinSearchResult, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return CoinSearchResult{}, false
	}

	// Check if input looks like a contract address (0x...).
	if isContractAddress(input) {
		return CoinSearchResult{
			ContractAddress: input,
			Tier:            CoinTierDEX,
		}, true
	}

	// Tier 1: Local alias dictionary.
	if r.aliases != nil {
		if result, ok := r.aliases.Resolve(input); ok {
			r.logger.Debug("coin resolved via local registry", "input", input, "symbol", result.Symbol, "tier", result.Tier)
			return result, true
		}
	}

	return CoinSearchResult{}, false
}

func (r *CoinResolver) enrichLocalDEX(ctx context.Context, result CoinSearchResult) (CoinSearchResult, bool) {
	if result.Tier != CoinTierDEX {
		return result, true
	}
	if result.ContractAddress == "" || r.dexScreener == nil {
		return result, true
	}
	if result.PairAddress != "" && result.ChainID != "" && result.Symbol != "" && result.Name != "" {
		return result, true
	}

	quote, err := r.dexScreener.FetchByAddress(ctx, result.ContractAddress)
	if err != nil {
		r.logger.Debug("local dex metadata refresh failed", "address", result.ContractAddress, "error", err)
		return result, true
	}

	if result.Symbol == "" {
		result.Symbol = quote.Symbol
	}
	if result.Name == "" {
		result.Name = quote.Name
	}
	if result.ChainID == "" {
		result.ChainID = quote.ChainID
	}
	if result.PairAddress == "" {
		result.PairAddress = quote.PairAddress
	}
	return result, true
}

// isContractAddress checks if input looks like an Ethereum/EVM contract address.
func isContractAddress(s string) bool {
	if !strings.HasPrefix(s, "0x") && !strings.HasPrefix(s, "0X") {
		return false
	}
	// Ethereum addresses are 42 chars (0x + 40 hex chars).
	// Solana addresses are longer (32-44 chars, base58).
	// Accept anything starting with 0x and length >= 10.
	return len(s) >= 10
}
