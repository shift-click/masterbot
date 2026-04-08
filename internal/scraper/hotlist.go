package scraper

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// HotListConfig holds configuration for the HotList.
type HotListConfig struct {
	PollInterval    time.Duration // How often to refresh hot stocks (default: 2s).
	IdleTimeout     time.Duration // Remove stock after this much idle time (default: 10m).
	OffHourInterval time.Duration // Poll interval outside market hours (default: 60s).
	MarketOpen      int           // Market open hour in KST (default: 9).
	MarketClose     int           // Market close hour in KST (default: 16, covers 15:30 close + settlement).
}

// DefaultHotListConfig returns sensible defaults.
func DefaultHotListConfig() HotListConfig {
	return HotListConfig{
		PollInterval:    2 * time.Second,
		IdleTimeout:     10 * time.Minute,
		OffHourInterval: 60 * time.Second,
		MarketOpen:      9,
		MarketClose:     16,
	}
}

// HotListFetcher is a function that fetches stock data for a code.
// Returns serialized JSON data. This avoids an import cycle with providers.
type HotListFetcher func(ctx context.Context, code string) (json.RawMessage, error)

// HotSnapshot is what the hotlist stores and returns.
type HotSnapshot struct {
	Data      json.RawMessage
	UpdatedAt time.Time
}

type hotEntry struct {
	snapshot       HotSnapshot
	lastAccess     time.Time
	exchangeZoneID string // e.g. "EST5EDT" for US stocks, empty for KST default
	isWorldStock   bool
}

// HotList maintains a set of actively-polled stocks.
// Stocks are added when first queried and removed after idle timeout.
type HotList struct {
	mu           sync.RWMutex
	entries      map[string]*hotEntry // keyed by stock code
	fetcher      HotListFetcher
	worldFetcher HotListFetcher // fetcher for world stocks (uses FetchWorldQuote)
	config       HotListConfig
	logger       *slog.Logger
	kst          *time.Location
}

// NewHotList creates a new HotList.
func NewHotList(fetcher HotListFetcher, cfg HotListConfig, logger *slog.Logger) *HotList {
	if logger == nil {
		logger = slog.Default()
	}

	kst, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		kst = time.FixedZone("KST", 9*60*60)
	}

	return &HotList{
		entries: make(map[string]*hotEntry),
		fetcher: fetcher,
		config:  cfg,
		logger:  logger.With("component", "hotlist"),
		kst:     kst,
	}
}

// SetWorldFetcher sets a separate fetcher for world stocks.
func (h *HotList) SetWorldFetcher(f HotListFetcher) {
	h.worldFetcher = f
}

// Get returns the cached snapshot for code, or false if not in hotlist.
// Also touches the entry to reset idle timer.
func (h *HotList) Get(code string) (HotSnapshot, bool) {
	h.mu.RLock()
	entry, ok := h.entries[code]
	h.mu.RUnlock()
	if !ok {
		return HotSnapshot{}, false
	}

	h.mu.Lock()
	entry.lastAccess = time.Now()
	h.mu.Unlock()

	return entry.snapshot, true
}

// Register adds a stock to the hotlist with the given initial data.
func (h *HotList) Register(code string, data json.RawMessage) {
	h.RegisterWithZone(code, data, "")
}

// RegisterWithZone adds a stock to the hotlist with timezone info for market hours.
func (h *HotList) RegisterWithZone(code string, data json.RawMessage, zoneID string) {
	h.RegisterWithMeta(code, data, false, zoneID)
}

// RegisterWithMeta adds a stock to the hotlist with world stock flag and timezone info.
func (h *HotList) RegisterWithMeta(code string, data json.RawMessage, isWorld bool, zoneID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.entries[code] = &hotEntry{
		snapshot: HotSnapshot{
			Data:      data,
			UpdatedAt: time.Now(),
		},
		lastAccess:     time.Now(),
		exchangeZoneID: zoneID,
		isWorldStock:   isWorld,
	}
	h.logger.Debug("registered stock to hotlist", "code", code, "isWorld", isWorld, "zoneID", zoneID)
}

