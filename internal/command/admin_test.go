package command

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/commandmeta"
	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/intent"
	"github.com/shift-click/masterbot/internal/store"
	"github.com/shift-click/masterbot/internal/transport"
)

// ── Auth ─────────────────────────────────────────────────────────────

func TestAdminHandlerRejectsNonAdminContext(t *testing.T) {
	t.Parallel()

	handler, cleanup := newAdminHarness(t)
	defer cleanup()

	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 현황",
		Raw: transport.RawChatLog{
			ChatID: "room-1",
			UserID: "user-1",
		},
	})
	if !strings.Contains(reply.Text, "관리자 권한이 없습니다") {
		t.Fatalf("reply = %q", reply.Text)
	}
}

// ── Register / Unregister ────────────────────────────────────────────

func TestAdminHandlerRegisterCurrentRoom(t *testing.T) {
	t.Parallel()

	handler, manager, _, cleanup := newAdminHarnessWithManager(t)
	defer cleanup()

	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 등록 테스트방",
		Raw: transport.RawChatLog{
			ChatID: "new-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "등록했습니다") {
		t.Fatalf("reply = %q", reply.Text)
	}

	room, ok := manager.FindRoomByAlias("테스트방")
	if !ok {
		t.Fatal("expected room with alias 테스트방")
	}
	if room.ChatID != "new-room" {
		t.Fatalf("chatID = %s", room.ChatID)
	}
}

func TestAdminHandlerRegisterCurrentRoomWithoutAlias(t *testing.T) {
	t.Parallel()

	handler, manager, _, cleanup := newAdminHarnessWithManager(t)
	defer cleanup()

	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 등록",
		Raw: transport.RawChatLog{
			ChatID: "new-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "등록했습니다") {
		t.Fatalf("reply = %q", reply.Text)
	}

	_, ok := findRoom(manager.Snapshot(), "new-room")
	if !ok {
		t.Fatal("expected new-room in snapshot")
	}
}

func TestAdminHandlerUnregisterCurrentRoom(t *testing.T) {
	t.Parallel()

	handler, manager, _, cleanup := newAdminHarnessWithManager(t)
	defer cleanup()

	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 해제",
		Raw: transport.RawChatLog{
			ChatID: "room-1",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "해제했습니다") {
		t.Fatalf("reply = %q", reply.Text)
	}

	_, ok := findRoom(manager.Snapshot(), "room-1")
	if ok {
		t.Fatal("expected room-1 to be removed")
	}
}

func TestAdminHandlerUnregisterByAlias(t *testing.T) {
	t.Parallel()

	handler, manager, _, cleanup := newAdminHarnessWithManager(t)
	defer cleanup()

	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 기본방 해제",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "해제했습니다") {
		t.Fatalf("reply = %q", reply.Text)
	}

	_, ok := findRoom(manager.Snapshot(), "room-1")
	if ok {
		t.Fatal("expected room-1 to be removed via alias")
	}
}

// ── Toggle intent ────────────────────────────────────────────────────

func TestAdminHandlerToggleIntentOnCurrentRoom(t *testing.T) {
	t.Parallel()

	handler, manager, _, cleanup := newAdminHarnessWithManager(t)
	defer cleanup()

	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 코인 켜기",
		Raw: transport.RawChatLog{
			ChatID: "room-1",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "코인 기능을 켰습니다") {
		t.Fatalf("reply = %q", reply.Text)
	}

	room, ok := findRoom(manager.Snapshot(), "room-1")
	if !ok {
		t.Fatal("expected room-1")
	}
	if !containsIntent(room.AllowIntents, "coin") {
		t.Fatalf("intents = %v", room.AllowIntents)
	}
}

func TestAdminHandlerToggleIntentOffCurrentRoom(t *testing.T) {
	t.Parallel()

	handler, manager, _, cleanup := newAdminHarnessWithManager(t)
	defer cleanup()

	// room-1 starts with "help" intent
	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 도움 끄기",
		Raw: transport.RawChatLog{
			ChatID: "room-1",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "도움 기능을 껐습니다") {
		t.Fatalf("reply = %q", reply.Text)
	}

	room, ok := findRoom(manager.Snapshot(), "room-1")
	if !ok {
		t.Fatal("expected room-1")
	}
	if containsIntent(room.AllowIntents, "help") {
		t.Fatalf("help should be removed, intents = %v", room.AllowIntents)
	}
}

