package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// NaverStockOHLC fetches historical OHLC data for Korean and world stocks
// from Naver Finance APIs.
type NaverStockOHLC struct {
	client *BreakerHTTPClient
	logger *slog.Logger
	now    func() time.Time
}

// NewNaverStockOHLC creates a new Naver stock OHLC provider.
func NewNaverStockOHLC(logger *slog.Logger) *NaverStockOHLC {
	if logger == nil {
		logger = slog.Default()
	}
	return &NaverStockOHLC{
		client: DefaultBreakerClient(10*time.Second, "naver_stock_ohlc", logger),
		logger: logger.With("component", "naver_stock_ohlc"),
		now:    time.Now,
	}
}

// FetchDomestic retrieves OHLC data for a Korean stock using fchart API.
func (n *NaverStockOHLC) FetchDomestic(ctx context.Context, code string, tf Timeframe) (OHLCData, error) {
	startDate, endDate := stockDateRange(n.now(), tf)
	requestStart := startDate
	if tf != Timeframe3M {
		requestStart = requestStart.AddDate(0, 0, -10) // extra buffer for non-trading days
	}

	url := fmt.Sprintf(
		"https://fchart.stock.naver.com/siseJson.naver?symbol=%s&requestType=1&startTime=%s&endTime=%s&timeframe=day",
		code,
		requestStart.Format("20060102"),
		endDate.Format("20060102"),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return OHLCData{}, fmt.Errorf("naver domestic ohlc request: %w", err)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return OHLCData{}, fmt.Errorf("naver domestic ohlc fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return OHLCData{}, fmt.Errorf("naver domestic ohlc: status %d: %s", resp.StatusCode, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return OHLCData{}, fmt.Errorf("naver domestic ohlc read: %w", err)
	}

	points, err := parseFchartResponse(string(body))
	if err != nil {
		return OHLCData{}, fmt.Errorf("naver domestic ohlc parse: %w", err)
	}

	if len(points) == 0 {
		return OHLCData{}, fmt.Errorf("naver domestic ohlc: no data for %s", code)
	}

	points = filterPointsByDateRange(points, startDate, endDate)
	if len(points) == 0 {
		return OHLCData{}, fmt.Errorf("naver domestic ohlc: no data in range for %s", code)
	}

	return OHLCData{Symbol: code, Points: points}, nil
}

// fchartRowRe matches a single data row from the fchart response.
// Example: ["20260330",171000,176650,170600,176300,22269147,48.62]
var fchartRowRe = regexp.MustCompile(`\["(\d{8})",\s*(\d+),\s*(\d+),\s*(\d+),\s*(\d+),\s*(\d+)`)

func parseFchartResponse(body string) ([]OHLCPoint, error) {
	matches := fchartRowRe.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil, nil
	}

	points := make([]OHLCPoint, 0, len(matches))
	for _, m := range matches {
		date, err := time.Parse("20060102", m[1])
		if err != nil {
			continue
		}
		open, _ := strconv.ParseFloat(m[2], 64)
		high, _ := strconv.ParseFloat(m[3], 64)
		low, _ := strconv.ParseFloat(m[4], 64)
		closePrice, _ := strconv.ParseFloat(m[5], 64)
		vol, _ := strconv.ParseFloat(m[6], 64)

		points = append(points, OHLCPoint{
			Time:   date,
			Open:   open,
			High:   high,
			Low:    low,
			Close:  closePrice,
			Volume: vol,
		})
	}

	return points, nil
}

