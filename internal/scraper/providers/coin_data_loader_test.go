package providers

import "testing"

func TestLoadCoinAliases_FromEmbeddedData(t *testing.T) {
	t.Parallel()

	aliases := loadCoinAliases()
	if len(aliases) == 0 {
		t.Fatal("coin aliases should not be empty")
	}
	if got := aliases["모나드"]; got != "monad" {
		t.Fatalf("aliases[모나드] = %q, want monad", got)
	}
	if got := aliases["MON"]; got != "monad" {
		t.Fatalf("aliases[MON] = %q, want monad", got)
	}
	if got := aliases["빗코"]; got != "BTC" {
		t.Fatalf("aliases[빗코] = %q, want BTC", got)
	}
	if got := aliases["wormhole"]; got != "W" {
		t.Fatalf("aliases[wormhole] = %q, want W", got)
	}
	if got := aliases["opn"]; got != "opinion" {
		t.Fatalf("aliases[opn] = %q, want opinion", got)
	}
	if got := aliases["링크"]; got != "LINK" {
		t.Fatalf("aliases[링크] = %q, want LINK (curated precedence)", got)
	}
	if _, ok := aliases["마나"]; ok {
		t.Fatal("guarded alias 마나 should not be imported into default exact aliases")
	}
}

func TestLoadCoinLocalResults_FromEmbeddedData(t *testing.T) {
	t.Parallel()

	results := loadCoinLocalResults()
	if len(results) == 0 {
		t.Fatal("coin local results should not be empty")
	}
	got, ok := results["monad"]
	if !ok {
		t.Fatal("monad entry should exist")
	}
	if got.Tier != CoinTierDEX {
		t.Fatalf("monad tier = %v, want %v", got.Tier, CoinTierDEX)
	}
	if got.ContractAddress == "" {
		t.Fatal("monad contract address should not be empty")
	}
	if got.UpbitMarket != "KRW-MON" {
		t.Fatalf("monad upbit market = %q, want KRW-MON", got.UpbitMarket)
	}
	if got.PreferredChartVenue != CoinChartVenueUpbit {
		t.Fatalf("monad preferred chart venue = %q, want %q", got.PreferredChartVenue, CoinChartVenueUpbit)
	}
	if generated, ok := results["opinion"]; !ok {
		t.Fatal("generated opinion entry should exist")
	} else {
		if generated.Tier != CoinTierCoinGecko {
			t.Fatalf("opinion tier = %v, want %v", generated.Tier, CoinTierCoinGecko)
		}
		if generated.CoinGeckoID != "opinion" {
			t.Fatalf("opinion CoinGeckoID = %q, want opinion", generated.CoinGeckoID)
		}
	}
	if referenceOnly, ok := results["kujira"]; !ok {
		t.Fatal("reference-only kujira entry should exist")
	} else if referenceOnly.CoinGeckoID != "UNSUPPORTED:kujira" {
		t.Fatalf("kujira CoinGeckoID = %q, want UNSUPPORTED:kujira", referenceOnly.CoinGeckoID)
	}
}

func TestLoadCoinMaps_ReturnsClonedMap(t *testing.T) {
	t.Parallel()

	aliases := loadCoinAliases()
	aliases["모나드"] = "modified"
	aliases2 := loadCoinAliases()
	if aliases2["모나드"] == "modified" {
		t.Fatal("loadCoinAliases should return cloned map")
	}

	results := loadCoinLocalResults()
	entry := results["monad"]
	entry.Symbol = "BROKEN"
	results["monad"] = entry
	results2 := loadCoinLocalResults()
	if results2["monad"].Symbol == "BROKEN" {
		t.Fatal("loadCoinLocalResults should return cloned map")
	}
}

