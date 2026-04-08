package intent_test

import (
	"testing"

	"github.com/shift-click/masterbot/internal/intent"
)

func TestNewCatalogRejectsDuplicateSlashAlias(t *testing.T) {
	t.Parallel()

	_, err := intent.NewCatalog([]intent.Entry{
		{ID: "coin", Name: "코인", SlashAliases: []string{"quote"}},
		{ID: "stock", Name: "주식", SlashAliases: []string{"quote"}},
	})
	if err == nil {
		t.Fatal("expected duplicate slash alias error")
	}
}

func TestDefaultCatalogResolvesAdminExplicitAlias(t *testing.T) {
	t.Parallel()

	entry, ok := intent.DefaultCatalog().ResolveExplicit("관리")
	if !ok {
		t.Fatal("expected 관리 explicit alias to resolve")
	}
	if entry.ID != "admin" {
		t.Fatalf("entry.ID = %q, want %q", entry.ID, "admin")
	}
}
