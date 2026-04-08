package providers

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"time"
)

// JudalThemeEntry represents a theme from the Judal theme list.
type JudalThemeEntry struct {
	Idx  int
	Name string
}

// JudalScraper scrapes theme data from judal.co.kr.
type JudalScraper struct {
	client *BreakerHTTPClient
	logger *slog.Logger
}

// NewJudalScraper creates a new JudalScraper.
func NewJudalScraper(logger *slog.Logger) *JudalScraper {
	if logger == nil {
		logger = slog.Default()
	}
	return &JudalScraper{
		client: DefaultBreakerClient(15 * time.Second, "judal", logger),
		logger: logger.With("component", "judal_scraper"),
	}
}

var (
	reThemeTalk  = regexp.MustCompile(`themeIdx=(\d+).*?title="(.+?) 테마토크"`)
	reStockCode  = regexp.MustCompile(`code=(\d{6})`)
)

// FetchThemeList scrapes the Judal theme list page and returns all theme entries.
func (j *JudalScraper) FetchThemeList(ctx context.Context) ([]JudalThemeEntry, error) {
	body, err := j.fetch(ctx, "https://www.judal.co.kr/?view=themeList")
	if err != nil {
		return nil, fmt.Errorf("judal theme list: %w", err)
	}

	matches := reThemeTalk.FindAllStringSubmatch(string(body), -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("judal theme list: no themes found in HTML")
	}

	seen := make(map[int]bool)
	var entries []JudalThemeEntry
	for _, m := range matches {
		var idx int
		fmt.Sscanf(m[1], "%d", &idx)
		if seen[idx] {
			continue
		}
		seen[idx] = true
		entries = append(entries, JudalThemeEntry{Idx: idx, Name: m[2]})
	}

	return entries, nil
}

// FetchStockCodes scrapes a Judal theme stock list page and returns stock codes.
func (j *JudalScraper) FetchStockCodes(ctx context.Context, themeIdx int) ([]string, error) {
	u := fmt.Sprintf("https://www.judal.co.kr/?view=stockList&themeIdx=%d", themeIdx)
	body, err := j.fetch(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("judal stock list: %w", err)
	}

	matches := reStockCode.FindAllStringSubmatch(string(body), -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("judal stock list: no stock codes found for themeIdx=%d", themeIdx)
	}

	seen := make(map[string]bool)
	var codes []string
	for _, m := range matches {
		code := m[1]
		if seen[code] {
			continue
		}
		seen[code] = true
		codes = append(codes, code)
	}

	return codes, nil
}

func (j *JudalScraper) fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 10) AppleWebKit/537.36")

	resp, err := j.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	return io.ReadAll(resp.Body)
}