func TestValidateCoinLocalResults(t *testing.T) {
	t.Parallel()

	if err := validateCoinLocalResults(map[string]coinLocalResultRecord{
		"monad": {Symbol: "MON", Tier: "dex"},
	}); err == nil {
		t.Fatal("expected dex local result without contract address to fail validation")
	}

	if err := validateCoinLocalResults(map[string]coinLocalResultRecord{
		"aave": {Symbol: "AAVE", Tier: "coingecko"},
	}); err == nil {
		t.Fatal("expected coingecko local result without id to fail validation")
	}

	if err := validateCoinLocalResults(map[string]coinLocalResultRecord{
		"monad": {Symbol: "MON", Tier: "dex", ContractAddress: "0xabc", PreferredChartVenue: "bogus"},
	}); err == nil {
		t.Fatal("expected invalid preferred chart venue to fail validation")
	}
}

func TestValidateCoinAliasTargetsAndOverlap(t *testing.T) {
	t.Parallel()

	results := map[string]coinLocalResultRecord{
		"BTC": {Symbol: "BTC", Tier: "cex"},
	}
	if err := validateCoinAliasTargets(map[string]string{"빗코": "BTC"}, results); err != nil {
		t.Fatalf("validateCoinAliasTargets valid = %v", err)
	}
	if err := validateCoinAliasTargets(map[string]string{"빗코": "UNKNOWN"}, results); err == nil {
		t.Fatal("expected unknown target validation failure")
	}
	if err := validateNoAliasOverlap(
		map[string]string{"마나": "MANA"},
		map[string]string{"마나": "MANA"},
	); err == nil {
		t.Fatal("expected overlap validation failure")
	}
}

func TestMergeCoinAliasesCuratedWins(t *testing.T) {
	t.Parallel()

	merged := mergeCoinAliases(
		map[string]string{"MON": "monad", "btc": "BTC"},
		map[string]string{"MON": "other", "빗코": "BTC"},
	)
	if got := merged["MON"]; got != "monad" {
		t.Fatalf("merged[MON] = %q, want monad", got)
	}
	if got := merged["빗코"]; got != "BTC" {
		t.Fatalf("merged[빗코] = %q, want BTC", got)
	}
}

func TestLoadCoinAliasAssetsAndValidationHelpers(t *testing.T) {
	t.Parallel()

	curated, generated, guarded, results, err := loadCoinAliasAssets()
	if err != nil {
		t.Fatalf("loadCoinAliasAssets: %v", err)
	}
	if len(curated) == 0 || len(results) == 0 {
		t.Fatalf("expected embedded assets to be non-empty: curated=%d results=%d", len(curated), len(results))
	}
	if err := validateCoinAliasAssets(curated, generated, guarded, results); err != nil {
		t.Fatalf("validateCoinAliasAssets: %v", err)
	}
}

func TestValidateCoinLocalResultHelpers(t *testing.T) {
	t.Parallel()

	valid := coinLocalResultRecord{Symbol: "BTC", Tier: "cex", PreferredQuoteVenue: "cex"}
	if err := validateCoinLocalResult("BTC", valid); err != nil {
		t.Fatalf("validateCoinLocalResult valid = %v", err)
	}
	if err := validateCoinLocalResult("", valid); err == nil {
		t.Fatal("expected empty key validation failure")
	}
	if err := validateCoinTierFields("monad", CoinTierDEX, coinLocalResultRecord{ContractAddress: "0xabc"}); err != nil {
		t.Fatalf("validateCoinTierFields dex valid = %v", err)
	}
	if err := validateCoinTierFields("monad", CoinTierDEX, coinLocalResultRecord{}); err == nil {
		t.Fatal("expected missing dex contract validation failure")
	}
	if err := validatePreferredVenue("btc", "quote", "cex", func(raw string) bool { return parseQuoteVenue(raw) != CoinQuoteVenueUnknown }); err != nil {
		t.Fatalf("validatePreferredVenue valid = %v", err)
	}
	if err := validatePreferredVenue("btc", "quote", "bogus", func(string) bool { return false }); err == nil {
		t.Fatal("expected invalid preferred venue failure")
	}
}
