package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ErrBudgetExhausted is returned when the daily API-Football call budget is exhausted.
var ErrBudgetExhausted = errors.New("api-football: daily budget exhausted")

const apiFootballDailyLimit = 100

// APIFootball fetches detailed match events from API-Football (RapidAPI).
type APIFootball struct {
	client *BreakerHTTPClient
	logger    *slog.Logger
	apiKey    string
	mu        sync.Mutex
	dailyUsed int
	lastReset time.Time // UTC date of last reset
}

// NewAPIFootball creates a new APIFootball provider.
func NewAPIFootball(apiKey string, logger *slog.Logger) *APIFootball {
	if logger == nil {
		logger = slog.Default()
	}
	return &APIFootball{
		client:    DefaultBreakerClient(10 * time.Second, "api_football", logger),
		logger:    logger.With("component", "api_football"),
		apiKey:    apiKey,
		lastReset: time.Now().UTC(),
	}
}

// apiFootballResponse is the top-level API-Football response.
type apiFootballResponse struct {
	Response []apiFootballEvent `json:"response"`
}

type apiFootballEvent struct {
	Time   apiFootballTime   `json:"time"`
	Team   apiFootballTeam   `json:"team"`
	Player apiFootballPlayer `json:"player"`
	Assist apiFootballPlayer `json:"assist"`
	Type   string            `json:"type"`
	Detail string            `json:"detail"`
}

type apiFootballTime struct {
	Elapsed int  `json:"elapsed"`
	Extra   *int `json:"extra"`
}

type apiFootballTeam struct {
	Name string `json:"name"`
}

type apiFootballPlayer struct {
	Name string `json:"name"`
}

// FetchEvents fetches detailed events for a specific match/fixture.
// fixtureID is the API-Football fixture identifier.
// Returns events (goals with assists, cards) or budget exhausted error.
func (a *APIFootball) FetchEvents(ctx context.Context, fixtureID int) ([]MatchEvent, error) {
	if err := a.consumeBudget(); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("https://v3.football.api-sports.io/fixtures/events?fixture=%d", fixtureID)

	body, err := a.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("api-football events: %w", err)
	}

	var resp apiFootballResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("api-football events parse: %w", err)
	}

	var events []MatchEvent
	for _, e := range resp.Response {
		eventType, ok := mapAPIFootballEventType(e.Type, e.Detail)
		if !ok {
			continue
		}

		minute := formatMinute(e.Time.Elapsed, e.Time.Extra)

		events = append(events, MatchEvent{
			Type:   eventType,
			Player: e.Player.Name,
			Assist: e.Assist.Name,
			Minute: minute,
			Team:   e.Team.Name,
		})
	}

	a.logger.Debug("api-football events fetched", "fixture", fixtureID, "events", len(events))
	return events, nil
}

// FixtureInfo holds a fixture ID and team names from API-Football.
type FixtureInfo struct {
	ID       int
	HomeTeam string
	AwayTeam string
}

