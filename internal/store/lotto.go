package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/shift-click/masterbot/internal/lotto"
	"github.com/shift-click/masterbot/internal/sqliteutil"
)

type SQLiteLottoStore struct {
	sdb *sqliteutil.SQLiteDB
}

func NewSQLiteLottoStore(dbPath string) (*SQLiteLottoStore, error) {
	sdb, err := sqliteutil.OpenSQLiteDB(dbPath)
	if err != nil {
		return nil, err
	}

	store := &SQLiteLottoStore{sdb: sdb}
	if err := store.migrate(); err != nil {
		_ = sdb.Close()
		return nil, fmt.Errorf("migrate lotto schema: %w", err)
	}
	if err := sdb.OptimizeFull(); err != nil {
		_ = sdb.Close()
		return nil, fmt.Errorf("initial optimize: %w", err)
	}
	return store, nil
}

func (s *SQLiteLottoStore) Close() error {
	if s == nil || s.sdb == nil {
		return nil
	}
	_ = s.sdb.Optimize()
	return s.sdb.Close()
}

func (s *SQLiteLottoStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS lotto_draws (
		round           INTEGER PRIMARY KEY,
		draw_date       DATETIME NOT NULL,
		num1            INTEGER NOT NULL,
		num2            INTEGER NOT NULL,
		num3            INTEGER NOT NULL,
		num4            INTEGER NOT NULL,
		num5            INTEGER NOT NULL,
		num6            INTEGER NOT NULL,
		bonus_number    INTEGER NOT NULL,
		rank1_winners   INTEGER DEFAULT 0,
		rank1_prize     INTEGER DEFAULT 0,
		rank2_winners   INTEGER DEFAULT 0,
		rank2_prize     INTEGER DEFAULT 0,
		rank3_winners   INTEGER DEFAULT 0,
		rank3_prize     INTEGER DEFAULT 0,
		rank4_winners   INTEGER DEFAULT 0,
		rank4_prize     INTEGER DEFAULT 0,
		rank5_winners   INTEGER DEFAULT 0,
		rank5_prize     INTEGER DEFAULT 0,
		total_winners   INTEGER DEFAULT 0,
		total_sales     INTEGER DEFAULT 0,
		create_time     DATETIME DEFAULT CURRENT_TIMESTAMP,
		update_time     DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_lotto_draws_draw_date ON lotto_draws(draw_date);

	CREATE TABLE IF NOT EXISTS lotto_draw_first_prize_shops (
		id               INTEGER PRIMARY KEY AUTOINCREMENT,
		round            INTEGER NOT NULL,
		shop_id          TEXT DEFAULT '',
		shop_name        TEXT DEFAULT '',
		region           TEXT DEFAULT '',
		district         TEXT DEFAULT '',
		address1         TEXT DEFAULT '',
		address2         TEXT DEFAULT '',
		address3         TEXT DEFAULT '',
		address4         TEXT DEFAULT '',
		full_address     TEXT DEFAULT '',
		win_method_code  TEXT DEFAULT '',
		win_method_text  TEXT DEFAULT '',
		latitude         REAL DEFAULT 0,
		longitude        REAL DEFAULT 0,
		create_time      DATETIME DEFAULT CURRENT_TIMESTAMP,
		update_time      DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (round) REFERENCES lotto_draws(round) ON DELETE CASCADE
	);
	CREATE INDEX IF NOT EXISTS idx_lotto_shops_round ON lotto_draw_first_prize_shops(round);

	CREATE TABLE IF NOT EXISTS lotto_tickets (
		id               INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id          TEXT NOT NULL,
		round            INTEGER NOT NULL,
		line_no          INTEGER NOT NULL,
		num1             INTEGER NOT NULL,
		num2             INTEGER NOT NULL,
		num3             INTEGER NOT NULL,
		num4             INTEGER NOT NULL,
		num5             INTEGER NOT NULL,
		num6             INTEGER NOT NULL,
		source           TEXT NOT NULL,
		status           TEXT NOT NULL DEFAULT 'active',
		create_time      DATETIME DEFAULT CURRENT_TIMESTAMP,
		update_time      DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_lotto_tickets_user_round_line ON lotto_tickets(user_id, round, line_no);
	CREATE INDEX IF NOT EXISTS idx_lotto_tickets_user_round_status ON lotto_tickets(user_id, round, status);
	CREATE INDEX IF NOT EXISTS idx_lotto_tickets_round_status ON lotto_tickets(round, status);

	CREATE TABLE IF NOT EXISTS lotto_user_room_profiles (
		user_id          TEXT NOT NULL,
		chat_id          TEXT NOT NULL,
		sender_name      TEXT NOT NULL,
		last_seen_at     DATETIME NOT NULL,
		create_time      DATETIME DEFAULT CURRENT_TIMESTAMP,
		update_time      DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (user_id, chat_id)
	);
	CREATE INDEX IF NOT EXISTS idx_lotto_profiles_chat_user ON lotto_user_room_profiles(chat_id, user_id);
	`
	_, err := s.sdb.Write.Exec(schema)
	return err
}

func (s *SQLiteLottoStore) UpsertDraw(ctx context.Context, draw lotto.Draw) error {
	_, err := s.sdb.Write.ExecContext(ctx, `
		INSERT INTO lotto_draws (
			round, draw_date, num1, num2, num3, num4, num5, num6, bonus_number,
			rank1_winners, rank1_prize, rank2_winners, rank2_prize, rank3_winners, rank3_prize,
			rank4_winners, rank4_prize, rank5_winners, rank5_prize, total_winners, total_sales
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(round) DO UPDATE SET
			draw_date = excluded.draw_date,
			num1 = excluded.num1,
			num2 = excluded.num2,
			num3 = excluded.num3,
			num4 = excluded.num4,
			num5 = excluded.num5,
			num6 = excluded.num6,
			bonus_number = excluded.bonus_number,
			rank1_winners = excluded.rank1_winners,
			rank1_prize = excluded.rank1_prize,
			rank2_winners = excluded.rank2_winners,
			rank2_prize = excluded.rank2_prize,
			rank3_winners = excluded.rank3_winners,
			rank3_prize = excluded.rank3_prize,
			rank4_winners = excluded.rank4_winners,
			rank4_prize = excluded.rank4_prize,
			rank5_winners = excluded.rank5_winners,
			rank5_prize = excluded.rank5_prize,
			total_winners = excluded.total_winners,
			total_sales = excluded.total_sales,
			update_time = CURRENT_TIMESTAMP
	`,
		draw.Round,
		draw.DrawDate,
		draw.Numbers[0],
		draw.Numbers[1],
		draw.Numbers[2],
		draw.Numbers[3],
		draw.Numbers[4],
		draw.Numbers[5],
		draw.BonusNumber,
		draw.Rank1Winners,
		draw.Rank1Prize,
		draw.Rank2Winners,
		draw.Rank2Prize,
		draw.Rank3Winners,
		draw.Rank3Prize,
		draw.Rank4Winners,
		draw.Rank4Prize,
		draw.Rank5Winners,
		draw.Rank5Prize,
		draw.TotalWinners,
		draw.TotalSales,
	)
	return err
}

func (s *SQLiteLottoStore) TouchDrawUpdateTime(ctx context.Context, round int, at time.Time) error {
	_, err := s.sdb.Write.ExecContext(ctx, `
		UPDATE lotto_draws
		SET update_time = ?
		WHERE round = ?
	`, at, round)
	return err
}

func (s *SQLiteLottoStore) LatestDraw(ctx context.Context) (*lotto.Draw, error) {
	row := s.sdb.Read.QueryRowContext(ctx, `
		SELECT round, draw_date, num1, num2, num3, num4, num5, num6, bonus_number,
		       rank1_winners, rank1_prize, rank2_winners, rank2_prize, rank3_winners, rank3_prize,
		       rank4_winners, rank4_prize, rank5_winners, rank5_prize, total_winners, total_sales,
		       create_time, update_time
		FROM lotto_draws
		ORDER BY round DESC
		LIMIT 1
	`)
	draw, err := scanLottoDraw(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &draw, nil
}

func (s *SQLiteLottoStore) GetDraw(ctx context.Context, round int) (*lotto.Draw, error) {
	row := s.sdb.Read.QueryRowContext(ctx, `
		SELECT round, draw_date, num1, num2, num3, num4, num5, num6, bonus_number,
		       rank1_winners, rank1_prize, rank2_winners, rank2_prize, rank3_winners, rank3_prize,
		       rank4_winners, rank4_prize, rank5_winners, rank5_prize, total_winners, total_sales,
		       create_time, update_time
		FROM lotto_draws
		WHERE round = ?
	`, round)
	draw, err := scanLottoDraw(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &draw, nil
}

func (s *SQLiteLottoStore) ReplaceFirstPrizeShops(ctx context.Context, round int, shops []lotto.FirstPrizeShop) error {
	return withLottoTx(ctx, s.sdb.Write, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM lotto_draw_first_prize_shops WHERE round = ?`, round); err != nil {
			return err
		}
		if len(shops) == 0 {
			return nil
		}
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO lotto_draw_first_prize_shops (
				round, shop_id, shop_name, region, district, address1, address2, address3, address4,
				full_address, win_method_code, win_method_text, latitude, longitude
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, shop := range shops {
			if _, err := stmt.ExecContext(ctx,
				round,
				shop.ShopID,
				shop.ShopName,
				shop.Region,
				shop.District,
				shop.Address1,
				shop.Address2,
				shop.Address3,
				shop.Address4,
				shop.FullAddress,
				shop.WinMethodCode,
				shop.WinMethodText,
				shop.Latitude,
				shop.Longitude,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SQLiteLottoStore) ListFirstPrizeShops(ctx context.Context, round int) ([]lotto.FirstPrizeShop, error) {
	rows, err := s.sdb.Read.QueryContext(ctx, `
		SELECT round, shop_id, shop_name, region, district, address1, address2, address3, address4,
		       full_address, win_method_code, win_method_text, latitude, longitude, create_time, update_time
		FROM lotto_draw_first_prize_shops
		WHERE round = ?
		ORDER BY region ASC, district ASC, shop_name ASC
	`, round)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLottoShops(rows)
}

func (s *SQLiteLottoStore) UpsertUserRoomProfile(ctx context.Context, profile lotto.UserRoomProfile) error {
	_, err := s.sdb.Write.ExecContext(ctx, `
		INSERT INTO lotto_user_room_profiles (user_id, chat_id, sender_name, last_seen_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id, chat_id) DO UPDATE SET
			sender_name = excluded.sender_name,
			last_seen_at = excluded.last_seen_at,
			update_time = CURRENT_TIMESTAMP
	`, strings.TrimSpace(profile.UserID), strings.TrimSpace(profile.ChatID), strings.TrimSpace(profile.SenderName), profile.LastSeenAt)
	return err
}

func (s *SQLiteLottoStore) GetUserRoomProfile(ctx context.Context, userID, chatID string) (*lotto.UserRoomProfile, error) {
	row := s.sdb.Read.QueryRowContext(ctx, `
		SELECT user_id, chat_id, sender_name, last_seen_at, create_time, update_time
		FROM lotto_user_room_profiles
		WHERE user_id = ? AND chat_id = ?
	`, strings.TrimSpace(userID), strings.TrimSpace(chatID))
	var profile lotto.UserRoomProfile
	err := row.Scan(
		&profile.UserID,
		&profile.ChatID,
		&profile.SenderName,
		&profile.LastSeenAt,
		&profile.CreateTime,
		&profile.UpdateTime,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func (s *SQLiteLottoStore) NextLineNo(ctx context.Context, userID string, round int) (int, error) {
	var maxLineNo int
	err := s.sdb.Read.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(line_no), 0)
		FROM lotto_tickets
		WHERE user_id = ? AND round = ?
	`, strings.TrimSpace(userID), round).Scan(&maxLineNo)
	if err != nil {
		return 0, err
	}
	return maxLineNo + 1, nil
}

func (s *SQLiteLottoStore) InsertTickets(ctx context.Context, tickets []lotto.TicketLine) error {
	if len(tickets) == 0 {
		return nil
	}
	return withLottoTx(ctx, s.sdb.Write, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO lotto_tickets (
				user_id, round, line_no, num1, num2, num3, num4, num5, num6, source, status
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, ticket := range tickets {
			if _, err := stmt.ExecContext(
				ctx,
				strings.TrimSpace(ticket.UserID),
				ticket.Round,
				ticket.LineNo,
				ticket.Numbers[0],
				ticket.Numbers[1],
				ticket.Numbers[2],
				ticket.Numbers[3],
				ticket.Numbers[4],
				ticket.Numbers[5],
				string(ticket.Source),
				string(ticket.Status),
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SQLiteLottoStore) ListUserTickets(ctx context.Context, userID string, round int, activeOnly bool) ([]lotto.TicketLine, error) {
	query := `
		SELECT id, user_id, round, line_no, num1, num2, num3, num4, num5, num6, source, status, create_time, update_time
		FROM lotto_tickets
		WHERE user_id = ? AND round = ?
	`
	args := []any{strings.TrimSpace(userID), round}
	if activeOnly {
		query += ` AND status = ?`
		args = append(args, string(lotto.TicketStatusActive))
	}
	query += ` ORDER BY line_no ASC`
	rows, err := s.sdb.Read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTicketLines(rows)
}

func (s *SQLiteLottoStore) ListRoundTickets(ctx context.Context, round int, activeOnly bool) ([]lotto.TicketLine, error) {
	query := `
		SELECT id, user_id, round, line_no, num1, num2, num3, num4, num5, num6, source, status, create_time, update_time
		FROM lotto_tickets
		WHERE round = ?
	`
	args := []any{round}
	if activeOnly {
		query += ` AND status = ?`
		args = append(args, string(lotto.TicketStatusActive))
	}
	query += ` ORDER BY user_id ASC, line_no ASC`
	rows, err := s.sdb.Read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTicketLines(rows)
}

func (s *SQLiteLottoStore) DeactivateAllUserTickets(ctx context.Context, userID string, round int) ([]lotto.TicketLine, error) {
	tickets, err := s.ListUserTickets(ctx, userID, round, true)
	if err != nil {
		return nil, err
	}
	if len(tickets) == 0 {
		return nil, nil
	}
	if _, err := s.sdb.Write.ExecContext(ctx, `
		UPDATE lotto_tickets
		SET status = ?, update_time = CURRENT_TIMESTAMP
		WHERE user_id = ? AND round = ? AND status = ?
	`, string(lotto.TicketStatusInactive), strings.TrimSpace(userID), round, string(lotto.TicketStatusActive)); err != nil {
		return nil, err
	}
	for i := range tickets {
		tickets[i].Status = lotto.TicketStatusInactive
	}
	return tickets, nil
}

func (s *SQLiteLottoStore) DeactivateTicketLines(ctx context.Context, userID string, round int, lineNos []int) ([]lotto.TicketLine, error) {
	lineNos = uniqueSortedInts(lineNos)
	if len(lineNos) == 0 {
		return nil, nil
	}
	args := []any{strings.TrimSpace(userID), round}
	placeholders := make([]string, 0, len(lineNos))
	for _, lineNo := range lineNos {
		placeholders = append(placeholders, "?")
		args = append(args, lineNo)
	}
	query := `
		SELECT id, user_id, round, line_no, num1, num2, num3, num4, num5, num6, source, status, create_time, update_time
		FROM lotto_tickets
		WHERE user_id = ? AND round = ? AND status = ?
		  AND line_no IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY line_no ASC
	`
	selectArgs := append([]any{}, args...)
	selectArgs = append(selectArgs[:2], append([]any{string(lotto.TicketStatusActive)}, selectArgs[2:]...)...)
	rows, err := s.sdb.Read.QueryContext(ctx, query, selectArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tickets, err := scanTicketLines(rows)
	if err != nil {
		return nil, err
	}
	if len(tickets) == 0 {
		return nil, nil
	}

	updateArgs := []any{string(lotto.TicketStatusInactive), strings.TrimSpace(userID), round, string(lotto.TicketStatusActive)}
	for _, lineNo := range lineNos {
		updateArgs = append(updateArgs, lineNo)
	}
	if _, err := s.sdb.Write.ExecContext(ctx, `
		UPDATE lotto_tickets
		SET status = ?, update_time = CURRENT_TIMESTAMP
		WHERE user_id = ? AND round = ? AND status = ?
		  AND line_no IN (`+strings.Join(placeholders, ",")+`)
	`, updateArgs...); err != nil {
		return nil, err
	}
	for i := range tickets {
		tickets[i].Status = lotto.TicketStatusInactive
	}
	return tickets, nil
}

func withLottoTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
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

type lottoRowScanner interface {
	Scan(dest ...any) error
}

func scanLottoDraw(scanner lottoRowScanner) (lotto.Draw, error) {
	var draw lotto.Draw
	err := scanner.Scan(
		&draw.Round,
		&draw.DrawDate,
		&draw.Numbers[0],
		&draw.Numbers[1],
		&draw.Numbers[2],
		&draw.Numbers[3],
		&draw.Numbers[4],
		&draw.Numbers[5],
		&draw.BonusNumber,
		&draw.Rank1Winners,
		&draw.Rank1Prize,
		&draw.Rank2Winners,
		&draw.Rank2Prize,
		&draw.Rank3Winners,
		&draw.Rank3Prize,
		&draw.Rank4Winners,
		&draw.Rank4Prize,
		&draw.Rank5Winners,
		&draw.Rank5Prize,
		&draw.TotalWinners,
		&draw.TotalSales,
		&draw.CreateTime,
		&draw.UpdateTime,
	)
	return draw, err
}

func scanTicketLines(rows *sql.Rows) ([]lotto.TicketLine, error) {
	var tickets []lotto.TicketLine
	for rows.Next() {
		var ticket lotto.TicketLine
		var source string
		var status string
		if err := rows.Scan(
			&ticket.ID,
			&ticket.UserID,
			&ticket.Round,
			&ticket.LineNo,
			&ticket.Numbers[0],
			&ticket.Numbers[1],
			&ticket.Numbers[2],
			&ticket.Numbers[3],
			&ticket.Numbers[4],
			&ticket.Numbers[5],
			&source,
			&status,
			&ticket.CreateTime,
			&ticket.UpdateTime,
		); err != nil {
			return nil, err
		}
		ticket.Source = lotto.TicketSource(source)
		ticket.Status = lotto.TicketStatus(status)
		tickets = append(tickets, ticket)
	}
	return tickets, rows.Err()
}

func scanLottoShops(rows *sql.Rows) ([]lotto.FirstPrizeShop, error) {
	var shops []lotto.FirstPrizeShop
	for rows.Next() {
		var shop lotto.FirstPrizeShop
		if err := rows.Scan(
			&shop.Round,
			&shop.ShopID,
			&shop.ShopName,
			&shop.Region,
			&shop.District,
			&shop.Address1,
			&shop.Address2,
			&shop.Address3,
			&shop.Address4,
			&shop.FullAddress,
			&shop.WinMethodCode,
			&shop.WinMethodText,
			&shop.Latitude,
			&shop.Longitude,
			&shop.CreateTime,
			&shop.UpdateTime,
		); err != nil {
			return nil, err
		}
		shops = append(shops, shop)
	}
	return shops, rows.Err()
}

func uniqueSortedInts(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(values))
	out := make([]int, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for i := 0; i < len(out)-1; i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

var _ lotto.Repository = (*SQLiteLottoStore)(nil)
