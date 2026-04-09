package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// MLB fetches baseball data from the official MLB Stats API.
type MLB struct {
	client *BreakerHTTPClient
	logger *slog.Logger
}

// NewMLB creates a new MLB Stats API provider.
func NewMLB(logger *slog.Logger) *MLB {
	if logger == nil {
		logger = slog.Default()
	}
	return &MLB{
		client: DefaultBreakerClient(10 * time.Second, "mlb", logger),
		logger: logger.With("component", "mlb"),
	}
}

// FetchSchedule fetches MLB games for a given date (YYYY-MM-DD format).
// gameType: "R" (regular), "S" (spring training), "P" (postseason), etc.
func (m *MLB) FetchSchedule(ctx context.Context, date string, gameType string) ([]BaseballMatch, error) {
	url := fmt.Sprintf(
		"https://statsapi.mlb.com/api/v1/schedule?sportId=1&date=%s&hydrate=linescore,team&gameType=%s",
		date, gameType,
	)

	body, err := m.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("mlb schedule: %w", err)
	}

	var resp mlbScheduleResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("mlb schedule parse: %w", err)
	}

	var matches []BaseballMatch
	for _, d := range resp.Dates {
		for _, g := range d.Games {
			match := parseMLBGame(g)
			matches = append(matches, match)
		}
	}
	return matches, nil
}

func (m *MLB) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

func parseMLBGame(g mlbGame) BaseballMatch {
	match := BaseballMatch{
		ID:     fmt.Sprintf("mlb-%d", g.GamePk),
		League: "mlb",
	}

	// Teams
	if g.Teams.Away.Team.Name != "" {
		match.AwayTeam = g.Teams.Away.Team.Name
	}
	if g.Teams.Home.Team.Name != "" {
		match.HomeTeam = g.Teams.Home.Team.Name
	}
	match.AwayScore = g.Teams.Away.Score
	match.HomeScore = g.Teams.Home.Score

	// Status
	match.Status = mapMLBStatus(g.Status.StatusCode)

	// Linescore (inning info)
	if g.Linescore.CurrentInning > 0 {
		match.Inning = g.Linescore.CurrentInning
		if g.Linescore.IsTopInning {
			match.Half = InningTop
		} else {
			match.Half = InningBottom
		}
	}

	// Start time
	if t, err := time.Parse(time.RFC3339, g.GameDate); err == nil {
		match.StartTime = t
	}

	return match
}

func mapMLBStatus(code string) BaseballMatchStatus {
	switch code {
	case "S", "P": // Scheduled, Preview
		return BaseballScheduled
	case "I", "MA", "MB", "MC": // In Progress, Manager challenge variants
		return BaseballLive
	case "F", "FO", "FT", "FR": // Final, Completed Early, Final: Tied, Final: Replay
		return BaseballFinished
	case "DR", "DI", "DC": // Postponed variants
		return BaseballCancelled
	default:
		return BaseballScheduled
	}
}

// --- JSON response types ---

type mlbScheduleResponse struct {
	Dates []mlbDate `json:"dates"`
}

type mlbDate struct {
	Games []mlbGame `json:"games"`
}

type mlbGame struct {
	GamePk   int    `json:"gamePk"`
	GameDate string `json:"gameDate"`
	Status   struct {
		StatusCode string `json:"statusCode"`
	} `json:"status"`
	Teams struct {
		Away mlbTeamEntry `json:"away"`
		Home mlbTeamEntry `json:"home"`
	} `json:"teams"`
	Linescore mlbLinescore `json:"linescore"`
}

type mlbTeamEntry struct {
	Team struct {
		Name string `json:"name"`
	} `json:"team"`
	Score int `json:"score"`
}

type mlbLinescore struct {
	CurrentInning int  `json:"currentInning"`
	IsTopInning   bool `json:"isTopInning"`
}
