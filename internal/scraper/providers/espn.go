package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// MatchStatus represents the state of a football match.
type MatchStatus string

const (
	MatchScheduled MatchStatus = "scheduled"
	MatchLive      MatchStatus = "live"
	MatchFinished  MatchStatus = "finished"
)

// EventType represents the type of a match event.
type EventType string

const (
	EventGoal       EventType = "goal"
	EventPenalty    EventType = "penalty"
	EventOwnGoal    EventType = "own_goal"
	EventRedCard    EventType = "red_card"
	EventYellowCard EventType = "yellow_card"
)

// FootballMatch represents a single football match with scores, events, and odds.
type FootballMatch struct {
	ID           string
	HomeTeam     string
	AwayTeam     string
	HomeScore    int
	AwayScore    int
	Status       MatchStatus
	StatusDetail string // e.g. "45'" for live, "FT" for finished
	StartTime    time.Time
	Events       []MatchEvent
	OddsHome     float64 // decimal odds, 0 if unavailable
	OddsDraw     float64
	OddsAway     float64
	League       string // league ID from our registry
}

// MatchEvent represents a notable event during a match.
type MatchEvent struct {
	Type   EventType
	Player string
	Assist string // empty if no assist or unknown
	Minute string // e.g. "36'", "90'+3'"
	Team   string
}

// ESPN fetches football scoreboard data from ESPN's public API.
type ESPN struct {
	client *BreakerHTTPClient
	logger *slog.Logger
}

// NewESPN creates a new ESPN provider.
func NewESPN(logger *slog.Logger) *ESPN {
	if logger == nil {
		logger = slog.Default()
	}
	return &ESPN{
		client: DefaultBreakerClient(10 * time.Second, "espn", logger),
		logger: logger.With("component", "espn"),
	}
}

// espnScoreboard is the top-level ESPN API response.
type espnScoreboard struct {
	Events []espnEvent `json:"events"`
}

type espnEvent struct {
	ID           string            `json:"id"`
	Competitions []espnCompetition `json:"competitions"`
}

type espnCompetition struct {
	Competitors []espnCompetitor `json:"competitors"`
	Status      espnStatus       `json:"status"`
	Odds        []espnOdds       `json:"odds"`
	Details     []espnDetail     `json:"details"`
	StartDate   string           `json:"startDate"`
}

type espnCompetitor struct {
	HomeAway string   `json:"homeAway"`
	Team     espnTeam `json:"team"`
	Score    string   `json:"score"`
}

type espnTeam struct {
	DisplayName string `json:"displayName"`
}

type espnStatus struct {
	Type espnStatusType `json:"type"`
}

type espnStatusType struct {
	State  string `json:"state"`  // "pre", "in", "post"
	Detail string `json:"detail"` // e.g. "45'", "FT"
}

type espnOdds struct {
	Moneyline *espnMoneyline `json:"moneyline"`
}

type espnMoneyline struct {
	Home json.RawMessage `json:"home"`
	Away json.RawMessage `json:"away"`
	Draw json.RawMessage `json:"draw"`
}

type espnMoneylineValue struct {
	Value float64 `json:"value"`
}

type espnDetail struct {
	Type             espnDetailType `json:"type"`
	Clock            espnClock      `json:"clock"`
	Team             espnDetailTeam `json:"team"`
	YellowCard       bool           `json:"yellowCard"`
	RedCard          bool           `json:"redCard"`
	OwnGoal          bool           `json:"ownGoal"`
	PenaltyKick      bool           `json:"penaltyKick"`
	AthletesInvolved []espnAthlete  `json:"athletesInvolved"`
}

type espnDetailType struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type espnClock struct {
	DisplayValue string `json:"displayValue"`
}

type espnDetailTeam struct {
	DisplayName string `json:"displayName"`
}

type espnAthlete struct {
	DisplayName string `json:"displayName"`
}

// FetchScoreboard fetches matches for a given ESPN league slug and date.
// slug examples: "eng.1", "esp.1", "ita.1", "ger.1", "uefa.champions"
// date format: "20260318"
func (e *ESPN) FetchScoreboard(ctx context.Context, slug string, date string) ([]FootballMatch, error) {
	url := fmt.Sprintf("https://site.api.espn.com/apis/site/v2/sports/soccer/%s/scoreboard?dates=%s", slug, date)

	body, err := e.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("espn scoreboard: %w", err)
	}

	var scoreboard espnScoreboard
	if err := json.Unmarshal(body, &scoreboard); err != nil {
		return nil, fmt.Errorf("espn scoreboard parse: %w", err)
	}

	matches := make([]FootballMatch, 0, len(scoreboard.Events))
	for _, event := range scoreboard.Events {
		match, ok := parseESPNEvent(event)
		if !ok {
			continue
		}
		matches = append(matches, match)
	}

	e.logger.Debug("espn scoreboard fetched", "slug", slug, "date", date, "matches", len(matches))
	return matches, nil
}

