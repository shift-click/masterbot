package bot_test

import (
	"context"
	"strings"
	"testing"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/commandmeta"
	"github.com/shift-click/masterbot/internal/intent"
)

type registryStubHandler struct {
	name string
}

func (h registryStubHandler) Name() string { return h.name }
func (h registryStubHandler) Aliases() []string {
	return nil
}
func (h registryStubHandler) Description() string { return h.name }
func (h registryStubHandler) Execute(context.Context, bot.CommandContext) error {
	return nil
}

type registryDescribedHandler struct {
	descriptor commandmeta.Descriptor
}

func (h registryDescribedHandler) Descriptor() commandmeta.Descriptor { return h.descriptor }
func (h registryDescribedHandler) Name() string                       { return h.descriptor.Name }
func (h registryDescribedHandler) Aliases() []string {
	return append([]string(nil), h.descriptor.SlashAliases...)
}
func (h registryDescribedHandler) Description() string { return h.descriptor.Description }
func (h registryDescribedHandler) Execute(context.Context, bot.CommandContext) error {
	return nil
}

type registryDescribedFallback struct {
	descriptor commandmeta.Descriptor
}

func (h registryDescribedFallback) Descriptor() commandmeta.Descriptor { return h.descriptor }
func (h registryDescribedFallback) Name() string                       { return h.descriptor.Name }
func (h registryDescribedFallback) Aliases() []string {
	return append([]string(nil), h.descriptor.SlashAliases...)
}
func (h registryDescribedFallback) Description() string { return h.descriptor.Description }
func (h registryDescribedFallback) Execute(context.Context, bot.CommandContext) error {
	return nil
}
func (h registryDescribedFallback) HandleFallback(context.Context, bot.CommandContext) error {
	return nil
}

func TestRegistryRegisterRejectsUnknownCatalogEntry(t *testing.T) {
	t.Parallel()

	registry := bot.NewRegistry(intent.DefaultCatalog())

	err := registry.Register(registryStubHandler{name: "없는핸들러"})
	if err == nil {
		t.Fatal("expected unknown catalog entry error")
	}
}

func TestRegistryValidateReportsMissingHandlers(t *testing.T) {
	t.Parallel()

	catalog, err := intent.NewCatalog([]intent.Entry{
		{ID: "help", Name: "도움", SlashAliases: []string{"help"}},
		{ID: "coin", Name: "코인", SlashAliases: []string{"coin"}},
	})
	if err != nil {
		t.Fatalf("new catalog: %v", err)
	}

	registry := bot.NewRegistry(catalog)
	if err := registry.Register(registryStubHandler{name: "도움"}); err != nil {
		t.Fatalf("register: %v", err)
	}

	err = registry.Validate()
	if err == nil {
		t.Fatal("expected missing handler validation error")
	}
	if !strings.Contains(err.Error(), "coin") {
		t.Fatalf("Validate() error = %v, want missing coin handler", err)
	}
}

func TestRegistryValidateAcceptsFallbackOnlyIntent(t *testing.T) {
	t.Parallel()

	catalog, err := intent.NewCatalog([]intent.Entry{
		{ID: "help", Name: "도움", SlashAliases: []string{"help"}},
		{ID: "forex-convert", Name: "환율변환", NormalizeKeys: []string{"forex-convert.detect"}},
	})
	if err != nil {
		t.Fatalf("new catalog: %v", err)
	}

	registry := bot.NewRegistry(catalog)
	if err := registry.Register(registryStubHandler{name: "도움"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := registry.AddDeterministicFallback(registryDescribedFallback{
		descriptor: commandmeta.Descriptor{
			ID:            "forex-convert",
			Name:          "환율변환",
			Description:   "채팅 내 통화 자동 원화 변환",
			NormalizeKeys: []string{"forex-convert.detect"},
		},
	}); err != nil {
		t.Fatalf("add deterministic fallback: %v", err)
	}

	if err := registry.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestVisibleEntriesExcludesHelpVisibleFalse(t *testing.T) {
	t.Parallel()

	catalog, err := intent.NewCatalog([]intent.Entry{
		{ID: "coin", Name: "코인", SlashAliases: []string{"coin"}, HelpVisible: true},
		{ID: "help", Name: "도움", SlashAliases: []string{"help"}, HelpVisible: false},
		{ID: "ai", Name: "AI", SlashAliases: []string{"ai"}, HelpVisible: false},
	})
	if err != nil {
		t.Fatalf("new catalog: %v", err)
	}

	registry := bot.NewRegistry(catalog)
	for _, name := range []string{"코인", "도움", "AI"} {
		if err := registry.Register(registryStubHandler{name: name}); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}

	entries := registry.VisibleEntries("test-chat", nil)
	if len(entries) != 1 {
		t.Fatalf("VisibleEntries count = %d, want 1", len(entries))
	}
	if entries[0].ID != "coin" {
		t.Fatalf("VisibleEntries[0].ID = %q, want coin", entries[0].ID)
	}
}

func TestVisibleEntriesDefaultHelpVisibleIsHidden(t *testing.T) {
	t.Parallel()

	catalog, err := intent.NewCatalog([]intent.Entry{
		{ID: "new-cmd", Name: "새기능", SlashAliases: []string{"new"}},
	})
	if err != nil {
		t.Fatalf("new catalog: %v", err)
	}

	registry := bot.NewRegistry(catalog)
	if err := registry.Register(registryStubHandler{name: "새기능"}); err != nil {
		t.Fatalf("register: %v", err)
	}

	entries := registry.VisibleEntries("test-chat", nil)
	if len(entries) != 0 {
		t.Fatalf("VisibleEntries count = %d, want 0 (HelpVisible zero value should hide)", len(entries))
	}
}

func TestRegistryRegisterRejectsDescriptorDrift(t *testing.T) {
	t.Parallel()

	catalog, err := intent.NewCatalog([]intent.Entry{
		{
			ID:            "admin",
			Name:          "관리",
			Description:   "ACL/조회 정책 운영 관리",
			SlashAliases:  []string{"admin"},
			NormalizeKeys: []string{"admin.status", "admin.acl"},
		},
	})
	if err != nil {
		t.Fatalf("new catalog: %v", err)
	}

	registry := bot.NewRegistry(catalog)
	err = registry.Register(registryDescribedHandler{descriptor: commandmeta.Must("admin")})
	if err == nil {
		t.Fatal("expected metadata drift error")
	}
	if !strings.Contains(err.Error(), "explicit aliases") {
		t.Fatalf("register error = %v, want explicit alias drift", err)
	}
}
