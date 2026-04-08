package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/shift-click/masterbot/internal/config"
)

func TestSQLiteACLStoreSeedsBootstrapAndLoadsSnapshot(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "access.db")
	aclStore, err := NewSQLiteACLStore(dbPath)
	if err != nil {
		t.Fatalf("new acl store: %v", err)
	}
	t.Cleanup(func() { _ = aclStore.Close() })

	ctx := context.Background()
	err = aclStore.SeedBootstrap(ctx, ACLBootstrap{
		Rooms: []config.AccessRoomConfig{
			{ChatID: "room-1", Alias: "코인방", AllowIntents: []string{"help.show", "coin.quote"}},
		},
		AdminRoomChatID: "admin-room",
		AdminUserID:     "admin-user",
	})
	if err != nil {
		t.Fatalf("seed bootstrap: %v", err)
	}

	snapshot, err := aclStore.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snapshot.Rooms) != 1 {
		t.Fatalf("rooms = %d, want 1", len(snapshot.Rooms))
	}
	if snapshot.Rooms[0].ChatID != "room-1" {
		t.Fatalf("room chat_id = %q", snapshot.Rooms[0].ChatID)
	}
	if len(snapshot.Rooms[0].AllowIntents) != 2 {
		t.Fatalf("room intents = %v", snapshot.Rooms[0].AllowIntents)
	}
	if len(snapshot.AdminRooms) != 1 || snapshot.AdminRooms[0] != "admin-room" {
		t.Fatalf("admin rooms = %v", snapshot.AdminRooms)
	}
	if len(snapshot.AdminUsers) != 1 || snapshot.AdminUsers[0] != "admin-user" {
		t.Fatalf("admin users = %v", snapshot.AdminUsers)
	}
}

func TestSQLiteACLStorePersistsMutationsAndAuditLogs(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "access.db")
	aclStore, err := NewSQLiteACLStore(dbPath)
	if err != nil {
		t.Fatalf("new acl store: %v", err)
	}

	ctx := context.Background()
	if _, err := aclStore.UpsertRoom(ctx, ACLRoom{ChatID: "room-1", Alias: "테스트"}); err != nil {
		t.Fatalf("upsert room: %v", err)
	}
	if _, err := aclStore.UpsertRoomIntent(ctx, "room-1", "coin.quote"); err != nil {
		t.Fatalf("upsert room intent: %v", err)
	}
	if _, err := aclStore.UpsertAdminRoom(ctx, "admin-room"); err != nil {
		t.Fatalf("upsert admin room: %v", err)
	}
	if _, err := aclStore.UpsertAdminUser(ctx, "admin-user"); err != nil {
		t.Fatalf("upsert admin user: %v", err)
	}
	if err := aclStore.AppendAudit(ctx, ACLAuditEntry{
		ActorChatID: "admin-room",
		ActorUserID: "admin-user",
		Action:      "room_intent.add",
		TargetType:  "room_intent",
		TargetID:    "room-1:coin.quote",
	}); err != nil {
		t.Fatalf("append audit: %v", err)
	}
	if err := aclStore.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened, err := NewSQLiteACLStore(dbPath)
	if err != nil {
		t.Fatalf("reopen acl store: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	snapshot, err := reopened.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot after reopen: %v", err)
	}
	if len(snapshot.Rooms) != 1 || len(snapshot.Rooms[0].AllowIntents) != 1 || snapshot.Rooms[0].AllowIntents[0] != "coin.quote" {
		t.Fatalf("snapshot rooms after reopen = %#v", snapshot.Rooms)
	}
	var auditCount int
	if err := reopened.sdb.Read.QueryRowContext(ctx, `SELECT COUNT(1) FROM acl_audit_logs`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("audit count = %d, want 1", auditCount)
	}
}

