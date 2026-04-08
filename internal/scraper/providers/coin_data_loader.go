package providers

import (
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

//go:embed data/coin_aliases.json data/coin_aliases.generated.json data/coin_aliases.guarded.json data/coin_local_results.json data/coin_local_results.generated.json
var coinDataFS embed.FS

const (
	coinAliasesFile          = "data/coin_aliases.json"
	coinGeneratedAliasesFile = "data/coin_aliases.generated.json"
	coinGuardedAliasesFile   = "data/coin_aliases.guarded.json"
	coinLocalResultsFile     = "data/coin_local_results.json"
	coinGeneratedResultsFile = "data/coin_local_results.generated.json"
)

type coinLocalResultRecord struct {
	Symbol              string `json:"symbol"`
	Name                string `json:"name"`
	Tier                string `json:"tier"`
	ContractAddress     string `json:"contract_address,omitempty"`
	PairAddress         string `json:"pair_address,omitempty"`
	ChainID             string `json:"chain_id,omitempty"`
	CoinGeckoID         string `json:"coingecko_id,omitempty"`
	BinanceSymbol       string `json:"binance_symbol,omitempty"`
	UpbitMarket         string `json:"upbit_market,omitempty"`
	PreferredQuoteVenue string `json:"preferred_quote_venue,omitempty"`
	PreferredChartVenue string `json:"preferred_chart_venue,omitempty"`
}

var (
	coinAliasesOnce sync.Once
	coinAliasesData map[string]string
	coinAliasesErr  error

	coinResultsOnce sync.Once
	coinResultsData map[string]CoinSearchResult
	coinResultsErr  error
)

func loadCoinAliases() map[string]string {
	coinAliasesOnce.Do(func() {
		curated, generated, guarded, localResultRecords, err := loadCoinAliasAssets()
		if err != nil {
			coinAliasesErr = err
			return
		}

		if err := validateCoinAliasAssets(curated, generated, guarded, localResultRecords); err != nil {
			coinAliasesErr = err
			return
		}

		coinAliasesData = mergeCoinAliases(curated, generated)
	})
	if coinAliasesErr != nil {
		slog.Default().Warn("failed to load coin aliases data", "error", coinAliasesErr)
		return fallbackCoinAliases()
	}
	return cloneStringMap(coinAliasesData)
}

func loadCoinAliasAssets() (map[string]string, map[string]string, map[string]string, map[string]coinLocalResultRecord, error) {
	curated, err := readCoinAliasAsset(coinAliasesFile)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	generated, err := readCoinAliasAsset(coinGeneratedAliasesFile)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	guarded, err := readCoinAliasAsset(coinGuardedAliasesFile)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	localResultRecords, err := readCoinLocalResultRecords()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return curated, generated, guarded, localResultRecords, nil
}

func validateCoinAliasAssets(curated, generated, guarded map[string]string, results map[string]coinLocalResultRecord) error {
	if err := validateCoinAliases(curated); err != nil {
		return err
	}
	if err := validateCoinAliasAsset(generated, true); err != nil {
		return err
	}
	if err := validateCoinAliasAsset(guarded, true); err != nil {
		return err
	}
	if err := validateCoinAliasTargets(curated, results); err != nil {
		return err
	}
	if err := validateCoinAliasTargets(generated, results); err != nil {
		return err
	}
	return validateNoAliasOverlap(generated, guarded)
}

func loadCoinLocalResults() map[string]CoinSearchResult {
	coinResultsOnce.Do(func() {
		parsed, err := readCoinLocalResultRecords()
		if err != nil {
			coinResultsErr = err
			return
		}
		results, err := convertCoinLocalResults(parsed)
		if err != nil {
			coinResultsErr = err
			return
		}
		coinResultsData = results
	})
	if coinResultsErr != nil {
		slog.Default().Warn("failed to load coin local results data", "error", coinResultsErr)
		return fallbackCoinLocalResults()
	}
	return cloneCoinResultMap(coinResultsData)
}

func readCoinAliasAsset(file string) (map[string]string, error) {
	b, err := coinDataFS.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var parsed map[string]string
	if err := json.Unmarshal(b, &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func readCoinLocalResultRecords() (map[string]coinLocalResultRecord, error) {
	curated, err := readCoinLocalResultAsset(coinLocalResultsFile)
	if err != nil {
		return nil, err
	}
	generated, err := readCoinLocalResultAsset(coinGeneratedResultsFile)
	if err != nil {
		return nil, err
	}
	return mergeCoinLocalResultRecords(curated, generated), nil
}

func readCoinLocalResultAsset(file string) (map[string]coinLocalResultRecord, error) {
	b, err := coinDataFS.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var parsed map[string]coinLocalResultRecord
	if err := json.Unmarshal(b, &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func cloneCoinResultMap(src map[string]CoinSearchResult) map[string]CoinSearchResult {
	dst := make(map[string]CoinSearchResult, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func convertCoinLocalResults(data map[string]coinLocalResultRecord) (map[string]CoinSearchResult, error) {
	if err := validateCoinLocalResults(data); err != nil {
		return nil, err
	}

	results := make(map[string]CoinSearchResult, len(data))
	for key, v := range data {
		tier, err := parseCoinTier(v.Tier)
		if err != nil {
			return nil, fmt.Errorf("local result %q: %w", key, err)
		}
		results[key] = CoinSearchResult{
			Symbol:              strings.TrimSpace(v.Symbol),
			Name:                strings.TrimSpace(v.Name),
			Tier:                tier,
			ContractAddress:     strings.TrimSpace(v.ContractAddress),
			PairAddress:         strings.TrimSpace(v.PairAddress),
			ChainID:             strings.TrimSpace(v.ChainID),
			CoinGeckoID:         strings.TrimSpace(v.CoinGeckoID),
			BinanceSymbol:       strings.TrimSpace(v.BinanceSymbol),
			UpbitMarket:         strings.TrimSpace(v.UpbitMarket),
			PreferredQuoteVenue: parseQuoteVenue(v.PreferredQuoteVenue),
			PreferredChartVenue: parseChartVenue(v.PreferredChartVenue),
		}
	}
	return results, nil
}

func validateCoinAliases(data map[string]string) error {
	return validateCoinAliasAsset(data, false)
}

func validateCoinAliasAsset(data map[string]string, allowEmpty bool) error {
	if len(data) == 0 {
		if allowEmpty {
			return nil
		}
		return fmt.Errorf("coin aliases data is empty")
	}
	for k, v := range data {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			return fmt.Errorf("coin aliases contains empty key/value")
		}
	}
	return nil
}

func validateCoinAliasTargets(aliases map[string]string, results map[string]coinLocalResultRecord) error {
	for alias, target := range aliases {
		if _, ok := results[target]; !ok {
			return fmt.Errorf("coin alias %q points to unknown local result %q", alias, target)
		}
	}
	return nil
}

func validateNoAliasOverlap(primary, secondary map[string]string) error {
	for alias := range primary {
		if _, ok := secondary[alias]; ok {
			return fmt.Errorf("coin alias %q exists in multiple assets", alias)
		}
	}
	return nil
}

func mergeCoinAliases(curated, generated map[string]string) map[string]string {
	merged := cloneStringMap(curated)
	for alias, target := range generated {
		if _, exists := merged[alias]; exists {
			continue
		}
		merged[alias] = target
	}
	return merged
}

func mergeCoinLocalResultRecords(curated, generated map[string]coinLocalResultRecord) map[string]coinLocalResultRecord {
	merged := make(map[string]coinLocalResultRecord, len(curated)+len(generated))
	for key, value := range curated {
		merged[key] = value
	}
	for key, value := range generated {
		if _, exists := merged[key]; exists {
			continue
		}
		merged[key] = value
	}
	return merged
}

func validateCoinLocalResults(data map[string]coinLocalResultRecord) error {
	if len(data) == 0 {
		return fmt.Errorf("coin local results data is empty")
	}
	for key, record := range data {
		if err := validateCoinLocalResult(key, record); err != nil {
			return err
		}
	}
	return nil
}

func validateCoinLocalResult(key string, record coinLocalResultRecord) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("coin local results contains empty key")
	}
	if strings.TrimSpace(record.Symbol) == "" {
		return fmt.Errorf("coin local result %q missing symbol", key)
	}
	if strings.TrimSpace(record.Tier) == "" {
		return fmt.Errorf("coin local result %q missing tier", key)
	}
	tier, err := parseCoinTier(record.Tier)
	if err != nil {
		return fmt.Errorf("coin local result %q invalid tier: %w", key, err)
	}
	if err := validateCoinTierFields(key, tier, record); err != nil {
		return err
	}
	if err := validatePreferredVenue(key, "quote", record.PreferredQuoteVenue, func(raw string) bool {
		return parseQuoteVenue(raw) != CoinQuoteVenueUnknown
	}); err != nil {
		return err
	}
	return validatePreferredVenue(key, "chart", record.PreferredChartVenue, func(raw string) bool {
		return parseChartVenue(raw) != CoinChartVenueUnknown
	})
}

func validateCoinTierFields(key string, tier CoinTier, record coinLocalResultRecord) error {
	switch tier {
	case CoinTierDEX:
		if strings.TrimSpace(record.ContractAddress) == "" {
			return fmt.Errorf("coin local result %q missing contract address for dex tier", key)
		}
	case CoinTierCoinGecko:
		if strings.TrimSpace(record.CoinGeckoID) == "" {
			return fmt.Errorf("coin local result %q missing coingecko id for coingecko tier", key)
		}
	}
	return nil
}

func validatePreferredVenue(key, venueType, raw string, isValid func(string) bool) error {
	venue := strings.TrimSpace(raw)
	if venue == "" {
		return nil
	}
	if isValid(venue) {
		return nil
	}
	return fmt.Errorf("coin local result %q invalid preferred %s venue %q", key, venueType, venue)
}

func parseCoinTier(raw string) (CoinTier, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "cex":
		return CoinTierCEX, nil
	case "coingecko":
		return CoinTierCoinGecko, nil
	case "dex":
		return CoinTierDEX, nil
	default:
		return 0, fmt.Errorf("unsupported coin tier %q", raw)
	}
}

func parseQuoteVenue(raw string) CoinQuoteVenue {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(CoinQuoteVenueCEX):
		return CoinQuoteVenueCEX
	case string(CoinQuoteVenueCoinGecko):
		return CoinQuoteVenueCoinGecko
	case string(CoinQuoteVenueDEX):
		return CoinQuoteVenueDEX
	default:
		return CoinQuoteVenueUnknown
	}
}

func parseChartVenue(raw string) CoinChartVenue {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(CoinChartVenueBinance):
		return CoinChartVenueBinance
	case string(CoinChartVenueUpbit):
		return CoinChartVenueUpbit
	case string(CoinChartVenueDEX):
		return CoinChartVenueDEX
	default:
		return CoinChartVenueUnknown
	}
}

func fallbackCoinAliases() map[string]string {
	return map[string]string{
		"비트코인":    "BTC",
		"이더리움":    "ETH",
		"리플":      "XRP",
		"솔라나":     "SOL",
		"에이다":     "ADA",
		"도지코인":    "DOGE",
		"도지":      "DOGE",
		"폴카닷":     "DOT",
		"아발란체":    "AVAX",
		"폴리곤":     "MATIC",
		"체인링크":    "LINK",
		"유니스왑":    "UNI",
		"코스모스":    "ATOM",
		"이더리움클래식": "ETC",
		"스텔라루멘":   "XLM",
		"니어프로토콜":  "NEAR",
		"샌드박스":    "SAND",
		"엑시인피니티":  "AXS",
		"에이브":     "AAVE",
		"이오스":     "EOS",
		"트론":      "TRX",
		"시바이누":    "SHIB",
		"라이트코인":   "LTC",
		"비트코인캐시":  "BCH",
		"아비트럼":    "ARB",
		"옵티미즘":    "OP",
		"앱토스":     "APT",
		"수이":      "SUI",
		"세이":      "SEI",
		"페페":      "PEPE",
		"봉크":      "BONK",
		"위프":      "WIF",
		"플로키":     "FLOKI",
		"비엔비":     "BNB",
		"톤":       "TON",
		"톤코인":     "TON",
		"엑스알피":    "XRP",
		"비코":      "BTC",
		"알고랜드":    "ALGO",
		"팬텀":      "FTM",
		"디센트럴랜드":  "MANA",
		"스텔라":     "XLM",
		"비트":      "BTC",
		"이더":      "ETH",
		"솔":       "SOL",
		"닷":       "DOT",
		"링크":      "LINK",
		"유니":      "UNI",
		"아톰":      "ATOM",
		"이클":      "ETC",
		"니어":      "NEAR",
		"라코":      "LTC",
		"비캐":      "BCH",
		"시바":      "SHIB",
		"ㅂㅌ":      "BTC",
		"ㅇㄷ":      "ETH",
		"ㄹㅍ":      "XRP",
		"ㅅㄹㄴ":     "SOL",
		"ㄷㅈ":      "DOGE",
		"ㅇㅇㅅ":     "EOS",
		"BTC":     "BTC",
		"ETH":     "ETH",
		"XRP":     "XRP",
		"SOL":     "SOL",
		"ADA":     "ADA",
		"DOGE":    "DOGE",
		"DOT":     "DOT",
		"AVAX":    "AVAX",
		"MATIC":   "MATIC",
		"POL":     "MATIC",
		"LINK":    "LINK",
		"UNI":     "UNI",
		"ATOM":    "ATOM",
		"ETC":     "ETC",
		"XLM":     "XLM",
		"ALGO":    "ALGO",
		"NEAR":    "NEAR",
		"FTM":     "FTM",
		"SAND":    "SAND",
		"MANA":    "MANA",
		"AXS":     "AXS",
		"AAVE":    "AAVE",
		"EOS":     "EOS",
		"TRX":     "TRX",
		"SHIB":    "SHIB",
		"LTC":     "LTC",
		"BCH":     "BCH",
		"ARB":     "ARB",
		"OP":      "OP",
		"APT":     "APT",
		"SUI":     "SUI",
		"SEI":     "SEI",
		"PEPE":    "PEPE",
		"BONK":    "BONK",
		"WIF":     "WIF",
		"FLOKI":   "FLOKI",
		"BNB":     "BNB",
		"TON":     "TON",
		"HBAR":    "HBAR",
		"FIL":     "FIL",
		"ICP":     "ICP",
		"VET":     "VET",
		"GRT":     "GRT",
		"INJ":     "INJ",
		"IMX":     "IMX",
		"RENDER":  "RENDER",
		"FET":     "FET",
		"STX":     "STX",
		"THETA":   "THETA",
		"RUNE":    "RUNE",
		"ENS":     "ENS",
		"MKR":     "MKR",
		"SNX":     "SNX",
		"CRV":     "CRV",
		"COMP":    "COMP",
		"1INCH":   "1INCH",
		"SUSHI":   "SUSHI",
		"YFI":     "YFI",
		"BAT":     "BAT",
		"ZRX":     "ZRX",
		"LDO":     "LDO",
		"RPL":     "RPL",
		"BLUR":    "BLUR",
		"JUP":     "JUP",
		"W":       "W",
		"WLD":     "WLD",
		"STRK":    "STRK",
		"TIA":     "TIA",
		"PYTH":    "PYTH",
		"JTO":     "JTO",
		"monad":   "monad",
		"MONAD":   "monad",
		"모나드":     "monad",
		"MON":     "monad",
	}
}

func fallbackCoinLocalResults() map[string]CoinSearchResult {
	results := map[string]CoinSearchResult{
		"monad": {
			Symbol:              "MON",
			Name:                "Monad",
			Tier:                CoinTierDEX,
			ContractAddress:     "0x3bd359C1119dA7Da1D913D1C4D2B7c461115433A",
			PairAddress:         "0x659bD0BC4167BA25c62E05656F78043E7eD4a9da",
			ChainID:             "monad",
			CoinGeckoID:         "monad",
			UpbitMarket:         "KRW-MON",
			PreferredQuoteVenue: CoinQuoteVenueCEX,
			PreferredChartVenue: CoinChartVenueUpbit,
		},
	}
	for _, canonical := range fallbackCoinAliases() {
		if canonical == "monad" {
			continue
		}
		if _, exists := results[canonical]; exists {
			continue
		}
		results[canonical] = CoinSearchResult{
			Symbol: canonical,
			Name:   canonical,
			Tier:   CoinTierCEX,
		}
	}
	return results
}
