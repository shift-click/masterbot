package scraper

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/shift-click/masterbot/internal/scraper/providers"
)

// ThemeSource indicates where a theme match came from.
type ThemeSource int

const (
	ThemeSourceJudal ThemeSource = iota
	ThemeSourceNaver
)

// ThemeMatchResult represents a matched theme from keyword search.
type ThemeMatchResult struct {
	No     int // Naver themeNo or Judal themeIdx
	Name   string
	Source ThemeSource
}

type themeDetailCacheEntry struct {
	detail    providers.ThemeDetail
	fetchedAt time.Time
}

type judalStockCacheEntry struct {
	codes     []string
	fetchedAt time.Time
}

// ThemeIndex caches themes from multiple sources and provides keyword matching.
type ThemeIndex struct {
	// Naver theme index
	naverMu      sync.RWMutex
	naverEntries []providers.ThemeEntry
	naverReady   bool

	// Judal theme index
	judalMu      sync.RWMutex
	judalEntries []providers.JudalThemeEntry
	judalReady   bool

	// Judal stock code cache (themeIdx → codes, TTL 1h)
	judalStockMu    sync.RWMutex
	judalStockCache map[int]judalStockCacheEntry
	judalStockTTL   time.Duration

	// Naver theme detail cache (themeNo → detail, TTL 3min)
	detailMu    sync.RWMutex
	detailCache map[int]themeDetailCacheEntry
	detailTTL   time.Duration

	naver  *providers.NaverStock
	judal  *providers.JudalScraper
	logger *slog.Logger
}

// NewThemeIndex creates a new ThemeIndex with Naver and optional Judal sources.
func NewThemeIndex(naver *providers.NaverStock, judal *providers.JudalScraper, logger *slog.Logger) *ThemeIndex {
	if logger == nil {
		logger = slog.Default()
	}
	return &ThemeIndex{
		judalStockCache: make(map[int]judalStockCacheEntry),
		judalStockTTL:   1 * time.Hour,
		detailCache:     make(map[int]themeDetailCacheEntry),
		detailTTL:       3 * time.Minute,
		naver:           naver,
		judal:           judal,
		logger:          logger.With("component", "theme_index"),
	}
}

// Start loads both theme indexes and refreshes them periodically.
func (t *ThemeIndex) Start(ctx context.Context) {
	t.refreshNaver(ctx)
	if t.judal != nil {
		t.refreshJudal(ctx)
	}

	naverTicker := time.NewTicker(1 * time.Hour)
	judalTicker := time.NewTicker(24 * time.Hour)
	defer naverTicker.Stop()
	defer judalTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-naverTicker.C:
			t.refreshNaver(ctx)
		case <-judalTicker.C:
			if t.judal != nil {
				t.refreshJudal(ctx)
			}
		}
	}
}

func (t *ThemeIndex) refreshNaver(ctx context.Context) {
	var all []providers.ThemeEntry
	for page := 1; page <= 5; page++ {
		entries, err := t.naver.FetchThemeList(ctx, page, 100)
		if err != nil {
			t.logger.Warn("naver theme list fetch failed", "page", page, "error", err)
			break
		}
		all = append(all, entries...)
		if len(entries) < 100 {
			break
		}
	}

	if len(all) == 0 {
		t.logger.Warn("naver theme index refresh returned 0 entries, keeping previous")
		return
	}

	t.naverMu.Lock()
	t.naverEntries = all
	t.naverReady = true
	t.naverMu.Unlock()

	t.logger.Info("naver theme index refreshed", "count", len(all))
}

func (t *ThemeIndex) refreshJudal(ctx context.Context) {
	entries, err := t.judal.FetchThemeList(ctx)
	if err != nil {
		t.logger.Warn("judal theme list fetch failed", "error", err)
		return
	}

	if len(entries) == 0 {
		t.logger.Warn("judal theme index refresh returned 0 entries, keeping previous")
		return
	}

	t.judalMu.Lock()
	t.judalEntries = entries
	t.judalReady = true
	t.judalMu.Unlock()

	t.logger.Info("judal theme index refreshed", "count", len(entries))
}

// Match searches the theme index for themes matching the keyword.
// Judal is searched first, then Naver as fallback.
func (t *ThemeIndex) Match(keyword string) []ThemeMatchResult {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil
	}

	// Try Judal first.
	if result := t.matchJudal(keyword); len(result) > 0 {
		return result
	}

	// Naver fallback.
	return t.matchNaver(keyword)
}

