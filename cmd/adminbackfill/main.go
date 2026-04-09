package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/metrics"
	irisclient "github.com/shift-click/masterbot/internal/transport/iris"
)

const defaultBatchSize = 1000

type irisQueryClient interface {
	Query(ctx context.Context, query string, bind ...string) ([]map[string]any, error)
}

type irisMessageRow struct {
	ID        string
	ChatID    string
	UserID    string
	CreatedAt time.Time
	RoomType  string
	RoomMeta  string
}

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to config file")
	sinceText := flag.String("since", "", "inclusive lower bound (RFC3339 or YYYY-MM-DD)")
	untilText := flag.String("until", "", "exclusive upper bound (RFC3339 or YYYY-MM-DD)")
	batchSize := flag.Int("batch-size", defaultBatchSize, "rows to fetch per batch")
	dryRun := flag.Bool("dry-run", false, "scan rows without writing metrics")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if !cfg.Admin.MetricsEnabled {
		slog.Error("admin metrics are disabled; enable admin.metrics_enabled first")
		os.Exit(1)
	}
	if strings.TrimSpace(cfg.Admin.PseudonymSecret) == "" {
		slog.Error("admin pseudonym secret is empty")
		os.Exit(1)
	}
	instances := cfg.Iris.ResolvedInstances()
	if len(instances) == 0 || strings.TrimSpace(instances[0].HTTPURL) == "" {
		slog.Error("iris http url is not configured")
		os.Exit(1)
	}
	if *batchSize <= 0 {
		slog.Error("batch-size must be greater than zero")
		os.Exit(1)
	}

	now := time.Now().UTC()
	since, err := parseBound(*sinceText, now.Add(-cfg.Admin.RawRetention), false)
	if err != nil {
		slog.Error("invalid since", "error", err)
		os.Exit(1)
	}
	until, err := parseBound(*untilText, now, true)
	if err != nil {
		slog.Error("invalid until", "error", err)
		os.Exit(1)
	}
	if !since.Before(until) {
		slog.Error("since must be before until", "since", since, "until", until)
		os.Exit(1)
	}

	client := irisclient.NewClient(instances[0], nil)
	store, err := metrics.NewSQLiteStore(cfg.Admin.MetricsDBPath)
	if err != nil {
		slog.Error("failed to open metrics store", "error", err)
		os.Exit(1)
	}
	defer func() { _ = store.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	aliases := roomAliasMap(cfg.Access.Rooms)
	summary, err := runBackfill(ctx, client, store, cfg.Admin.PseudonymSecret, aliases, since, until, *batchSize, *dryRun)
	if err != nil {
		slog.Error("backfill failed", "error", err)
		os.Exit(1)
	}

	slog.Info("backfill complete",
		"since", since.Format(time.RFC3339),
		"until", until.Format(time.RFC3339),
		"scanned", summary.Scanned,
		"inserted", summary.Inserted,
		"dry_run", *dryRun,
	)
}

type backfillSummary struct {
	Scanned  int64
	Inserted int64
}

func runBackfill(
	ctx context.Context,
	client irisQueryClient,
	store *metrics.SQLiteStore,
	secret string,
	aliases map[string]string,
	since, until time.Time,
	batchSize int,
	dryRun bool,
) (backfillSummary, error) {
	var summary backfillSummary
	lastID := "0"
	for {
		rows, err := queryMessageRows(ctx, client, since, until, lastID, batchSize)
		if err != nil {
			return summary, err
		}
		if len(rows) == 0 {
			break
		}
		summary.Scanned += int64(len(rows))
		lastID = rows[len(rows)-1].ID

		if dryRun {
			continue
		}
		events := make([]metrics.StoredEvent, 0, len(rows))
		for _, row := range rows {
			roomName := extractRoomName(row.RoomType, row.RoomMeta, row.ChatID, secret)
			events = append(events, metrics.BuildStoredEvent(metrics.Event{
				OccurredAt:     row.CreatedAt,
				RequestID:      row.ID,
				EventName:      metrics.EventMessageReceived,
				RawRoomID:      row.ChatID,
				RawTenantID:    row.ChatID,
				RawScopeRoomID: row.ChatID,
				RoomName:       roomName,
				RawUserID:      row.UserID,
				Audience:       "customer",
				FeatureKey:     "",
				Attribution:    "",
			}, secret, aliases))
		}
		inserted, err := store.InsertEventsIfAbsent(ctx, events)
		if err != nil {
			return summary, err
		}
		summary.Inserted += inserted
	}

	if !dryRun && summary.Inserted > 0 {
		if err := store.RebuildRollups(ctx); err != nil {
			return summary, err
		}
	}
	return summary, nil
}

