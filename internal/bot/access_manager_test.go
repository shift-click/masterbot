package bot

import (
	"context"
	"errors"
	"testing"

	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/intent"
	"github.com/shift-click/masterbot/internal/store"
)

type mockACLStore struct {
	seedErr     error
	snapshotErr error
	auditErr    error

	snapshot store.ACLSnapshot
	audits   []store.ACLAuditEntry
}

func (m *mockACLStore) SeedBootstrap(_ context.Context, bootstrap store.ACLBootstrap) error {
	if m.seedErr != nil {
		return m.seedErr
	}
	if len(m.snapshot.Rooms) > 0 || len(m.snapshot.AdminRooms) > 0 || len(m.snapshot.AdminUsers) > 0 {
		return nil
	}
	for _, room := range bootstrap.Rooms {
		m.snapshot.Rooms = append(m.snapshot.Rooms, store.ACLRoom{
			ChatID:       room.ChatID,
			Alias:        room.Alias,
			AllowIntents: append([]string(nil), room.AllowIntents...),
		})
	}
	if bootstrap.AdminRoomChatID != "" {
		m.snapshot.AdminRooms = append(m.snapshot.AdminRooms, bootstrap.AdminRoomChatID)
	}
	if bootstrap.AdminUserID != "" {
		m.snapshot.AdminUsers = append(m.snapshot.AdminUsers, bootstrap.AdminUserID)
	}
	return nil
}

func (m *mockACLStore) Snapshot(context.Context) (store.ACLSnapshot, error) {
	if m.snapshotErr != nil {
		return store.ACLSnapshot{}, m.snapshotErr
	}
	return m.snapshot, nil
}

func (m *mockACLStore) UpsertRoom(_ context.Context, room store.ACLRoom) (bool, error) {
	for i, existing := range m.snapshot.Rooms {
		if existing.ChatID == room.ChatID {
			m.snapshot.Rooms[i] = room
			return true, nil
		}
	}
	m.snapshot.Rooms = append(m.snapshot.Rooms, room)
	return true, nil
}