func TestAdminHandlerToggleIntentOnRemoteRoom(t *testing.T) {
	t.Parallel()

	handler, manager, _, cleanup := newAdminHarnessWithManager(t)
	defer cleanup()

	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 기본방 코인 켜기",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "코인 기능을 켰습니다") {
		t.Fatalf("reply = %q", reply.Text)
	}

	room, ok := findRoom(manager.Snapshot(), "room-1")
	if !ok {
		t.Fatal("expected room-1")
	}
	if !containsIntent(room.AllowIntents, "coin") {
		t.Fatalf("intents = %v", room.AllowIntents)
	}
}

func TestAdminHandlerRejectsUnknownIntentName(t *testing.T) {
	t.Parallel()

	handler, cleanup := newAdminHarness(t)
	defer cleanup()

	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 알수없는기능 켜기",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	// "알수없는기능" is not a known intent or alias → error
	if !strings.Contains(reply.Text, "알 수 없는") {
		t.Fatalf("reply = %q", reply.Text)
	}
}

func TestAdminHandlerRejectsUnknownAlias(t *testing.T) {
	t.Parallel()

	handler, cleanup := newAdminHarness(t)
	defer cleanup()

	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 없는방 코인 켜기",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "알 수 없는") {
		t.Fatalf("reply = %q", reply.Text)
	}
}

// ── Toggle all ───────────────────────────────────────────────────────

func TestAdminHandlerToggleAllOn(t *testing.T) {
	t.Parallel()

	handler, manager, _, cleanup := newAdminHarnessWithManager(t)
	defer cleanup()

	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 전체 켜기",
		Raw: transport.RawChatLog{
			ChatID: "room-1",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "모든 기능을 켰습니다") {
		t.Fatalf("reply = %q", reply.Text)
	}

	room, ok := findRoom(manager.Snapshot(), "room-1")
	if !ok {
		t.Fatal("expected room-1")
	}

	toggleable := commandmeta.ToggleableIntentIDs()
	for _, id := range toggleable {
		if !containsIntent(room.AllowIntents, id) {
			t.Errorf("expected intent %s to be on", id)
		}
	}
}

func TestAdminHandlerToggleAllOff(t *testing.T) {
	t.Parallel()

	handler, manager, _, cleanup := newAdminHarnessWithManager(t)
	defer cleanup()

	// First turn all on
	executeAdmin(t, handler, transport.Message{
		Msg: "관리 전체 켜기",
		Raw: transport.RawChatLog{ChatID: "room-1", UserID: "bootstrap-user"},
	})

	// Then turn all off
	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 전체 끄기",
		Raw: transport.RawChatLog{
			ChatID: "room-1",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "모든 기능을 껐습니다") {
		t.Fatalf("reply = %q", reply.Text)
	}

	room, ok := findRoom(manager.Snapshot(), "room-1")
	if !ok {
		t.Fatal("expected room-1")
	}
	if len(room.AllowIntents) != 0 {
		t.Fatalf("expected no intents, got %v", room.AllowIntents)
	}
}

func TestAdminHandlerToggleAllExcludesNonToggleable(t *testing.T) {
	t.Parallel()

	toggleable := commandmeta.ToggleableIntentIDs()
	for _, id := range toggleable {
		if id == "admin" || id == "forex-convert" || id == "calc" {
			t.Errorf("toggleable should not include %s", id)
		}
	}
}

// ── Status / Overview ────────────────────────────────────────────────

func TestAdminHandlerStatusShowsOnOff(t *testing.T) {
	t.Parallel()

	handler, cleanup := newAdminHarness(t)
	defer cleanup()

	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 상태",
		Raw: transport.RawChatLog{
			ChatID: "room-1",
			UserID: "bootstrap-user",
		},
	})
	// room-1 has "help" intent, should show ✅ for 도움 and ❌ for others
	if !strings.Contains(reply.Text, "✅") {
		t.Fatalf("expected ✅ in output, reply = %q", reply.Text)
	}
	if !strings.Contains(reply.Text, "❌") {
		t.Fatalf("expected ❌ in output, reply = %q", reply.Text)
	}
	if !strings.Contains(reply.Text, "도움") {
		t.Fatalf("expected 도움 in output, reply = %q", reply.Text)
	}
	if !strings.Contains(reply.Text, "기본방") {
		t.Fatalf("expected alias 기본방 in output, reply = %q", reply.Text)
	}
}