// FetchWorld retrieves OHLC data for a world stock using the Naver price API.
// Pages are fetched concurrently with errgroup (max 5 concurrent requests).
func (n *NaverStockOHLC) FetchWorld(ctx context.Context, reutersCode string, tf Timeframe) (OHLCData, error) {
	if isUnsupportedReferenceID(reutersCode) {
		return OHLCData{}, fmt.Errorf("%w: %s", ErrWorldStockChartUnavailable, reutersCode)
	}

	startDate, endDate := stockDateRange(n.now(), tf)
	maxPages := stockWorldPageBudget(startDate, endDate)

	// Fetch all pages concurrently.
	var mu sync.Mutex
	var allPoints []OHLCPoint

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(5)

	for page := 1; page <= maxPages; page++ {
		g.Go(func() error {
			points, err := n.fetchWorldPage(gctx, reutersCode, page)
			if err != nil {
				return err
			}
			if len(points) == 0 {
				return nil
			}
			mu.Lock()
			allPoints = append(allPoints, points...)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		// If we got some data despite errors, use it.
		if len(allPoints) == 0 {
			return OHLCData{}, fmt.Errorf("naver world ohlc: %w", err)
		}
		n.logger.Warn("naver world ohlc partial failure", "error", err, "points", len(allPoints))
	}

	if len(allPoints) == 0 {
		return OHLCData{}, fmt.Errorf("naver world ohlc: no data for %s", reutersCode)
	}

	// Sort chronologically
	sort.Slice(allPoints, func(i, j int) bool {
		return allPoints[i].Time.Before(allPoints[j].Time)
	})

	filtered := filterPointsByDateRange(allPoints, startDate, endDate)
	if len(filtered) == 0 {
		return OHLCData{}, fmt.Errorf("naver world ohlc: no data in range for %s", reutersCode)
	}

	return OHLCData{Symbol: reutersCode, Points: filtered}, nil
}

type naverWorldPriceItem struct {
	LocalTradedAt string `json:"localTradedAt"`
	ClosePrice    string `json:"closePrice"`
	OpenPrice     string `json:"openPrice"`
	HighPrice     string `json:"highPrice"`
	LowPrice      string `json:"lowPrice"`
}

func (n *NaverStockOHLC) fetchWorldPage(ctx context.Context, reutersCode string, page int) ([]OHLCPoint, error) {
	url := fmt.Sprintf(
		"https://api.stock.naver.com/stock/%s/price?page=%d&pageSize=20",
		reutersCode, page,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("naver world ohlc request: %w", err)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("naver world ohlc fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("naver world ohlc: status %d: %s", resp.StatusCode, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("naver world ohlc read: %w", err)
	}

	var items []naverWorldPriceItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("naver world ohlc parse: %w", err)
	}

	points := make([]OHLCPoint, 0, len(items))
	for _, item := range items {
		t := parseWorldDate(item.LocalTradedAt)
		if t.IsZero() {
			continue
		}

		closePrice := parseNaverPrice(item.ClosePrice)
		open := parseNaverPrice(item.OpenPrice)
		high := parseNaverPrice(item.HighPrice)
		low := parseNaverPrice(item.LowPrice)

		points = append(points, OHLCPoint{
			Time:  t,
			Open:  open,
			High:  high,
			Low:   low,
			Close: closePrice,
			// Volume not available from this API
		})
	}

	return points, nil
}

func parseWorldDate(s string) time.Time {
	// Try ISO 8601 with timezone: "2026-03-30T16:00:00-04:00"
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	// Try date only: "2026-03-30"
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t
	}
	return time.Time{}
}

func parseNaverPrice(s string) float64 {
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func stockDateRange(now time.Time, tf Timeframe) (time.Time, time.Time) {
	endDate := normalizeCalendarDate(now)
	switch tf {
	case Timeframe3M:
		return endDate.AddDate(0, -3, 0), endDate
	default:
		return endDate.AddDate(0, 0, -TimeframeDays(tf)), endDate
	}
}

func stockWorldPageBudget(startDate, endDate time.Time) int {
	calendarDays := int(endDate.Sub(startDate).Hours()/24) + 1
	if calendarDays < 20 {
		calendarDays = 20
	}
	return (calendarDays / 20) + 3
}

func filterPointsByDateRange(points []OHLCPoint, startDate, endDate time.Time) []OHLCPoint {
	filtered := make([]OHLCPoint, 0, len(points))
	for _, p := range points {
		pointDate := normalizeCalendarDate(p.Time)
		if pointDate.Before(startDate) || pointDate.After(endDate) {
			continue
		}
		filtered = append(filtered, p)
	}
	return filtered
}

func normalizeCalendarDate(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