func (m *mockACLStore) DeleteRoom(_ context.Context, chatID string) (bool, error) {
	for i, room := range m.snapshot.Rooms {
		if room.ChatID == chatID {
			m.snapshot.Rooms = append(m.snapshot.Rooms[:i], m.snapshot.Rooms[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (m *mockACLStore) UpsertRoomIntent(_ context.Context, chatID, intentID string) (bool, error) {
	for i := range m.snapshot.Rooms {
		if m.snapshot.Rooms[i].ChatID == chatID {
			for _, existing := range m.snapshot.Rooms[i].AllowIntents {
				if existing == intentID {
					return false, nil
				}
			}
			m.snapshot.Rooms[i].AllowIntents = append(m.snapshot.Rooms[i].AllowIntents, intentID)
			return true, nil
		}
	}
	return false, nil
}

func (m *mockACLStore) DeleteRoomIntent(_ context.Context, chatID, intentID string) (bool, error) {
	for i := range m.snapshot.Rooms {
		if m.snapshot.Rooms[i].ChatID != chatID {
			continue
		}
		for j, existing := range m.snapshot.Rooms[i].AllowIntents {
			if existing == intentID {
				m.snapshot.Rooms[i].AllowIntents = append(m.snapshot.Rooms[i].AllowIntents[:j], m.snapshot.Rooms[i].AllowIntents[j+1:]...)
				return true, nil
			}
		}
	}
	return false, nil
}

func (m *mockACLStore) UpsertAdminRoom(_ context.Context, chatID string) (bool, error) {
	for _, existing := range m.snapshot.AdminRooms {
		if existing == chatID {
			return false, nil
		}
	}
	m.snapshot.AdminRooms = append(m.snapshot.AdminRooms, chatID)
	return true, nil
}

func (m *mockACLStore) DeleteAdminRoom(_ context.Context, chatID string) (bool, error) {
	for i, existing := range m.snapshot.AdminRooms {
		if existing == chatID {
			m.snapshot.AdminRooms = append(m.snapshot.AdminRooms[:i], m.snapshot.AdminRooms[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (m *mockACLStore) UpsertAdminUser(_ context.Context, userID string) (bool, error) {
	for _, existing := range m.snapshot.AdminUsers {
		if existing == userID {
			return false, nil
		}
	}
	m.snapshot.AdminUsers = append(m.snapshot.AdminUsers, userID)
	return true, nil
}

func (m *mockACLStore) DeleteAdminUser(_ context.Context, userID string) (bool, error) {
	for i, existing := range m.snapshot.AdminUsers {
		if existing == userID {
			m.snapshot.AdminUsers = append(m.snapshot.AdminUsers[:i], m.snapshot.AdminUsers[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (m *mockACLStore) AppendAudit(_ context.Context, entry store.ACLAuditEntry) error {
	if m.auditErr != nil {
		return m.auditErr
	}
	m.audits = append(m.audits, entry)
	return nil
}

func (m *mockACLStore) Close() error { return nil }

func TestAccessManagerBootstrapSnapshotAndAlias(t *testing.T) {
	t.Parallel()

	mock := &mockACLStore{}
	controller := NewAccessController(intent.DefaultCatalog(), config.AccessConfig{
		DefaultPolicy: config.AccessPolicyDeny,
		Rooms: []config.AccessRoomConfig{
			{ChatID: "room-1", Alias: "기본방", AllowIntents: []string{"help.show"}},
		},
	})
	manager := NewAccessManager(controller, mock, config.AccessConfig{
		Rooms: []config.AccessRoomConfig{
			{ChatID: "room-2", Alias: "코인방", AllowIntents: []string{"coin.quote"}},
		},
		BootstrapAdminRoomChatID: "admin-room",
		BootstrapAdminUserID:     "admin-user",
	}, nil)

	if err := manager.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	snapshot := manager.Snapshot()
	if len(snapshot.Rooms) == 0 {
		t.Fatal("expected snapshot rooms after bootstrap")
	}

	room, ok := manager.FindRoomByAlias(" 코인방 ")
	if !ok || room.ChatID != "room-2" {
		t.Fatalf("FindRoomByAlias() = (%+v,%v)", room, ok)
	}
	if _, ok := manager.FindRoomByAlias(""); ok {
		t.Fatal("expected alias lookup miss for blank value")
	}

	if (*AccessManager)(nil).Snapshot().Rooms != nil {
		t.Fatal("nil manager Snapshot should be empty")
	}
	if _, ok := (*AccessManager)(nil).FindRoomByAlias("x"); ok {
		t.Fatal("nil manager FindRoomByAlias should fail")
	}
}

func TestAccessManagerMutationFlows(t *testing.T) {
	t.Parallel()

	mock := &mockACLStore{
		snapshot: store.ACLSnapshot{
			Rooms: []store.ACLRoom{{ChatID: "room-1", Alias: "방1", AllowIntents: []string{"help.show"}}},
		},
	}
	controller := NewAccessController(intent.DefaultCatalog(), config.AccessConfig{
		DefaultPolicy: config.AccessPolicyDeny,
	})
	manager := NewAccessManager(controller, mock, config.AccessConfig{}, nil)
	actor := AccessActor{ChatID: "admin-room", UserID: "admin-user"}
	ctx := context.Background()

	if changed, err := manager.UpsertRoom(ctx, actor, store.ACLRoom{ChatID: "room-2", Alias: "방2"}); err != nil || !changed {
		t.Fatalf("UpsertRoom = (%v,%v)", changed, err)
	}
	if changed, err := manager.AddRoomIntent(ctx, actor, "room-1", "coin.quote"); err != nil || !changed {
		t.Fatalf("AddRoomIntent = (%v,%v)", changed, err)
	}
	if changed, err := manager.RemoveRoomIntent(ctx, actor, "room-1", "help.show"); err != nil || !changed {
		t.Fatalf("RemoveRoomIntent = (%v,%v)", changed, err)
	}
	if changed, err := manager.AddAdminRoom(ctx, actor, "admin-room"); err != nil || !changed {
		t.Fatalf("AddAdminRoom = (%v,%v)", changed, err)
	}
	if changed, err := manager.RemoveAdminRoom(ctx, actor, "admin-room"); err != nil || !changed {
		t.Fatalf("RemoveAdminRoom = (%v,%v)", changed, err)
	}
	if changed, err := manager.AddAdminUser(ctx, actor, "admin-user"); err != nil || !changed {
		t.Fatalf("AddAdminUser = (%v,%v)", changed, err)
	}
	if changed, err := manager.RemoveAdminUser(ctx, actor, "admin-user"); err != nil || !changed {
		t.Fatalf("RemoveAdminUser = (%v,%v)", changed, err)
	}
	if changed, err := manager.DeleteRoom(ctx, actor, "room-2"); err != nil || !changed {
		t.Fatalf("DeleteRoom = (%v,%v)", changed, err)
	}

	if len(mock.audits) < 8 {
		t.Fatalf("expected audit logs for changed mutations, got %d", len(mock.audits))
	}
}

func TestAccessManagerErrorPaths(t *testing.T) {
	t.Parallel()

	controller := NewAccessController(intent.DefaultCatalog(), config.AccessConfig{})
	mock := &mockACLStore{seedErr: errors.New("seed failed")}
	manager := NewAccessManager(controller, mock, config.AccessConfig{}, nil)
	if err := manager.Bootstrap(context.Background()); err == nil {
		t.Fatal("expected bootstrap error from store")
	}

	mock = &mockACLStore{snapshotErr: errors.New("snapshot failed")}
	manager = NewAccessManager(controller, mock, config.AccessConfig{}, nil)
	if err := manager.Reload(context.Background()); err == nil {
		t.Fatal("expected reload error from store")
	}

	mock = &mockACLStore{
		snapshot: store.ACLSnapshot{
			Rooms: []store.ACLRoom{{ChatID: "room-1"}},
		},
		auditErr: errors.New("audit failed"),
	}
	manager = NewAccessManager(controller, mock, config.AccessConfig{}, nil)
	if _, err := manager.AddRoomIntent(context.Background(), AccessActor{}, "room-1", "coin.quote"); err == nil {
		t.Fatal("expected mutation error when audit append fails")
	}

	if err := (*AccessManager)(nil).Bootstrap(context.Background()); err != nil {
		t.Fatalf("nil manager bootstrap should be noop: %v", err)
	}
	if err := (*AccessManager)(nil).Reload(context.Background()); err != nil {
		t.Fatalf("nil manager reload should be noop: %v", err)
	}
}

func TestAccessManagerBootstrapEnsuresConfiguredAdminPrincipals(t *testing.T) {
	t.Parallel()

	mock := &mockACLStore{
		snapshot: store.ACLSnapshot{
			Rooms:      []store.ACLRoom{{ChatID: "room-1", Alias: "기존방", AllowIntents: []string{"help"}}},
			AdminRooms: []string{"legacy-admin-room"},
			AdminUsers: []string{"legacy-admin-user"},
		},
	}
	cfg := config.AccessConfig{
		RuntimeDBPath:            "data/access.db",
		BootstrapAdminRoomChatID: "configured-admin-room",
		BootstrapAdminUserID:     "configured-admin-user",
	}
	controller := NewAccessController(intent.DefaultCatalog(), cfg)
	manager := NewAccessManager(controller, mock, cfg, nil)

	if err := manager.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	snapshot := manager.Snapshot()
	if !testContains(snapshot.AdminRooms, "configured-admin-room") {
		t.Fatalf("expected configured bootstrap admin room in snapshot: %v", snapshot.AdminRooms)
	}
	if !testContains(snapshot.AdminUsers, "configured-admin-user") {
		t.Fatalf("expected configured bootstrap admin user in snapshot: %v", snapshot.AdminUsers)
	}
	if !testContains(snapshot.AdminRooms, "legacy-admin-room") || !testContains(snapshot.AdminUsers, "legacy-admin-user") {
		t.Fatalf("expected existing runtime admins preserved: rooms=%v users=%v", snapshot.AdminRooms, snapshot.AdminUsers)
	}

	var sawRoomEnsure, sawUserEnsure bool
	for _, audit := range mock.audits {
		switch audit.Action {
		case "bootstrap_admin_room.ensure":
			sawRoomEnsure = true
		case "bootstrap_admin_user.ensure":
			sawUserEnsure = true
		}
	}
	if !sawRoomEnsure || !sawUserEnsure {
		t.Fatalf("expected bootstrap ensure audits, got %+v", mock.audits)
	}
}

func testContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