func TestAdminHandlerStatusRemoteByAlias(t *testing.T) {
	t.Parallel()

	handler, cleanup := newAdminHarness(t)
	defer cleanup()

	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 기본방 상태",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "기본방") {
		t.Fatalf("expected alias 기본방 in output, reply = %q", reply.Text)
	}
	if !strings.Contains(reply.Text, "✅") {
		t.Fatalf("expected ✅ in output, reply = %q", reply.Text)
	}
}

func TestAdminHandlerOverview(t *testing.T) {
	t.Parallel()

	handler, cleanup := newAdminHarness(t)
	defer cleanup()

	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 현황",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "등록된 방") {
		t.Fatalf("expected overview header, reply = %q", reply.Text)
	}
	if !strings.Contains(reply.Text, "기본방") {
		t.Fatalf("expected alias in overview, reply = %q", reply.Text)
	}
	if !strings.Contains(reply.Text, "도움") {
		t.Fatalf("expected intent display name, reply = %q", reply.Text)
	}
}

func TestAdminHandlerStatusContainsHint(t *testing.T) {
	t.Parallel()

	handler, cleanup := newAdminHarness(t)
	defer cleanup()

	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 상태",
		Raw: transport.RawChatLog{
			ChatID: "room-1",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "💡") {
		t.Fatalf("expected hint in status output, reply = %q", reply.Text)
	}
}

// ── Alias-intent ambiguity ───────────────────────────────────────────

func TestAdminHandlerIntentTakesPriorityOverAlias(t *testing.T) {
	t.Parallel()

	handler, manager, _, cleanup := newAdminHarnessWithManager(t)
	defer cleanup()

	// Register a room with alias "코인" (same as intent display name)
	executeAdmin(t, handler, transport.Message{
		Msg: "관리 등록 코인",
		Raw: transport.RawChatLog{
			ChatID: "ambiguous-room",
			UserID: "bootstrap-user",
		},
	})

	// "관리 코인 켜기" should add coin intent to current room,
	// NOT interpret "코인" as an alias
	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 코인 켜기",
		Raw: transport.RawChatLog{
			ChatID: "room-1",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "코인 기능을 켰습니다") {
		t.Fatalf("expected intent priority, reply = %q", reply.Text)
	}

	// Verify it was added to room-1, not ambiguous-room
	room, ok := findRoom(manager.Snapshot(), "room-1")
	if !ok {
		t.Fatal("expected room-1")
	}
	if !containsIntent(room.AllowIntents, "coin") {
		t.Fatalf("intents = %v", room.AllowIntents)
	}
}

// ── Policy (unchanged functionality) ─────────────────────────────────

func TestAdminHandlerSetsAutoQueryPolicy(t *testing.T) {
	t.Parallel()

	handler, _, autoQueries, cleanup := newAdminHarnessWithManager(t)
	defer cleanup()

	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 정책 설정 room-1 local-auto",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "local-auto") {
		t.Fatalf("reply = %q", reply.Text)
	}

	policy, _, err := autoQueries.PolicyForRoom(context.Background(), "room-1")
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	if policy.Mode != bot.AutoQueryModeLocalAuto {
		t.Fatalf("policy mode = %s", policy.Mode)
	}
}

// ── Admin principal (unchanged functionality) ────────────────────────

func TestAdminHandlerRequiresBootstrapSuperAdminForPrincipalMutation(t *testing.T) {
	t.Parallel()

	handler, manager, _, cleanup := newAdminHarnessWithManager(t)
	defer cleanup()

	if _, err := manager.AddAdminUser(context.Background(), bot.AccessActor{ChatID: "admin-room", UserID: "bootstrap-user"}, "runtime-admin"); err != nil {
		t.Fatalf("add runtime admin user: %v", err)
	}

	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 관리자 사용자 추가 other-admin",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "runtime-admin",
		},
	})
	if !strings.Contains(reply.Text, "bootstrap super admin만") {
		t.Fatalf("reply = %q", reply.Text)
	}
}

