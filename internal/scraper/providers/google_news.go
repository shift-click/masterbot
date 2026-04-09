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

// NewsItem represents a single popular news article.
type NewsItem struct {
	Title  string
	Link   string
	Source string
}

// GoogleNews fetches popular news from Google News RSS.
type GoogleNews struct {
	client *BreakerHTTPClient
	logger   *slog.Logger
	cacheTTL time.Duration

	mu        sync.RWMutex
	items     []NewsItem
	updatedAt time.Time
}

const googleNewsRSSURL = "https://news.google.com/rss?hl=ko&gl=KR&ceid=KR:ko"

// NewGoogleNews creates a new GoogleNews provider.
func NewGoogleNews(logger *slog.Logger) *GoogleNews {
	if logger == nil {
		logger = slog.Default()
	}
	return &GoogleNews{
		client:   DefaultBreakerClient(10 * time.Second, "google_news", logger),
		logger:   logger.With("component", "google_news"),
		cacheTTL: 5 * time.Minute,
	}
}

// TopNews returns the top N popular news items.
func (g *GoogleNews) TopNews(ctx context.Context, n int) ([]NewsItem, error) {
	if items := g.cached(n); items != nil {
		return items, nil
	}
	if err := g.refresh(ctx); err != nil {
		if items := g.stale(n); items != nil {
			return items, nil
		}
		return nil, err
	}
	return g.cached(n), nil
}

func (g *GoogleNews) cached(n int) []NewsItem {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.items != nil && time.Since(g.updatedAt) < g.cacheTTL {
		return g.take(n)
	}
	return nil
}

func (g *GoogleNews) stale(n int) []NewsItem {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.items != nil {
		return g.take(n)
	}
	return nil
}

// take returns up to n items. Must be called with mu held.
func (g *GoogleNews) take(n int) []NewsItem {
	if n <= 0 || n > len(g.items) {
		n = len(g.items)
	}
	out := make([]NewsItem, n)
	copy(out, g.items[:n])
	return out
}

func (g *GoogleNews) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleNewsRSSURL, nil)
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
		return fmt.Errorf("google news RSS status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return err
	}

	rssItems, err := parseNewsRSS(body)
	if err != nil {
		return fmt.Errorf("parse news RSS: %w", err)
	}

	items := make([]NewsItem, len(rssItems))
	for i, raw := range rssItems {
		items[i] = NewsItem{
			Title:  raw.Title,
			Link:   raw.Link,
			Source: raw.Source,
		}
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	g.items = items
	g.updatedAt = time.Now()

	return nil
}

// RSS XML structures for Google News.

type newsRSS struct {
	XMLName xml.Name    `xml:"rss"`
	Channel newsChannel `xml:"channel"`
}

type newsChannel struct {
	Items []newsRSSItem `xml:"item"`
}

type newsRSSItem struct {
	Title  string `xml:"title"`
	Link   string `xml:"link"`
	Source string `xml:"source"`
}

func parseNewsRSS(data []byte) ([]newsRSSItem, error) {
	var rss newsRSS
	if err := xml.Unmarshal(data, &rss); err != nil {
		return nil, err
	}
	return rss.Channel.Items, nil
}