// parseMoneylineOdds extracts an American odds value from a JSON raw message
// and converts it to decimal odds.
// ESPN moneyline structure: {"open": {"odds": "+215"}, "close": {"odds": "+210", ...}}
// or simpler: {"value": 150} or just a number or string.
func parseMoneylineOdds(raw json.RawMessage) float64 {
	if len(raw) == 0 {
		return 0
	}
	if v, ok := parseMoneylineFromNested(raw); ok {
		return americanToDecimal(v)
	}
	if v, ok := parseMoneylineFromObjectValue(raw); ok {
		return americanToDecimal(v)
	}
	if v, ok := parseMoneylineFromNumber(raw); ok {
		return americanToDecimal(v)
	}
	if v, ok := parseMoneylineFromString(raw); ok {
		return americanToDecimal(v)
	}
	return 0
}

func parseESPNEvent(event espnEvent) (FootballMatch, bool) {
	if len(event.Competitions) == 0 {
		return FootballMatch{}, false
	}
	comp := event.Competitions[0]
	match := FootballMatch{ID: event.ID}

	applyESPNCompetitors(&match, comp.Competitors)
	match.Status = mapESPNStatus(comp.Status.Type.State)
	match.StatusDetail = comp.Status.Type.Detail
	match.StartTime = parseESPNStartTime(comp.StartDate)
	applyESPNOdds(&match, comp.Odds)
	applyESPNDetails(&match, comp.Details)
	return match, true
}

func applyESPNCompetitors(match *FootballMatch, competitors []espnCompetitor) {
	for _, competitor := range competitors {
		score, _ := strconv.Atoi(competitor.Score)
		switch competitor.HomeAway {
		case "home":
			match.HomeTeam = competitor.Team.DisplayName
			match.HomeScore = score
		case "away":
			match.AwayTeam = competitor.Team.DisplayName
			match.AwayScore = score
		}
	}
}

func mapESPNStatus(state string) MatchStatus {
	switch state {
	case "in":
		return MatchLive
	case "post":
		return MatchFinished
	default:
		return MatchScheduled
	}
}

func parseESPNStartTime(startDate string) time.Time {
	for _, layout := range espnTimeLayouts() {
		parsed, err := time.Parse(layout, startDate)
		if err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func espnTimeLayouts() []string {
	return []string{
		time.RFC3339,
		"2006-01-02T15:04Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z",
	}
}

func applyESPNOdds(match *FootballMatch, odds []espnOdds) {
	if len(odds) == 0 || odds[0].Moneyline == nil {
		return
	}
	moneyline := odds[0].Moneyline
	match.OddsHome = parseMoneylineOdds(moneyline.Home)
	match.OddsDraw = parseMoneylineOdds(moneyline.Draw)
	match.OddsAway = parseMoneylineOdds(moneyline.Away)
}

func applyESPNDetails(match *FootballMatch, details []espnDetail) {
	for _, detail := range details {
		event := parseESPNDetail(detail)
		if event == nil {
			continue
		}
		match.Events = append(match.Events, *event)
	}
}

func parseMoneylineFromNested(raw json.RawMessage) (float64, bool) {
	var nested struct {
		Close struct {
			Odds string `json:"odds"`
		} `json:"close"`
		Open struct {
			Odds string `json:"odds"`
		} `json:"open"`
	}
	if err := json.Unmarshal(raw, &nested); err != nil {
		return 0, false
	}
	oddsText := nested.Close.Odds
	if oddsText == "" {
		oddsText = nested.Open.Odds
	}
	return parseMoneylineText(oddsText)
}

func parseMoneylineFromObjectValue(raw json.RawMessage) (float64, bool) {
	var obj espnMoneylineValue
	if err := json.Unmarshal(raw, &obj); err != nil || obj.Value == 0 {
		return 0, false
	}
	return obj.Value, true
}

func parseMoneylineFromNumber(raw json.RawMessage) (float64, bool) {
	var num float64
	if err := json.Unmarshal(raw, &num); err != nil || num == 0 {
		return 0, false
	}
	return num, true
}

func parseMoneylineFromString(raw json.RawMessage) (float64, bool) {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, false
	}
	return parseMoneylineText(text)
}

func parseMoneylineText(text string) (float64, bool) {
	if text == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(strings.TrimPrefix(text, "+"), 64)
	if err != nil || value == 0 {
		return 0, false
	}
	return value, true
}

// americanToDecimal converts American moneyline odds to decimal odds.
// Positive +N -> (N/100) + 1, Negative -N -> (100/N) + 1.
func americanToDecimal(american float64) float64 {
	if american >= 0 {
		return (american / 100) + 1
	}
	return (100 / math.Abs(american)) + 1
}

// parseESPNDetail converts an ESPN detail entry into a MatchEvent.
// Returns nil if the detail is not a recognized event type.
func parseESPNDetail(detail espnDetail) *MatchEvent {
	var eventType EventType

	switch {
	case detail.RedCard:
		eventType = EventRedCard
	case detail.YellowCard:
		eventType = EventYellowCard
	case detail.OwnGoal:
		eventType = EventOwnGoal
	case detail.PenaltyKick:
		eventType = EventPenalty
	case detail.Type.ID == "70": // scoring play
		eventType = EventGoal
	default:
		return nil
	}

	player := ""
	if len(detail.AthletesInvolved) > 0 {
		player = detail.AthletesInvolved[0].DisplayName
	}

	return &MatchEvent{
		Type:   eventType,
		Player: player,
		Assist: "", // ESPN does NOT provide assists in its details
		Minute: detail.Clock.DisplayValue,
		Team:   detail.Team.DisplayName,
	}
}

func (e *ESPN) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	return io.ReadAll(resp.Body)
}
