package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/sqliteutil"
)

type ACLRoom struct {
	ChatID       string
	Alias        string
	AllowIntents []string
}

type ACLSnapshot struct {
	Rooms      []ACLRoom
	AdminRooms []string
	AdminUsers []string
}

type ACLBootstrap struct {
	Rooms           []config.AccessRoomConfig
	AdminRoomChatID string
	AdminUserID     string
}

type ACLAuditEntry struct {
	ActorChatID string
	ActorUserID string
	Action      string
	TargetType  string
	TargetID    string
	Details     string
	CreatedAt   time.Time
}

type ACLStore interface {
	SeedBootstrap(context.Context, ACLBootstrap) error
	Snapshot(context.Context) (ACLSnapshot, error)
	UpsertRoom(context.Context, ACLRoom) (bool, error)
	DeleteRoom(context.Context, string) (bool, error)
	UpsertRoomIntent(context.Context, string, string) (bool, error)
	DeleteRoomIntent(context.Context, string, string) (bool, error)
	UpsertAdminRoom(context.Context, string) (bool, error)
	DeleteAdminRoom(context.Context, string) (bool, error)
	UpsertAdminUser(context.Context, string) (bool, error)
	DeleteAdminUser(context.Context, string) (bool, error)
	AppendAudit(context.Context, ACLAuditEntry) error
	Close() error
}

type SQLiteACLStore struct {
	sdb *sqliteutil.SQLiteDB
}

func NewSQLiteACLStore(dbPath string) (*SQLiteACLStore, error) {
	sdb, err := sqliteutil.OpenSQLiteDB(dbPath)
	if err != nil {
		return nil, err
	}

	store := &SQLiteACLStore{sdb: sdb}
	if err := store.migrate(); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("migrate acl schema: %w", err)
	}

	if err := sdb.OptimizeFull(); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("initial optimize: %w", err)
	}

	return store, nil
}