func TestAdminHandlerAllowsBootstrapSuperAdminOutsideRuntimeAdminSnapshot(t *testing.T) {
	t.Parallel()

	cfg := config.AccessConfig{
		DefaultPolicy:            config.AccessPolicyDeny,
		RuntimeDBPath:            filepath.Join(t.TempDir(), "access.db"),
		BootstrapAdminRoomChatID: "admin-room",
		BootstrapAdminUserID:     "bootstrap-user",
		Rooms: []config.AccessRoomConfig{
			{ChatID: "room-1", Alias: "기본방", AllowIntents: []string{"help"}},
		},
	}
	controller := bot.NewAccessController(intent.DefaultCatalog(), cfg)
	controller.LoadRuntimeSnapshot(store.ACLSnapshot{
		Rooms:      []store.ACLRoom{{ChatID: "room-1", Alias: "기본방", AllowIntents: []string{"help"}}},
		AdminRooms: []string{"runtime-room"},
		AdminUsers: []string{"runtime-user"},
	})

	aclStore, err := store.NewSQLiteACLStore(cfg.RuntimeDBPath)
	if err != nil {
		t.Fatalf("new acl store: %v", err)
	}
	defer aclStore.Close()

	manager := bot.NewAccessManager(controller, aclStore, cfg, nil)
	catalog := intent.DefaultCatalog()
	autoQueryManager := bot.NewAutoQueryManager(catalog, bot.NewAutoQueryStore(store.NewMemoryStore()), bot.DefaultAutoQueryPolicy(catalog))
	handler := NewAdminHandler(controller, manager, autoQueryManager, func() []string {
		return []string{"help", "admin"}
	}, nil)

	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 현황",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "기본방") {
		t.Fatalf("reply = %q", reply.Text)
	}
}

// ── Usage help ────────────────────────────────────────────────────────

func TestAdminHandlerShowsUsageOnEmptyArgs(t *testing.T) {
	t.Parallel()

	handler, cleanup := newAdminHarness(t)
	defer cleanup()

	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "관리 명령") {
		t.Fatalf("expected usage text, reply = %q", reply.Text)
	}
	if !strings.Contains(reply.Text, "등록") {
		t.Fatalf("expected 등록 in usage, reply = %q", reply.Text)
	}
}

// ── Harness ──────────────────────────────────────────────────────────

func newAdminHarness(t *testing.T) (*AdminHandler, func()) {
	t.Helper()
	handler, _, _, cleanup := newAdminHarnessWithManager(t)
	return handler, cleanup
}

func newAdminHarnessWithManager(t *testing.T) (*AdminHandler, *bot.AccessManager, *bot.AutoQueryManager, func()) {
	t.Helper()

	cfg := config.AccessConfig{
		DefaultPolicy:              config.AccessPolicyDeny,
		RuntimeDBPath:              filepath.Join(t.TempDir(), "access.db"),
		BootstrapAdminRoomChatID:   "admin-room",
		BootstrapAdminUserID:       "bootstrap-user",
		BootstrapSuperAdminUsers:   []string{"bootstrap-user"},
		Rooms: []config.AccessRoomConfig{
			{ChatID: "room-1", Alias: "기본방", AllowIntents: []string{"help"}},
		},
	}
	controller := bot.NewAccessController(intent.DefaultCatalog(), cfg)
	aclStore, err := store.NewSQLiteACLStore(cfg.RuntimeDBPath)
	if err != nil {
		t.Fatalf("new acl store: %v", err)
	}
	manager := bot.NewAccessManager(controller, aclStore, cfg, nil)
	if err := manager.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap access manager: %v", err)
	}
	catalog := intent.DefaultCatalog()
	autoQueries := bot.NewAutoQueryManager(catalog, bot.NewAutoQueryStore(store.NewMemoryStore()), bot.DefaultAutoQueryPolicy(catalog))

	knownIntents := func() []string {
		return []string{"help", "coin", "stock", "admin"}
	}
	handler := NewAdminHandler(controller, manager, autoQueries, knownIntents, nil)

	cleanup := func() {
		_ = aclStore.Close()
	}
	return handler, manager, autoQueries, cleanup
}

func executeAdmin(t *testing.T, handler *AdminHandler, msg transport.Message) bot.Reply {
	t.Helper()

	var reply bot.Reply
	err := handler.Execute(context.Background(), bot.CommandContext{
		Message: msg,
		Command: handler.Name(),
		Source:  "explicit",
		Args:    strings.Fields(strings.TrimSpace(strings.TrimPrefix(msg.Msg, handler.Name()))),
		Reply: func(_ context.Context, r bot.Reply) error {
			reply = r
			return nil
		},
		Now: time.Now,
	})
	if err != nil {
		t.Fatalf("execute admin: %v", err)
	}
	return reply
}