// FetchFixtures fetches fixtures for a league and date from API-Football.
// date format: "YYYY-MM-DD", leagueID: API-Football league ID, season: year (e.g. 2026)
func (a *APIFootball) FetchFixtures(ctx context.Context, date string, leagueID int, season int) ([]FixtureInfo, error) {
	if a.apiKey == "" {
		return nil, nil
	}
	if err := a.consumeBudget(); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("https://v3.football.api-sports.io/fixtures?date=%s&league=%d&season=%d", date, leagueID, season)

	body, err := a.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("api-football fixtures: %w", err)
	}

	var resp struct {
		Response []struct {
			Fixture struct {
				ID int `json:"id"`
			} `json:"fixture"`
			Teams struct {
				Home struct{ Name string `json:"name"` } `json:"home"`
				Away struct{ Name string `json:"name"` } `json:"away"`
			} `json:"teams"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("api-football fixtures parse: %w", err)
	}

	fixtures := make([]FixtureInfo, 0, len(resp.Response))
	for _, f := range resp.Response {
		fixtures = append(fixtures, FixtureInfo{
			ID:       f.Fixture.ID,
			HomeTeam: f.Teams.Home.Name,
			AwayTeam: f.Teams.Away.Name,
		})
	}

	a.logger.Debug("api-football fixtures fetched", "date", date, "league", leagueID, "count", len(fixtures))
	return fixtures, nil
}

// FetchEventsForMatch finds the fixture ID by matching team names, then fetches events.
// This is for cases where we have Livescore match data but need API-Football events.
func (a *APIFootball) FetchEventsForMatch(ctx context.Context, date string, leagueID int, season int, homeTeam, awayTeam string) ([]MatchEvent, error) {
	if a.apiKey == "" {
		return nil, nil
	}

	fixtures, err := a.FetchFixtures(ctx, date, leagueID, season)
	if err != nil {
		return nil, err
	}

	// Find matching fixture by team name similarity
	for _, f := range fixtures {
		if TeamNameMatch(f.HomeTeam, homeTeam) && TeamNameMatch(f.AwayTeam, awayTeam) {
			return a.FetchEvents(ctx, f.ID)
		}
	}

	return nil, nil // no matching fixture found
}

// TeamNameMatch checks if two team names refer to the same team.
// Uses multiple strategies: exact, substring, first-word, and Korean translation matching.
func TeamNameMatch(name1, name2 string) bool {
	a := strings.ToLower(strings.TrimSpace(name1))
	b := strings.ToLower(strings.TrimSpace(name2))
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	// Substring match
	if strings.Contains(a, b) || strings.Contains(b, a) {
		return true
	}
	// First word match (e.g. "Jeonbuk Hyundai Motors" vs "Jeonbuk FC" → both start with "jeonbuk")
	aFirst := strings.Fields(a)[0]
	bFirst := strings.Fields(b)[0]
	if len(aFirst) >= 3 && aFirst == bFirst {
		return true
	}
	// Korean translation match (both resolve to same Korean name)
	aKR := TranslateTeamName(name1)
	bKR := TranslateTeamName(name2)
	if aKR == bKR && aKR != name1 {
		return true
	}
	return false
}

// DailyBudgetRemaining returns remaining daily API calls.
func (a *APIFootball) DailyBudgetRemaining() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.resetIfNewDay()
	remaining := apiFootballDailyLimit - a.dailyUsed
	if remaining < 0 {
		return 0
	}
	return remaining
}

// consumeBudget checks and consumes one unit of the daily budget.
func (a *APIFootball) consumeBudget() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.resetIfNewDay()
	if a.dailyUsed >= apiFootballDailyLimit {
		return ErrBudgetExhausted
	}
	a.dailyUsed++
	return nil
}

// resetIfNewDay resets the daily counter if the UTC date has changed.
// Must be called with mu held.
func (a *APIFootball) resetIfNewDay() {
	now := time.Now().UTC()
	if now.Year() != a.lastReset.Year() ||
		now.YearDay() != a.lastReset.YearDay() {
		a.dailyUsed = 0
		a.lastReset = now
	}
}

// mapAPIFootballEventType maps API-Football type+detail to our EventType.
func mapAPIFootballEventType(typ, detail string) (EventType, bool) {
	switch {
	case typ == "Goal" && detail == "Normal Goal":
		return EventGoal, true
	case typ == "Goal" && detail == "Penalty":
		return EventPenalty, true
	case typ == "Goal" && detail == "Own Goal":
		return EventOwnGoal, true
	case typ == "Card" && detail == "Red Card":
		return EventRedCard, true
	case typ == "Card" && detail == "Yellow Card":
		return EventYellowCard, true
	default:
		return "", false
	}
}

// formatMinute formats elapsed + extra into a display string like "36'" or "90'+3'".
func formatMinute(elapsed int, extra *int) string {
	if extra != nil && *extra > 0 {
		return fmt.Sprintf("%d'+%d'", elapsed, *extra)
	}
	return fmt.Sprintf("%d'", elapsed)
}

func (a *APIFootball) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-apisports-key", a.apiKey)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	return io.ReadAll(resp.Body)
}