func (s *SQLiteACLStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS acl_rooms (
		chat_id     TEXT PRIMARY KEY,
		alias       TEXT DEFAULT '',
		created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS acl_room_intents (
		chat_id     TEXT NOT NULL,
		intent_id   TEXT NOT NULL,
		created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (chat_id, intent_id),
		FOREIGN KEY (chat_id) REFERENCES acl_rooms(chat_id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS acl_admin_rooms (
		chat_id     TEXT PRIMARY KEY,
		created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS acl_admin_users (
		user_id     TEXT PRIMARY KEY,
		created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS acl_audit_logs (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		actor_chat_id TEXT DEFAULT '',
		actor_user_id TEXT DEFAULT '',
		action        TEXT NOT NULL,
		target_type   TEXT NOT NULL,
		target_id     TEXT NOT NULL,
		details       TEXT DEFAULT '',
		created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_acl_audit_logs_created_at ON acl_audit_logs(created_at);
	`
	_, err := s.sdb.Write.Exec(schema)
	return err
}

func (s *SQLiteACLStore) SeedBootstrap(ctx context.Context, bootstrap ACLBootstrap) error {
	return withTx(ctx, s.sdb.Write, func(tx *sql.Tx) error {
		empty, err := aclTablesEmpty(ctx, tx)
		if err != nil {
			return err
		}
		if !empty {
			return nil
		}

		for _, room := range bootstrap.Rooms {
			if err := seedBootstrapRoom(ctx, tx, room); err != nil {
				return err
			}
		}
		return seedBootstrapPrincipals(ctx, tx, bootstrap)
	})
}

func seedBootstrapRoom(ctx context.Context, tx *sql.Tx, room config.AccessRoomConfig) error {
	chatID := strings.TrimSpace(room.ChatID)
	if chatID == "" {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO acl_rooms(chat_id, alias, updated_at)
		VALUES(?, ?, CURRENT_TIMESTAMP)
	`, chatID, strings.TrimSpace(room.Alias)); err != nil {
		return fmt.Errorf("seed room %s: %w", chatID, err)
	}
	for _, intentID := range room.AllowIntents {
		intentID = strings.TrimSpace(intentID)
		if intentID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO acl_room_intents(chat_id, intent_id)
			VALUES(?, ?)
		`, chatID, intentID); err != nil {
			return fmt.Errorf("seed room intent %s/%s: %w", chatID, intentID, err)
		}
	}
	return nil
}

func seedBootstrapPrincipals(ctx context.Context, tx *sql.Tx, bootstrap ACLBootstrap) error {
	if chatID := strings.TrimSpace(bootstrap.AdminRoomChatID); chatID != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO acl_admin_rooms(chat_id) VALUES(?)`, chatID); err != nil {
			return fmt.Errorf("seed admin room %s: %w", chatID, err)
		}
	}
	if userID := strings.TrimSpace(bootstrap.AdminUserID); userID != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO acl_admin_users(user_id) VALUES(?)`, userID); err != nil {
			return fmt.Errorf("seed admin user %s: %w", userID, err)
		}
	}
	return nil
}

func (s *SQLiteACLStore) Snapshot(ctx context.Context) (ACLSnapshot, error) {
	rooms, err := s.listRooms(ctx)
	if err != nil {
		return ACLSnapshot{}, err
	}
	adminRooms, err := s.listAdminRooms(ctx)
	if err != nil {
		return ACLSnapshot{}, err
	}
	adminUsers, err := s.listAdminUsers(ctx)
	if err != nil {
		return ACLSnapshot{}, err
	}
	return ACLSnapshot{
		Rooms:      rooms,
		AdminRooms: adminRooms,
		AdminUsers: adminUsers,
	}, nil
}

func (s *SQLiteACLStore) UpsertRoom(ctx context.Context, room ACLRoom) (bool, error) {
	chatID := strings.TrimSpace(room.ChatID)
	if chatID == "" {
		return false, nil
	}
	res, err := s.sdb.Write.ExecContext(ctx, `
		INSERT INTO acl_rooms(chat_id, alias, updated_at)
		VALUES(?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(chat_id) DO UPDATE SET
			alias = excluded.alias,
			updated_at = CURRENT_TIMESTAMP
	`, chatID, strings.TrimSpace(room.Alias))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *SQLiteACLStore) DeleteRoom(ctx context.Context, chatID string) (bool, error) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return false, nil
	}
	res, err := s.sdb.Write.ExecContext(ctx, `DELETE FROM acl_rooms WHERE chat_id = ?`, chatID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *SQLiteACLStore) UpsertRoomIntent(ctx context.Context, chatID, intentID string) (bool, error) {
	chatID = strings.TrimSpace(chatID)
	intentID = strings.TrimSpace(intentID)
	if chatID == "" || intentID == "" {
		return false, nil
	}
	res, err := s.sdb.Write.ExecContext(ctx, `
		INSERT INTO acl_room_intents(chat_id, intent_id)
		VALUES(?, ?)
		ON CONFLICT(chat_id, intent_id) DO NOTHING
	`, chatID, intentID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *SQLiteACLStore) DeleteRoomIntent(ctx context.Context, chatID, intentID string) (bool, error) {
	chatID = strings.TrimSpace(chatID)
	intentID = strings.TrimSpace(intentID)
	if chatID == "" || intentID == "" {
		return false, nil
	}
	res, err := s.sdb.Write.ExecContext(ctx, `DELETE FROM acl_room_intents WHERE chat_id = ? AND intent_id = ?`, chatID, intentID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *SQLiteACLStore) UpsertAdminRoom(ctx context.Context, chatID string) (bool, error) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return false, nil
	}
	res, err := s.sdb.Write.ExecContext(ctx, `
		INSERT INTO acl_admin_rooms(chat_id)
		VALUES(?)
		ON CONFLICT(chat_id) DO NOTHING
	`, chatID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *SQLiteACLStore) DeleteAdminRoom(ctx context.Context, chatID string) (bool, error) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return false, nil
	}
	res, err := s.sdb.Write.ExecContext(ctx, `DELETE FROM acl_admin_rooms WHERE chat_id = ?`, chatID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *SQLiteACLStore) UpsertAdminUser(ctx context.Context, userID string) (bool, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false, nil
	}
	res, err := s.sdb.Write.ExecContext(ctx, `
		INSERT INTO acl_admin_users(user_id)
		VALUES(?)
		ON CONFLICT(user_id) DO NOTHING
	`, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *SQLiteACLStore) DeleteAdminUser(ctx context.Context, userID string) (bool, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false, nil
	}
	res, err := s.sdb.Write.ExecContext(ctx, `DELETE FROM acl_admin_users WHERE user_id = ?`, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *SQLiteACLStore) AppendAudit(ctx context.Context, entry ACLAuditEntry) error {
	createdAt := entry.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	_, err := s.sdb.Write.ExecContext(ctx, `
		INSERT INTO acl_audit_logs(actor_chat_id, actor_user_id, action, target_type, target_id, details, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)
	`,
		strings.TrimSpace(entry.ActorChatID),
		strings.TrimSpace(entry.ActorUserID),
		strings.TrimSpace(entry.Action),
		strings.TrimSpace(entry.TargetType),
		strings.TrimSpace(entry.TargetID),
		strings.TrimSpace(entry.Details),
		createdAt,
	)
	return err
}

func (s *SQLiteACLStore) Close() error {
	if s == nil || s.sdb == nil {
		return nil
	}
	_ = s.sdb.Optimize()
	return s.sdb.Close()
}

func (s *SQLiteACLStore) listRooms(ctx context.Context) ([]ACLRoom, error) {
	rows, err := s.sdb.Read.QueryContext(ctx, `
		SELECT chat_id, alias
		FROM acl_rooms
		ORDER BY chat_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rooms := make([]ACLRoom, 0)
	roomIndexByChatID := make(map[string]int)
	for rows.Next() {
		var room ACLRoom
		if err := rows.Scan(&room.ChatID, &room.Alias); err != nil {
			return nil, err
		}
		rooms = append(rooms, room)
		roomIndexByChatID[room.ChatID] = len(rooms) - 1
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	intentRows, err := s.sdb.Read.QueryContext(ctx, `
		SELECT chat_id, intent_id
		FROM acl_room_intents
		ORDER BY chat_id, intent_id
	`)
	if err != nil {
		return nil, err
	}
	defer intentRows.Close()

	for intentRows.Next() {
		var chatID, intentID string
		if err := intentRows.Scan(&chatID, &intentID); err != nil {
			return nil, err
		}
		index, ok := roomIndexByChatID[chatID]
		if !ok {
			continue
		}
		rooms[index].AllowIntents = append(rooms[index].AllowIntents, intentID)
	}
	if err := intentRows.Err(); err != nil {
		return nil, err
	}
	return rooms, nil
}

func (s *SQLiteACLStore) listAdminRooms(ctx context.Context) ([]string, error) {
	return listSingleColumn(ctx, s.sdb.Read, `SELECT chat_id FROM acl_admin_rooms ORDER BY chat_id`)
}

func (s *SQLiteACLStore) listAdminUsers(ctx context.Context) ([]string, error) {
	return listSingleColumn(ctx, s.sdb.Read, `SELECT user_id FROM acl_admin_users ORDER BY user_id`)
}

func listSingleColumn(ctx context.Context, db *sql.DB, query string) ([]string, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(values)
	return values, nil
}

func aclTablesEmpty(ctx context.Context, tx *sql.Tx) (bool, error) {
	var total int
	for _, table := range []string{"acl_rooms", "acl_admin_rooms", "acl_admin_users"} {
		var count int
		row := tx.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(1) FROM %s", table))
		if err := row.Scan(&count); err != nil {
			return false, err
		}
		total += count
	}
	return total == 0, nil
}

func withTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
