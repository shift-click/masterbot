package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"time"
)

// NaverEsports fetches LoL match schedule from Naver Game as a fallback provider.
// It extracts schedule data from the __NEXT_DATA__ JSON embedded in the SSR page.
type NaverEsports struct {
	client *BreakerHTTPClient
	logger *slog.Logger
}

// NewNaverEsports creates a new NaverEsports provider.
func NewNaverEsports(logger *slog.Logger) *NaverEsports {
	if logger == nil {
		logger = slog.Default()
	}
	return &NaverEsports{
		client: DefaultBreakerClient(10 * time.Second, "naver_esports", logger),
		logger: logger.With("component", "naver_esports"),
	}
}

// naverNextDataRe extracts the __NEXT_DATA__ JSON from the HTML page.
var naverNextDataRe = regexp.MustCompile(`<script id="__NEXT_DATA__" type="application/json">(.+?)</script>`)

// naver __NEXT_DATA__ response types

type naverNextData struct {
	Props struct {
		PageProps struct {
			ScheduleData []naverScheduleMatch `json:"scheduleData"`
		} `json:"pageProps"`
	} `json:"props"`
}

type naverScheduleMatch struct {
	GameID        string          `json:"gameId"`
	StartDate     int64           `json:"startDate"` // unix millis
	MatchStatus   string          `json:"matchStatus"` // "RESULT", "BEFORE", "LIVE"
	HomeTeam      naverTeam       `json:"homeTeam"`
	AwayTeam      naverTeam       `json:"awayTeam"`
	HomeScore     int             `json:"homeScore"`
	AwayScore     int             `json:"awayScore"`
	MaxMatchCount int             `json:"maxMatchCount"` // 3 or 5 for bestOf
}

type naverTeam struct {
	Name        string `json:"name"`
	NameAcronym string `json:"nameAcronym"`
}

// FetchSchedule fetches LoL match schedule from Naver for a given month.
// month format: "2026-03"
func (n *NaverEsports) FetchSchedule(ctx context.Context, month string) ([]EsportsMatch, error) {
	url := "https://game.naver.com/esports/League_of_Legends/schedule?timestamp=" + month

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create naver esports request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ko-KR,ko;q=0.9")

	resp, err := n.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch naver esports: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("naver esports returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read naver esports response: %w", err)
	}

	html := string(body)

	// Extract __NEXT_DATA__ JSON from HTML.
	match := naverNextDataRe.FindStringSubmatch(html)
	if match == nil {
		return nil, fmt.Errorf("naver esports: __NEXT_DATA__ not found")
	}

	var nextData naverNextData
	if err := json.Unmarshal([]byte(match[1]), &nextData); err != nil {
		return nil, fmt.Errorf("naver esports parse __NEXT_DATA__: %w", err)
	}

	scheduleData := nextData.Props.PageProps.ScheduleData
	if len(scheduleData) == 0 {
		n.logger.Debug("naver esports: no schedule data found", "month", month)
		return nil, nil
	}

	var matches []EsportsMatch
	for _, m := range scheduleData {
		var status MatchStatus
		switch m.MatchStatus {
		case "RESULT":
			status = MatchFinished
		case "LIVE":
			status = MatchLive
		default:
			status = MatchScheduled
		}

		startTime := time.UnixMilli(m.StartDate)

		bestOf := m.MaxMatchCount
		if bestOf == 0 {
			bestOf = 3
		}

		matches = append(matches, EsportsMatch{
			ID:        m.GameID,
			Team1:     m.HomeTeam.Name,
			Team1Code: m.HomeTeam.NameAcronym,
			Team2:     m.AwayTeam.Name,
			Team2Code: m.AwayTeam.NameAcronym,
			Score1:    m.HomeScore,
			Score2:    m.AwayScore,
			BestOf:    bestOf,
			Status:    status,
			StartTime: startTime,
		})
	}

	n.logger.Debug("naver esports schedule fetched", "month", month, "matches", len(matches))
	return matches, nil
}
