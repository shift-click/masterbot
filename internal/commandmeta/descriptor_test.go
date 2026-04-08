package commandmeta

import "testing"

func TestDescriptorsLookupAndHelpers(t *testing.T) {
	t.Parallel()

	all := Descriptors()
	if len(all) == 0 {
		t.Fatal("expected descriptors")
	}

	coin, ok := Lookup("  coin  ")
	if !ok {
		t.Fatal("expected coin lookup by alias")
	}
	if coin.ID != "coin" {
		t.Fatalf("lookup id = %q, want coin", coin.ID)
	}

	if _, ok := Lookup("unknown-intent"); ok {
		t.Fatal("unexpected lookup success for unknown intent")
	}
	if id, ok := NormalizeIntentID("stock.quote"); !ok || id != "stock" {
		t.Fatalf("NormalizeIntentID(stock.quote) = (%q,%v)", id, ok)
	}
	if _, ok := NormalizeIntentID("   "); ok {
		t.Fatal("expected NormalizeIntentID miss for blank value")
	}
	if got := DisplayName("coin"); got != "코인" {
		t.Fatalf("DisplayName(coin) = %q", got)
	}
	if got := DisplayName("does-not-exist"); got != "does-not-exist" {
		t.Fatalf("DisplayName unknown = %q", got)
	}

	admin := Must("admin")
	if admin.ID != "admin" {
		t.Fatalf("Must(admin).ID = %q", admin.ID)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for Must(unknown)")
		}
	}()
	_ = Must("unknown")
}

func TestDescriptorsAndToggleableReturnsDefensiveCopies(t *testing.T) {
	t.Parallel()

	list := Descriptors()
	originalName := list[0].Name
	list[0].Name = "changed"
	list[0].SlashAliases = append(list[0].SlashAliases, "changed")

	again := Descriptors()
	if again[0].Name != originalName {
		t.Fatalf("descriptor copy should not mutate source, got %q want %q", again[0].Name, originalName)
	}

	ids := ToggleableIntentIDs()
	if len(ids) == 0 {
		t.Fatal("expected toggleable intents")
	}
	got := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		got[id] = struct{}{}
	}
	if _, exists := got["admin"]; exists {
		t.Fatal("admin must be excluded from toggleable list")
	}
	if _, exists := got["forex-convert"]; exists {
		t.Fatal("forex-convert must be excluded from toggleable list")
	}
	if _, exists := got["calc"]; exists {
		t.Fatal("calc must be excluded from toggleable list")
	}
	if _, exists := got["coin"]; !exists {
		t.Fatal("coin should be toggleable")
	}
	if _, exists := got["weather"]; !exists {
		t.Fatal("weather should be toggleable")
	}
}
