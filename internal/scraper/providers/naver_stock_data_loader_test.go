package providers

import "testing"

func TestLoadNaverAliases_FromEmbeddedData(t *testing.T) {
	t.Parallel()

	aliases := loadNaverAliases()
	if len(aliases) == 0 {
		t.Fatal("aliases should not be empty")
	}
	if got := aliases["삼전"]; got != "삼성전자" {
		t.Fatalf("aliases[삼전] = %q, want 삼성전자", got)
	}
	if got := aliases["보일"]; got != "BOIL" {
		t.Fatalf("aliases[보일] = %q, want BOIL", got)
	}
	if got := aliases["빅스"]; got != "^VIX" {
		t.Fatalf("aliases[빅스] = %q, want ^VIX", got)
	}
}

func TestLoadNaverLocalResults_FromEmbeddedData(t *testing.T) {
	t.Parallel()

	results := loadNaverLocalResults()
	if len(results) == 0 {
		t.Fatal("local results should not be empty")
	}
	got, ok := results["GOOGL"]
	if !ok {
		t.Fatal("GOOGL entry should exist")
	}
	if got.ReutersCode != "GOOGL.O" {
		t.Fatalf("GOOGL ReutersCode = %q, want GOOGL.O", got.ReutersCode)
	}
	if numeric, ok := results["005930"]; !ok {
		t.Fatal("curated domestic numeric key 005930 should exist")
	} else if numeric.Name != "삼성전자" {
		t.Fatalf("results[005930].Name = %q, want 삼성전자", numeric.Name)
	}
	if generated, ok := results["BOIL"]; !ok {
		t.Fatal("generated BOIL entry should exist")
	} else {
		if generated.ReutersCode == "" {
			t.Fatal("generated BOIL ReutersCode should not be empty")
		}
		if generated.NationCode != "USA" {
			t.Fatalf("generated BOIL NationCode = %q, want USA", generated.NationCode)
		}
	}
	if referenceOnly, ok := results["^VIX"]; !ok {
		t.Fatal("reference-only ^VIX entry should exist")
	} else if referenceOnly.ReutersCode != "UNSUPPORTED:^VIX" {
		t.Fatalf("^VIX ReutersCode = %q, want UNSUPPORTED:^VIX", referenceOnly.ReutersCode)
	}
}

func TestLoadMaps_ReturnsClonedMap(t *testing.T) {
	t.Parallel()

	aliases := loadNaverAliases()
	aliases["삼전"] = "수정됨"
	aliases2 := loadNaverAliases()
	if aliases2["삼전"] == "수정됨" {
		t.Fatal("loadNaverAliases should return cloned map")
	}

	results := loadNaverLocalResults()
	entry := results["삼전"]
	entry.Name = "수정됨"
	results["삼전"] = entry
	results2 := loadNaverLocalResults()
	if results2["삼전"].Name == "수정됨" {
		t.Fatal("loadNaverLocalResults should return cloned map")
	}
}
