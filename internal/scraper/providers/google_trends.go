package providers

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// TrendItem represents a single trending search item.
type TrendItem struct {
	Title   string
	Traffic string // approximate traffic, e.g. "2000+"
	Change  TrendChange
}

// TrendChange represents the rank change direction.
type TrendChange int

const (
	TrendChangeNone TrendChange = iota // ➡️ same rank
	TrendChangeUp                      // 🔺 moved up
	TrendChangeDown                    // 🔻 moved down
	TrendChangeNew                     // 🆕 new entry
)

// GoogleTrends fetches trending search data from Google Trends RSS.
type GoogleTrends struct {
	client *BreakerHTTPClient
	logger   *slog.Logger
	cacheTTL time.Duration

	mu        sync.RWMutex
	items     []TrendItem
	updatedAt time.Time
	prevRanks map[string]int // keyword → previous rank (1-based)
}

const googleTrendsRSSURL = "https://trends.google.com/trending/rss?geo=KR"

// NewGoogleTrends creates a new GoogleTrends provider.
func NewGoogleTrends(logger *slog.Logger) *GoogleTrends {
	if logger == nil {
		logger = slog.Default()
	}
	return &GoogleTrends{
		client:   DefaultBreakerClient(10 * time.Second, "google_trends", logger),
		logger:   logger.With("component", "google_trends"),
		cacheTTL: 5 * time.Minute,
	}
}

// Trends returns the current trending items, fetching from RSS if cache is stale.
func (g *GoogleTrends) Trends(ctx context.Context) ([]TrendItem, error) {
	if items := g.cached(); items != nil {
		return items, nil
	}
	if err := g.refresh(ctx); err != nil {
		if items := g.stale(); items != nil {
			return items, nil
		}
		return nil, err
	}
	return g.cached(), nil
}

func (g *GoogleTrends) cached() []TrendItem {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.items != nil && time.Since(g.updatedAt) < g.cacheTTL {
		return append([]TrendItem(nil), g.items...)
	}
	return nil
}

func (g *GoogleTrends) stale() []TrendItem {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.items != nil {
		return append([]TrendItem(nil), g.items...)
	}
	return nil
}

func (g *GoogleTrends) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleTrendsRSSURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; JucoBot/2.0)")

	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("google trends RSS status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return err
	}

	rssItems, err := parseTrendsRSS(body)
	if err != nil {
		return fmt.Errorf("parse trends RSS: %w", err)
	}

	const maxItems = 10
	if len(rssItems) > maxItems {
		rssItems = rssItems[:maxItems]
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Compute rank changes against previous snapshot.
	items := make([]TrendItem, len(rssItems))
	for i, raw := range rssItems {
		change := TrendChangeNone
		if g.prevRanks != nil {
			prevRank, existed := g.prevRanks[raw.Title]
			newRank := i + 1
			switch {
			case !existed:
				change = TrendChangeNew
			case newRank < prevRank:
				change = TrendChangeUp
			case newRank > prevRank:
				change = TrendChangeDown
			default:
				change = TrendChangeNone
			}
		}
		items[i] = TrendItem{
			Title:   raw.Title,
			Traffic: raw.Traffic,
			Change:  change,
		}
	}

	// Update previous ranks for next comparison.
	newPrev := make(map[string]int, len(rssItems))
	for i, raw := range rssItems {
		newPrev[raw.Title] = i + 1
	}
	g.prevRanks = newPrev
	g.items = items
	g.updatedAt = time.Now()

	return nil
}

// RSS XML structures for Google Trends.

type trendsRSS struct {
	XMLName xml.Name       `xml:"rss"`
	Channel trendsChannel  `xml:"channel"`
}

type trendsChannel struct {
	Items []trendsRSSItem `xml:"item"`
}

type trendsRSSItem struct {
	Title   string `xml:"title"`
	Traffic string `xml:"approx_traffic"`
}

func parseTrendsRSS(data []byte) ([]trendsRSSItem, error) {
	var rss trendsRSS
	if err := xml.Unmarshal(data, &rss); err != nil {
		return nil, err
	}
	return rss.Channel.Items, nil
}
