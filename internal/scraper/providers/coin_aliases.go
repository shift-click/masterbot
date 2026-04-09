package providers

import (
	"sort"
	"strings"
)

// CoinAliases holds the mapping from user input to canonical local coin keys and typed local results.
type CoinAliases struct {
	aliases      map[string]string
	localResults map[string]CoinSearchResult
}

// NewCoinAliases creates a new CoinAliases with embedded reference data.
func NewCoinAliases() *CoinAliases {
	return &CoinAliases{
		aliases:      loadCoinAliases(),
		localResults: loadCoinLocalResults(),
	}
}

// Lookup tries to find a coin symbol for the given input.
// Returns (symbol, true) if found, ("", false) otherwise.
func (a *CoinAliases) Lookup(input string) (string, bool) {
	result, ok := a.Resolve(input)
	if !ok || strings.TrimSpace(result.Symbol) == "" {
		return "", false
	}
	return result.Symbol, true
}

// Resolve returns the typed local coin result for the given input using local exact data only.
func (a *CoinAliases) Resolve(input string) (CoinSearchResult, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return CoinSearchResult{}, false
	}

	if result, ok := a.lookupExact(input); ok {
		return result, true
	}

	upper := strings.ToUpper(input)
	if upper != input {
		if result, ok := a.lookupExact(upper); ok {
			return result, true
		}
	}

	return CoinSearchResult{}, false
}

func (a *CoinAliases) lookupExact(input string) (CoinSearchResult, bool) {
	if result, ok := a.localResults[input]; ok {
		return result, true
	}

	canonical, ok := a.aliases[input]
	if !ok {
		return CoinSearchResult{}, false
	}

	result, ok := a.localResults[canonical]
	if !ok {
		return CoinSearchResult{}, false
	}
	return result, true
}

// IsCoinTicker returns true if the input looks like a known coin ticker.
func (a *CoinAliases) IsCoinTicker(input string) bool {
	_, ok := a.Lookup(input)
	return ok
}

func (a *CoinAliases) BinanceSymbols() []string {
	set := make(map[string]struct{})
	for _, result := range a.localResults {
		if sym := result.EffectiveBinanceSymbol(); sym != "" {
			set[sym] = struct{}{}
		}
	}
	return sortedStringKeys(set)
}

func (a *CoinAliases) UpbitSymbols() []string {
	set := make(map[string]struct{})
	for _, result := range a.localResults {
		if sym := result.EffectiveUpbitSymbol(); sym != "" {
			set[sym] = struct{}{}
		}
	}
	return sortedStringKeys(set)
}

func (a *CoinAliases) MarketCapSymbols() []string {
	set := make(map[string]struct{})
	for _, result := range a.localResults {
		if result.Symbol != "" && result.HasCEXCapability() {
			set[strings.ToUpper(result.Symbol)] = struct{}{}
		}
	}
	return sortedStringKeys(set)
}

func (a *CoinAliases) LocalResults() []CoinSearchResult {
	results := make([]CoinSearchResult, 0, len(a.localResults))
	for _, result := range a.localResults {
		results = append(results, result)
	}
	return results
}

func sortedStringKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