// Codes returns all currently active stock codes.
func (h *HotList) Codes() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	codes := make([]string, 0, len(h.entries))
	for code := range h.entries {
		codes = append(codes, code)
	}
	return codes
}

// Start begins the background polling and eviction loops.
// Blocks until ctx is cancelled.
func (h *HotList) Start(ctx context.Context) {
	h.logger.Info("hotlist polling started",
		"poll_interval", h.config.PollInterval,
		"idle_timeout", h.config.IdleTimeout,
	)

	pollTicker := time.NewTicker(h.config.PollInterval)
	evictTicker := time.NewTicker(30 * time.Second)
	defer pollTicker.Stop()
	defer evictTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("hotlist polling stopped")
			return

		case <-pollTicker.C:
			h.pollAll(ctx)
			// Adjust ticker based on market hours.
			newInterval := h.currentPollInterval()
			pollTicker.Reset(newInterval)

		case <-evictTicker.C:
			h.evictIdle()
		}
	}
}

// pollAll refreshes all stocks in the hotlist.
func (h *HotList) pollAll(ctx context.Context) {
	h.mu.RLock()
	type pollItem struct {
		code    string
		isWorld bool
	}
	items := make([]pollItem, 0, len(h.entries))
	for code, entry := range h.entries {
		items = append(items, pollItem{code: code, isWorld: entry.isWorldStock})
	}
	h.mu.RUnlock()

	if len(items) == 0 {
		return
	}

	for _, item := range items {
		select {
		case <-ctx.Done():
			return
		default:
		}

		fetcher := h.fetcher
		if item.isWorld && h.worldFetcher != nil {
			fetcher = h.worldFetcher
		}

		data, err := fetcher(ctx, item.code)
		if err != nil {
			h.logger.Warn("hotlist poll failed", "code", item.code, "error", err)
			continue
		}

		h.mu.Lock()
		if entry, ok := h.entries[item.code]; ok {
			entry.snapshot = HotSnapshot{
				Data:      data,
				UpdatedAt: time.Now(),
			}
		}
		h.mu.Unlock()
	}
}

// evictIdle removes stocks that haven't been accessed within idle timeout.
func (h *HotList) evictIdle() {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	for code, entry := range h.entries {
		if now.Sub(entry.lastAccess) > h.config.IdleTimeout {
			delete(h.entries, code)
			h.logger.Debug("evicted idle stock from hotlist", "code", code)
		}
	}
}

// currentPollInterval returns the appropriate poll interval based on market hours.
// Uses the most aggressive (shortest) interval needed across all entries.
func (h *HotList) currentPollInterval() time.Duration {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// If any entry needs active polling, use the active interval.
	for _, entry := range h.entries {
		if h.isMarketOpen(entry.exchangeZoneID) {
			return h.config.PollInterval
		}
	}

	// No entries have open markets.
	if len(h.entries) > 0 {
		return h.config.OffHourInterval
	}

	return h.config.PollInterval
}

// isMarketOpen checks if the market is currently open for the given timezone.
// Empty zoneID defaults to KST (Korean market).
func (h *HotList) isMarketOpen(zoneID string) bool {
	var now time.Time
	if zoneID == "" {
		// Default: Korean market (KST)
		now = time.Now().In(h.kst)
	} else {
		loc, err := time.LoadLocation(zoneID)
		if err != nil {
			// Fallback to KST if timezone is invalid.
			now = time.Now().In(h.kst)
		} else {
			now = time.Now().In(loc)
		}
	}

	weekday := now.Weekday()
	if weekday == time.Saturday || weekday == time.Sunday {
		return false
	}

	hour := now.Hour()
	return hour >= h.config.MarketOpen && hour < h.config.MarketClose
}
