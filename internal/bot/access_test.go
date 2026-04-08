package bot

import (
	"testing"

	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/intent"
	"github.com/shift-click/masterbot/internal/store"
	"github.com/shift-click/masterbot/internal/transport"
)

func TestAccessControllerRuntimeReloadAndAdminAuthentication(t *testing.T) {
	t.Parallel()

	controller := NewAccessController(intent.DefaultCatalog(), config.AccessConfig{
		DefaultPolicy:            config.AccessPolicyDeny,
		RuntimeDBPath:            "data/access.db",
		BootstrapAdminRoomChatID: "bootstrap-room",
		BootstrapAdminUserID:     "bootstrap-user",
		BootstrapSuperAdminUsers: []string{"global-super-admin"},
		Rooms: []config.AccessRoomConfig{
			{ChatID: "room-1", AllowIntents: []string{"help.show"}},
		},
	})

	if !controller.IsAllowed("room-1", "help.show") {
		t.Fatal("expected bootstrap room to allow help.show")
	}

	controller.LoadRuntimeSnapshot(store.ACLSnapshot{
		Rooms: []store.ACLRoom{
			{ChatID: "room-2", Alias: "주식방", AllowIntents: []string{"stock.quote"}},
		},
		AdminRooms: []string{"admin-room"},
		AdminUsers: []string{"admin-user"},
	})

	if controller.IsAllowed("room-1", "help.show") {
		t.Fatal("expected old bootstrap snapshot to be replaced after runtime reload")
	}
	if !controller.IsAllowed("room-2", "stock.quote") {
		t.Fatal("expected runtime snapshot to allow stock.quote")
	}
	adminMsg := transport.Message{Raw: transport.RawChatLog{ChatID: "admin-room", UserID: "admin-user"}}
	if !controller.IsRuntimeAdmin(adminMsg) {
		t.Fatal("expected admin-room/admin-user to be runtime admin")
	}
	if !controller.CanExecute(adminMsg, AdminACLIntentID) {
		t.Fatal("expected runtime admin to execute admin intent")
	}
	if controller.IsRuntimeAdmin(transport.Message{Raw: transport.RawChatLog{ChatID: "admin-room", UserID: "other-user"}}) {
		t.Fatal("expected mismatched user to be denied")
	}
	if !controller.IsBootstrapSuperAdmin(transport.Message{Raw: transport.RawChatLog{ChatID: "bootstrap-room", UserID: "bootstrap-user"}}) {
		t.Fatal("expected bootstrap super admin to match configured identity")
	}
	if !controller.IsBootstrapSuperAdmin(transport.Message{Raw: transport.RawChatLog{ChatID: "random-room", UserID: "global-super-admin"}}) {
		t.Fatal("expected global super admin user to match regardless of room")
	}
	controller.LoadRuntimeSnapshot(store.ACLSnapshot{
		Rooms:      []store.ACLRoom{{ChatID: "room-2", Alias: "주식방", AllowIntents: []string{"stock.quote"}}},
		AdminRooms: []string{"other-admin-room"},
		AdminUsers: []string{"other-admin-user"},
	})
	if !controller.CanExecute(transport.Message{Raw: transport.RawChatLog{ChatID: "bootstrap-room", UserID: "bootstrap-user"}}, AdminACLIntentID) {
		t.Fatal("expected bootstrap super admin to retain admin intent access even when runtime snapshot differs")
	}
}
