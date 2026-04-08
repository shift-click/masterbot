package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

// KBO fetches baseball data from Naver Sports API.
type KBO struct {
	client *BreakerHTTPClient
	logger *slog.Logger
}

// NewKBO creates a new KBO (Naver Sports) provider.
func NewKBO(logger *slog.Logger) *KBO {
	if logger == nil {
		logger = slog.Default()
	}
	return &KBO{
		client: DefaultBreakerClient(10 * time.Second, "kbo", logger),
		logger: logger.With("component", "kbo"),
	}
}

// FetchSchedule fetches KBO games for a given date (YYYY-MM-DD format).
func (k *KBO) FetchSchedule(ctx context.Context, date string) ([]BaseballMatch, error) {
	url := fmt.Sprintf(
		"https://api-gw.sports.naver.com/schedule/games?fields=basic,currentInning&upperCategoryId=kbaseball&date=%s",
		date,
	)

	body, err := k.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("kbo schedule: %w", err)
	}

	var resp naverScheduleResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("kbo schedule parse: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("kbo schedule: API returned success=false")
	}

	var matches []BaseballMatch
	for _, g := range resp.Result.Games {
		if g.CategoryID != "kbo" {
			continue // skip non-KBO entries (e.g. kbaseballetc)
		}
		match := parseNaverGame(g)
		matches = append(matches, match)
	}
	return matches, nil
}

func (k *KBO) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")

	resp, err := k.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

var kboInningRegex = regexp.MustCompile(`(\d+)회(초|말)`)

func parseNaverGame(g naverGame) BaseballMatch {
	match := BaseballMatch{
		ID:        g.GameID,
		League:    "kbo",
		HomeTeam:  g.HomeTeamName, // Already in Korean
		AwayTeam:  g.AwayTeamName, // Already in Korean
		HomeScore: g.HomeTeamScore,
		AwayScore: g.AwayTeamScore,
	}

	// Status mapping
	if g.Cancel {
		match.Status = BaseballCancelled
	} else {
		match.Status = mapKBOStatus(g.StatusCode)
	}

	// Parse inning from currentInning field (e.g. "5회초", "7회말")
	if g.CurrentInning != "" {
		if m := kboInningRegex.FindStringSubmatch(g.CurrentInning); len(m) == 3 {
			match.Inning, _ = strconv.Atoi(m[1])
			if m[2] == "초" {
				match.Half = InningTop
			} else {
				match.Half = InningBottom
			}
		}
	}

	// Start time
	if t, err := time.Parse("2006-01-02T15:04:05", g.GameDateTime); err == nil {
		loc, _ := time.LoadLocation("Asia/Seoul")
		if loc == nil {
			loc = time.FixedZone("KST", 9*60*60)
		}
		match.StartTime = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc)
	}

	return match
}

func mapKBOStatus(code string) BaseballMatchStatus {
	switch code {
	case "BEFORE":
		return BaseballScheduled
	case "PLAY":
		return BaseballLive
	case "RESULT":
		return BaseballFinished
	case "CANCEL":
		return BaseballCancelled
	default:
		return BaseballScheduled
	}
}

// --- JSON response types ---

type naverScheduleResponse struct {
	Code    int  `json:"code"`
	Success bool `json:"success"`
	Result  struct {
		Games []naverGame `json:"games"`
	} `json:"result"`
}

type naverGame struct {
	GameID         string `json:"gameId"`
	CategoryID     string `json:"categoryId"`
	GameDate       string `json:"gameDate"`
	GameDateTime   string `json:"gameDateTime"`
	HomeTeamCode   string `json:"homeTeamCode"`
	HomeTeamName   string `json:"homeTeamName"`
	HomeTeamScore  int    `json:"homeTeamScore"`
	AwayTeamCode   string `json:"awayTeamCode"`
	AwayTeamName   string `json:"awayTeamName"`
	AwayTeamScore  int    `json:"awayTeamScore"`
	StatusCode     string `json:"statusCode"`
	StatusInfo     string `json:"statusInfo"`
	Cancel         bool   `json:"cancel"`
	CurrentInning  string `json:"currentInning"`
}
