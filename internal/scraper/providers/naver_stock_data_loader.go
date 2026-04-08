package providers

import (
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

//go:embed data/naver_aliases.json data/naver_aliases.generated.json data/naver_local_results.json data/naver_local_results.generated.json
var naverStockDataFS embed.FS

const (
	naverAliasesFile          = "data/naver_aliases.json"
	naverGeneratedAliasesFile = "data/naver_aliases.generated.json"
	naverLocalResultsFile     = "data/naver_local_results.json"
	naverGeneratedResultsFile = "data/naver_local_results.generated.json"
)

var (
	naverAliasesOnce sync.Once
	naverAliasesData map[string]string
	naverAliasesErr  error

	naverResultsOnce sync.Once
	naverResultsData map[string]StockSearchResult
	naverResultsErr  error
)

func loadNaverAliases() map[string]string {
	naverAliasesOnce.Do(func() {
		curated, err := readNaverAliasAsset(naverAliasesFile)
		if err != nil {
			naverAliasesErr = err
			return
		}
		generated, err := readNaverAliasAsset(naverGeneratedAliasesFile)
		if err != nil {
			naverAliasesErr = err
			return
		}
		localResults, err := readNaverLocalResultRecords()
		if err != nil {
			naverAliasesErr = err
			return
		}
		if err := validateAliases(curated); err != nil {
			naverAliasesErr = err
			return
		}
		if err := validateAliases(generated); err != nil {
			naverAliasesErr = err
			return
		}
		if err := validateAliasTargets(generated, localResults); err != nil {
			naverAliasesErr = err
			return
		}
		naverAliasesData = mergeAliases(curated, generated)
	})
	if naverAliasesErr != nil {
		slog.Default().Warn("failed to load naver aliases data", "error", naverAliasesErr)
		return fallbackNaverAliases()
	}
	return cloneStringMap(naverAliasesData)
}

func loadNaverLocalResults() map[string]StockSearchResult {
	naverResultsOnce.Do(func() {
		parsed, err := readNaverLocalResultRecords()
		if err != nil {
			naverResultsErr = err
			return
		}
		if err := validateLocalResults(parsed); err != nil {
			naverResultsErr = err
			return
		}
		naverResultsData = materializeDomesticNumericKeys(parsed)
	})
	if naverResultsErr != nil {
		slog.Default().Warn("failed to load naver local results data", "error", naverResultsErr)
		return fallbackNaverLocalResults()
	}
	return cloneStockResultMap(naverResultsData)
}

func readNaverAliasAsset(file string) (map[string]string, error) {
	b, err := naverStockDataFS.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var parsed map[string]string
	if err := json.Unmarshal(b, &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func readNaverLocalResultRecords() (map[string]StockSearchResult, error) {
	curated, err := readNaverLocalResultAsset(naverLocalResultsFile)
	if err != nil {
		return nil, err
	}
	generated, err := readNaverLocalResultAsset(naverGeneratedResultsFile)
	if err != nil {
		return nil, err
	}
	return mergeLocalResults(curated, generated), nil
}

func readNaverLocalResultAsset(file string) (map[string]StockSearchResult, error) {
	b, err := naverStockDataFS.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var parsed map[string]StockSearchResult
	if err := json.Unmarshal(b, &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func cloneStringMap(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func cloneStockResultMap(src map[string]StockSearchResult) map[string]StockSearchResult {
	dst := make(map[string]StockSearchResult, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func validateAliases(data map[string]string) error {
	if len(data) == 0 {
		return fmt.Errorf("aliases data is empty")
	}
	for k, v := range data {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			return fmt.Errorf("aliases contains empty key/value")
		}
	}
	return nil
}

func validateAliasTargets(aliases map[string]string, results map[string]StockSearchResult) error {
	for alias, target := range aliases {
		if _, ok := results[target]; !ok {
			return fmt.Errorf("alias %q points to unknown local result %q", alias, target)
		}
	}
	return nil
}

func mergeAliases(curated, generated map[string]string) map[string]string {
	merged := cloneStringMap(curated)
	for alias, target := range generated {
		if _, exists := merged[alias]; exists {
			continue
		}
		merged[alias] = target
	}
	return merged
}

func mergeLocalResults(curated, generated map[string]StockSearchResult) map[string]StockSearchResult {
	merged := cloneStockResultMap(curated)
	for key, value := range generated {
		if _, exists := merged[key]; exists {
			continue
		}
		merged[key] = value
	}
	return merged
}

func materializeDomesticNumericKeys(data map[string]StockSearchResult) map[string]StockSearchResult {
	result := cloneStockResultMap(data)
	for _, entry := range data {
		if !looksLikeDomesticStockCode(entry.Code) {
			continue
		}
		if strings.TrimSpace(entry.NationCode) != "" && entry.NationCode != "KOR" {
			continue
		}
		if _, exists := result[entry.Code]; exists {
			continue
		}
		result[entry.Code] = entry
	}
	return result
}

func validateLocalResults(data map[string]StockSearchResult) error {
	if len(data) == 0 {
		return fmt.Errorf("local results data is empty")
	}
	for k, v := range data {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("local results contains empty key")
		}
		if strings.TrimSpace(v.Code) == "" || strings.TrimSpace(v.Name) == "" || strings.TrimSpace(v.Market) == "" {
			return fmt.Errorf("local results entry is missing required fields for key %q", k)
		}
		if strings.TrimSpace(v.NationCode) != "" && v.NationCode != "KOR" && strings.TrimSpace(v.ReutersCode) == "" {
			return fmt.Errorf("world stock entry missing reuters code for key %q", k)
		}
	}
	return nil
}

func fallbackNaverAliases() map[string]string {
	return map[string]string{
		"삼전":  "삼성전자",
		"하닉":  "SK하이닉스",
		"구글":  "GOOGL",
		"테슬라": "TSLA",
		"애플":  "AAPL",
	}
}

func fallbackNaverLocalResults() map[string]StockSearchResult {
	return materializeDomesticNumericKeys(map[string]StockSearchResult{
		"삼전":   {Code: "005930", Name: "삼성전자", Market: "KOSPI"},
		"하닉":   {Code: "000660", Name: "SK하이닉스", Market: "KOSPI"},
		"구글":   {Code: "GOOGL", Name: "알파벳 Class A", Market: "NASDAQ", NationCode: "USA", ReutersCode: "GOOGL.O"},
		"테슬라":  {Code: "TSLA", Name: "테슬라", Market: "NASDAQ", NationCode: "USA", ReutersCode: "TSLA.O"},
		"애플":   {Code: "AAPL", Name: "애플", Market: "NASDAQ", NationCode: "USA", ReutersCode: "AAPL.O"},
		"삼성전자": {Code: "005930", Name: "삼성전자", Market: "KOSPI"},
	})
}
