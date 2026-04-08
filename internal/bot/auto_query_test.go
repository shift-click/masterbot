package bot_test

import (
	"context"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/intent"
	"github.com/shift-click/masterbot/internal/store"
	"github.com/shift-click/masterbot/internal/transport"
)

func TestAutoQueryManager_DegradesRoomWhenBudgetExceeded(t *testing.T) {
	t.Parallel()

	catalog := intent.DefaultCatalog()
	policyStore := bot.NewAutoQueryStore(store.NewMemoryStore())
	manager := bot.NewAutoQueryManager(catalog, policyStore, bot.DefaultAutoQueryPolicy(catalog))

	if err := manager.SetRoomPolicy(context.Background(), "room-1", bot.AutoQueryPolicy{
		Mode:              bot.AutoQueryModeLocalAuto,
		AllowedHandlers:   []string{"coin"},
		BudgetPerHour:     1,
		CooldownWindow:    time.Millisecond,
		DegradationTarget: bot.AutoQueryModeExplicitOnly,
	}); err != nil {
		t.Fatalf("set room policy: %v", err)
	}

	msg := transport.Message{Raw: transport.RawChatLog{ChatID: "room-1"}}

	allowed, err := manager.AllowAutomatic(context.Background(), msg, "비트", []string{"coin"})
	if err != nil {
		t.Fatalf("first allow: %v", err)
	}
	if !allowed {
		t.Fatal("expected first request to be allowed")
	}

	allowed, err = manager.AllowAutomatic(context.Background(), msg, "이더", []string{"coin"})
	if err != nil {
		t.Fatalf("second allow: %v", err)
	}
	if allowed {
		t.Fatal("expected second request to be denied after budget exhaustion")
	}

	policy, err := manager.Policy(context.Background(), msg)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	if policy.Mode != bot.AutoQueryModeExplicitOnly {
		t.Fatalf("policy mode = %s, want explicit-only", policy.Mode)
	}
}

func TestAutoQueryManager_BootstrapSeedsMissingPolicyOnly(t *testing.T) {
	t.Parallel()

	catalog := intent.DefaultCatalog()
	policyStore := bot.NewAutoQueryStore(store.NewMemoryStore())
	manager := bot.NewAutoQueryManager(catalog, policyStore, bot.DefaultAutoQueryPolicy(catalog))

	if err := manager.Bootstrap(context.Background(), []bot.AutoQueryBootstrapRoom{
		{
			ChatID: "room-1",
			Policy: bot.AutoQueryPolicy{Mode: bot.AutoQueryModeLocalAuto},
		},
	}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	policy, stored, err := manager.PolicyForRoom(context.Background(), "room-1")
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	if !stored {
		t.Fatal("expected room policy to be stored")
	}
	if policy.Mode != bot.AutoQueryModeLocalAuto {
		t.Fatalf("policy mode = %s", policy.Mode)
	}

	if _, err := manager.UpdateRoomPolicy(context.Background(), bot.AutoQueryActor{ChatID: "admin", UserID: "u1"}, "room-1", bot.AutoQueryPolicy{
		Mode: bot.AutoQueryModeExplicitOnly,
	}); err != nil {
		t.Fatalf("update room policy: %v", err)
	}

	if err := manager.Bootstrap(context.Background(), []bot.AutoQueryBootstrapRoom{
		{
			ChatID: "room-1",
			Policy: bot.AutoQueryPolicy{Mode: bot.AutoQueryModeLocalAuto},
		},
	}); err != nil {
		t.Fatalf("bootstrap second: %v", err)
	}

	policy, stored, err = manager.PolicyForRoom(context.Background(), "room-1")
	if err != nil {
		t.Fatalf("policy second: %v", err)
	}
	if !stored {
		t.Fatal("expected room policy to remain stored")
	}
	if policy.Mode != bot.AutoQueryModeExplicitOnly {
		t.Fatalf("policy mode after override = %s, want explicit-only", policy.Mode)
	}
}