func queryMessageRows(
	ctx context.Context,
	client irisQueryClient,
	since, until time.Time,
	lastID string,
	limit int,
) ([]irisMessageRow, error) {
	rows, err := client.Query(ctx, `
		SELECT
			CAST(c._id AS TEXT) AS _id,
			CAST(c.chat_id AS TEXT) AS chat_id,
			CAST(COALESCE(c.user_id, '') AS TEXT) AS user_id,
			CAST(c.created_at AS TEXT) AS created_at,
			COALESCE(r.type, '') AS room_type,
			COALESCE(r.meta, '') AS room_meta
		FROM chat_logs c
		LEFT JOIN chat_rooms r
		  ON r.id = c.chat_id
		WHERE c.created_at >= CAST(? AS INTEGER)
		  AND c.created_at < CAST(? AS INTEGER)
		  AND c._id > CAST(? AS INTEGER)
		ORDER BY c._id ASC
		LIMIT CAST(? AS INTEGER)
	`, strconv.FormatInt(since.Unix(), 10), strconv.FormatInt(until.Unix(), 10), lastID, strconv.Itoa(limit))
	if err != nil {
		return nil, fmt.Errorf("query iris chat logs: %w", err)
	}

	out := make([]irisMessageRow, 0, len(rows))
	for _, row := range rows {
		createdAt, err := parseUnixField(row["created_at"])
		if err != nil {
			return nil, fmt.Errorf("parse created_at for row %v: %w", row["_id"], err)
		}
		out = append(out, irisMessageRow{
			ID:        strings.TrimSpace(anyString(row["_id"])),
			ChatID:    strings.TrimSpace(anyString(row["chat_id"])),
			UserID:    strings.TrimSpace(anyString(row["user_id"])),
			CreatedAt: createdAt,
			RoomType:  strings.TrimSpace(anyString(row["room_type"])),
			RoomMeta:  strings.TrimSpace(anyString(row["room_meta"])),
		})
	}
	return out, nil
}

func roomAliasMap(rooms []config.AccessRoomConfig) map[string]string {
	aliases := make(map[string]string, len(rooms))
	for _, room := range rooms {
		chatID := strings.TrimSpace(room.ChatID)
		if chatID == "" {
			continue
		}
		aliases[chatID] = strings.TrimSpace(room.Alias)
	}
	return aliases
}

func parseBound(value string, fallback time.Time, endOfDay bool) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback.UTC(), nil
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts.UTC(), nil
	}
	day, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("unsupported time format %q", value)
	}
	if endOfDay {
		return day.Add(24 * time.Hour).UTC(), nil
	}
	return day.UTC(), nil
}

func parseUnixField(value any) (time.Time, error) {
	raw := strings.TrimSpace(anyString(value))
	sec, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(sec, 0).UTC(), nil
}

func anyString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

// fallbackLabelHashLength controls how many hex characters are appended to
// fallback room labels (e.g., "KAKAO-직접 #a3f0"). 4 hex chars = 16 bits =
// 65,536 distinct ids — enough to disambiguate rooms in operational scale
// without bloating the label width.
const fallbackLabelHashLength = 4

func extractRoomName(roomType, metaText, chatID, secret string) string {
	type metaEntry struct {
		Type    int    `json:"type"`
		Content string `json:"content"`
	}
	var entries []metaEntry
	if strings.TrimSpace(metaText) != "" {
		if err := json.Unmarshal([]byte(metaText), &entries); err == nil {
			for _, entry := range entries {
				if entry.Type == 3 {
					title := strings.TrimSpace(entry.Content)
					if title != "" {
						return title
					}
				}
			}
		}
	}
	var base string
	switch strings.TrimSpace(roomType) {
	case "DirectChat":
		base = "KAKAO-직접"
	case "MultiChat":
		base = "KAKAO-그룹"
	default:
		return ""
	}
	suffix := metrics.RoomShortHash(secret, chatID, fallbackLabelHashLength)
	if suffix == "" {
		return base
	}
	return base + " #" + suffix
}