func TestSQLiteACLStoreSnapshotKeepsIntentsForAllRooms(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "access.db")
	aclStore, err := NewSQLiteACLStore(dbPath)
	if err != nil {
		t.Fatalf("new acl store: %v", err)
	}
	t.Cleanup(func() { _ = aclStore.Close() })

	ctx := context.Background()
	rooms := []ACLRoom{
		{ChatID: "18478040786186624", Alias: "오픈채팅"},
		{ChatID: "451548784675371", Alias: "직접방"},
		{ChatID: "452142492304127", Alias: "그룹방"},
	}
	for _, room := range rooms {
		if _, err := aclStore.UpsertRoom(ctx, room); err != nil {
			t.Fatalf("upsert room %s: %v", room.ChatID, err)
		}
	}
	for _, intentID := range []string{"coin.quote", "coupang.track", "help.show", "stock.quote"} {
		if _, err := aclStore.UpsertRoomIntent(ctx, "18478040786186624", intentID); err != nil {
			t.Fatalf("upsert openchat intent %s: %v", intentID, err)
		}
		if _, err := aclStore.UpsertRoomIntent(ctx, "451548784675371", intentID); err != nil {
			t.Fatalf("upsert direct intent %s: %v", intentID, err)
		}
		if _, err := aclStore.UpsertRoomIntent(ctx, "452142492304127", intentID); err != nil {
			t.Fatalf("upsert group intent %s: %v", intentID, err)
		}
	}

	snapshot, err := aclStore.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snapshot.Rooms) != 3 {
		t.Fatalf("rooms = %d, want 3", len(snapshot.Rooms))
	}
	for _, room := range snapshot.Rooms {
		if len(room.AllowIntents) != 4 {
			t.Fatalf("room %s intents = %v, want 4 intents", room.ChatID, room.AllowIntents)
		}
	}
}

func TestSQLiteACLStoreDeleteAndTxRollbackPaths(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "access-delete.db")
	aclStore, err := NewSQLiteACLStore(dbPath)
	if err != nil {
		t.Fatalf("new acl store: %v", err)
	}
	t.Cleanup(func() { _ = aclStore.Close() })

	ctx := context.Background()
	if _, err := aclStore.UpsertRoom(ctx, ACLRoom{ChatID: "room-1", Alias: "테스트"}); err != nil {
		t.Fatalf("upsert room: %v", err)
	}
	if _, err := aclStore.UpsertRoomIntent(ctx, "room-1", "coin.quote"); err != nil {
		t.Fatalf("upsert room intent: %v", err)
	}
	if _, err := aclStore.UpsertAdminRoom(ctx, "admin-room"); err != nil {
		t.Fatalf("upsert admin room: %v", err)
	}
	if _, err := aclStore.UpsertAdminUser(ctx, "admin-user"); err != nil {
		t.Fatalf("upsert admin user: %v", err)
	}

	if changed, err := aclStore.DeleteRoomIntent(ctx, "room-1", "coin.quote"); err != nil || !changed {
		t.Fatalf("delete room intent = (%v,%v), want changed", changed, err)
	}
	if changed, err := aclStore.DeleteAdminRoom(ctx, "admin-room"); err != nil || !changed {
		t.Fatalf("delete admin room = (%v,%v), want changed", changed, err)
	}
	if changed, err := aclStore.DeleteAdminUser(ctx, "admin-user"); err != nil || !changed {
		t.Fatalf("delete admin user = (%v,%v), want changed", changed, err)
	}
	if changed, err := aclStore.DeleteRoom(ctx, "room-1"); err != nil || !changed {
		t.Fatalf("delete room = (%v,%v), want changed", changed, err)
	}

	if changed, err := aclStore.DeleteRoomIntent(ctx, "room-1", "coin.quote"); err != nil || changed {
		t.Fatalf("second delete room intent = (%v,%v), want not changed", changed, err)
	}
	if changed, err := aclStore.DeleteAdminRoom(ctx, "admin-room"); err != nil || changed {
		t.Fatalf("second delete admin room = (%v,%v), want not changed", changed, err)
	}
	if changed, err := aclStore.DeleteAdminUser(ctx, "admin-user"); err != nil || changed {
		t.Fatalf("second delete admin user = (%v,%v), want not changed", changed, err)
	}
	if changed, err := aclStore.DeleteRoom(ctx, "room-1"); err != nil || changed {
		t.Fatalf("second delete room = (%v,%v), want not changed", changed, err)
	}

	if err := withTx(ctx, aclStore.sdb.Write, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO acl_rooms(chat_id, alias) VALUES(?, ?)`, "tx-room", "tx"); err != nil {
			return err
		}
		return errors.New("rollback this tx")
	}); err == nil {
		t.Fatal("expected withTx rollback error")
	}
	if snapshot, err := aclStore.Snapshot(ctx); err != nil {
		t.Fatalf("snapshot after rollback: %v", err)
	} else if len(snapshot.Rooms) != 0 {
		t.Fatalf("expected rollback to keep rooms empty, got %v", snapshot.Rooms)
	}
}