func (t *ThemeIndex) matchJudal(keyword string) []ThemeMatchResult {
	t.judalMu.RLock()
	defer t.judalMu.RUnlock()

	if !t.judalReady {
		return nil
	}

	var exact, prefix []ThemeMatchResult
	allowPrefix := utf8.RuneCountInString(keyword) >= 2

	for _, e := range t.judalEntries {
		r := ThemeMatchResult{No: e.Idx, Name: e.Name, Source: ThemeSourceJudal}
		// Strip parenthetical suffix for matching: "금(Gold)" → "금"
		baseName := e.Name
		if idx := strings.IndexByte(baseName, '('); idx > 0 {
			baseName = baseName[:idx]
		}

		if baseName == keyword || e.Name == keyword {
			exact = append(exact, r)
		} else if allowPrefix && (strings.HasPrefix(baseName, keyword) || strings.HasPrefix(e.Name, keyword)) {
			prefix = append(prefix, r)
		}
	}

	if len(exact) > 0 {
		return exact
	}
	if len(prefix) > 0 {
		return prefix
	}
	return nil
}

func (t *ThemeIndex) matchNaver(keyword string) []ThemeMatchResult {
	t.naverMu.RLock()
	defer t.naverMu.RUnlock()

	if !t.naverReady {
		return nil
	}

	var exact, prefix []ThemeMatchResult
	allowPrefix := utf8.RuneCountInString(keyword) >= 2

	for _, e := range t.naverEntries {
		r := ThemeMatchResult{No: e.No, Name: e.Name, Source: ThemeSourceNaver}
		if e.Name == keyword {
			exact = append(exact, r)
		} else if allowPrefix && strings.HasPrefix(e.Name, keyword) {
			prefix = append(prefix, r)
		}
	}

	if len(exact) > 0 {
		return exact
	}
	if len(prefix) > 0 {
		return prefix
	}
	return nil
}

// FetchJudalStockCodes fetches stock codes for a Judal theme with caching (TTL 1h).
func (t *ThemeIndex) FetchJudalStockCodes(ctx context.Context, themeIdx int) ([]string, error) {
	t.judalStockMu.RLock()
	if entry, ok := t.judalStockCache[themeIdx]; ok && time.Since(entry.fetchedAt) < t.judalStockTTL {
		t.judalStockMu.RUnlock()
		return entry.codes, nil
	}
	t.judalStockMu.RUnlock()

	codes, err := t.judal.FetchStockCodes(ctx, themeIdx)
	if err != nil {
		// Return stale cache if available.
		t.judalStockMu.RLock()
		if entry, ok := t.judalStockCache[themeIdx]; ok {
			t.judalStockMu.RUnlock()
			t.logger.Warn("judal stock codes fetch failed, returning stale", "themeIdx", themeIdx, "error", err)
			return entry.codes, nil
		}
		t.judalStockMu.RUnlock()
		return nil, err
	}

	t.judalStockMu.Lock()
	t.judalStockCache[themeIdx] = judalStockCacheEntry{codes: codes, fetchedAt: time.Now()}
	t.judalStockMu.Unlock()

	return codes, nil
}

// FetchDetail fetches Naver theme detail with caching (TTL 3 min).
func (t *ThemeIndex) FetchDetail(ctx context.Context, themeNo int) (providers.ThemeDetail, error) {
	t.detailMu.RLock()
	if entry, ok := t.detailCache[themeNo]; ok && time.Since(entry.fetchedAt) < t.detailTTL {
		t.detailMu.RUnlock()
		return entry.detail, nil
	}
	t.detailMu.RUnlock()

	detail, err := t.naver.FetchThemeDetail(ctx, themeNo)
	if err != nil {
		t.detailMu.RLock()
		if entry, ok := t.detailCache[themeNo]; ok {
			t.detailMu.RUnlock()
			t.logger.Warn("theme detail fetch failed, returning stale", "themeNo", themeNo, "error", err)
			return entry.detail, nil
		}
		t.detailMu.RUnlock()
		return providers.ThemeDetail{}, err
	}

	sort.Slice(detail.Stocks, func(i, j int) bool {
		return detail.Stocks[i].MarketValue > detail.Stocks[j].MarketValue
	})

	t.detailMu.Lock()
	t.detailCache[themeNo] = themeDetailCacheEntry{detail: detail, fetchedAt: time.Now()}
	t.detailMu.Unlock()

	return detail, nil
}
