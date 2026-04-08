package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Livescore fetches football match data from Livescore's public CDN API.
type Livescore struct {
	client *BreakerHTTPClient
	logger *slog.Logger
}

// NewLivescore creates a new Livescore provider.
func NewLivescore(logger *slog.Logger) *Livescore {
	if logger == nil {
		logger = slog.Default()
	}
	return &Livescore{
		client: DefaultBreakerClient(10 * time.Second, "livescore", logger),
		logger: logger.With("component", "livescore"),
	}
}

// livescoreResponse is the top-level Livescore API response.
type livescoreResponse struct {
	Stages []livescoreStage `json:"Stages"`
}

type livescoreStage struct {
	Snm    string           `json:"Snm"` // stage/league name
	Events []livescoreEvent `json:"Events"`
}

type livescoreEvent struct {
	T1  []livescoreTeam `json:"T1"`
	T2  []livescoreTeam `json:"T2"`
	Tr1 string          `json:"Tr1"` // home score
	Tr2 string          `json:"Tr2"` // away score
	Eps string          `json:"Eps"` // status: "NS", "FT", "HT", "'45", "45'", etc.
	Esd json.RawMessage `json:"Esd"` // start time - can be string or number
	Eid string          `json:"Eid"` // event ID
}

type livescoreTeam struct {
	Nm string `json:"Nm"` // team name
}

// FetchMatches fetches all football matches for a given date, optionally filtered by league name.
// date format: "20260318"
// leagueFilter: if non-empty, only return matches from stages matching this name (e.g. "K-League 1")
func (l *Livescore) FetchMatches(ctx context.Context, date string, leagueFilter string) ([]FootballMatch, error) {
	url := fmt.Sprintf("https://prod-cdn-public-api.livescore.com/v1/api/app/date/soccer/%s/1", date)

	body, err := l.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("livescore fetch: %w", err)
	}

	var resp livescoreResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("livescore parse: %w", err)
	}

	var matches []FootballMatch
	for _, stage := range resp.Stages {
		if !shouldIncludeLivescoreStage(stage.Snm, leagueFilter) {
			continue
		}
		matches = append(matches, parseLivescoreStageMatches(stage)...)
	}

	l.logger.Debug("livescore matches fetched", "date", date, "filter", leagueFilter, "matches", len(matches))
	return matches, nil
}

// parseLivescoreStatus maps a Livescore Eps value to MatchStatus and a display detail.
func parseLivescoreStatus(eps string) (MatchStatus, string) {
	normalized := strings.TrimSpace(eps)
	switch strings.ToUpper(normalized) {
	case "NS":
		return MatchScheduled, "NS"
	case "FT", "AET", "AP":
		return MatchFinished, normalized
	case "HT":
		return MatchLive, "HT"
	case "":
		return MatchScheduled, ""
	default:
		// Any other value (e.g. "'45", "45'", "90'+3") indicates live play.
		return MatchLive, normalized
	}
}

// livescoreTZ is CET (Central European Time, UTC+1).
// Livescore Esd timestamps are in CET, not UTC.
var livescoreTZ = time.FixedZone("CET", 1*60*60)

// parseLivescoreTime parses a Livescore Esd value into time.Time.
// Esd can be a JSON string "YYYYMMDDHHmmss" or a JSON number (same format as int).
// Times are in CET (UTC+1).
func parseLivescoreTime(raw json.RawMessage) time.Time {
	// Try as string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && len(s) >= 14 {
		t, err := time.ParseInLocation("20060102150405", s[:14], livescoreTZ)
		if err == nil {
			return t
		}
	}

	// Try as number (e.g. 20260318193000).
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil && n > 0 {
		s := strconv.FormatInt(n, 10)
		if len(s) >= 14 {
			t, err := time.ParseInLocation("20060102150405", s[:14], livescoreTZ)
			if err == nil {
				return t
			}
		}
	}

	return time.Time{}
}

// FetchMatchEvents fetches detailed events (goals, assists, cards) for a specific match.
// Uses the Livescore scoreboard endpoint which provides Incs-s (incidents) data.
func (l *Livescore) FetchMatchEvents(ctx context.Context, matchID string) ([]MatchEvent, error) {
	url := fmt.Sprintf("https://prod-cdn-public-api.livescore.com/v1/api/app/scoreboard/soccer/%s", matchID)

	body, err := l.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("livescore scoreboard: %w", err)
	}

	var resp livescoreScoreboard
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("livescore scoreboard parse: %w", err)
	}

	// Get team names for mapping
	homeName := ""
	awayName := ""
	if len(resp.T1) > 0 {
		homeName = resp.T1[0].Nm
	}
	if len(resp.T2) > 0 {
		awayName = resp.T2[0].Nm
	}

	allGroups := collectLivescoreScoredGroups(resp)
	events := make([]MatchEvent, 0, len(allGroups))
	prevSc := []int{0, 0}
	for _, group := range allGroups {
		team := inferScoringTeam(homeName, awayName, prevSc, group.sc)
		events = append(events, livescoreGroupEvents(group, team)...)
		if len(group.sc) == 2 {
			prevSc = group.sc
		}
	}

	l.logger.Debug("livescore events fetched", "matchID", matchID, "events", len(events))
	return events, nil
}

type livescoreScoreboard struct {
	T1   []livescoreTeam                     `json:"T1"`
	T2   []livescoreTeam                     `json:"T2"`
	Incs map[string][]livescoreIncidentGroup `json:"Incs-s"`
}

type livescoreIncidentGroup struct {
	Min  int                 `json:"Min"`
	Sc   []int               `json:"Sc"`
	Incs []livescoreIncident `json:"Incs"`
}

type livescoreIncident struct {
	Min int    `json:"Min"`
	IT  int    `json:"IT"` // incident type: 36=goal, 37=own_goal, 39=penalty, 17=yellow, 19=red, 63=assist
	Pn  string `json:"Pn"` // player name
	Sc  []int  `json:"Sc"` // score after this incident [home, away]
}

type scoredGroup struct {
	min  int
	sc   []int
	incs []livescoreIncident
}

func shouldIncludeLivescoreStage(stageName, leagueFilter string) bool {
	if leagueFilter == "" {
		return true
	}
	return strings.EqualFold(stageName, leagueFilter)
}

func parseLivescoreStageMatches(stage livescoreStage) []FootballMatch {
	matches := make([]FootballMatch, 0, len(stage.Events))
	for _, event := range stage.Events {
		matches = append(matches, parseLivescoreEvent(stage.Snm, event))
	}
	return matches
}

func parseLivescoreEvent(stageName string, event livescoreEvent) FootballMatch {
	homeScore, _ := strconv.Atoi(event.Tr1)
	awayScore, _ := strconv.Atoi(event.Tr2)
	status, detail := parseLivescoreStatus(event.Eps)

	match := FootballMatch{
		ID:           event.Eid,
		League:       stageName,
		HomeTeam:     livescoreTeamName(event.T1),
		AwayTeam:     livescoreTeamName(event.T2),
		HomeScore:    homeScore,
		AwayScore:    awayScore,
		Status:       status,
		StatusDetail: detail,
	}
	if len(event.Esd) > 0 {
		match.StartTime = parseLivescoreTime(event.Esd)
	}
	return match
}

func livescoreTeamName(teams []livescoreTeam) string {
	if len(teams) == 0 {
		return ""
	}
	return teams[0].Nm
}

func collectLivescoreScoredGroups(resp livescoreScoreboard) []scoredGroup {
	var groups []scoredGroup
	for _, periodEvents := range resp.Incs {
		for _, group := range periodEvents {
			groups = append(groups, scoredGroup{
				min:  group.Min,
				sc:   group.Sc,
				incs: group.Incs,
			})
		}
	}
	return groups
}

func inferScoringTeam(homeName, awayName string, prevScore, currentScore []int) string {
	team := homeName
	if len(currentScore) == 2 && len(prevScore) == 2 && currentScore[1] > prevScore[1] {
		team = awayName
	}
	return team
}

func livescoreGroupEvents(group scoredGroup, team string) []MatchEvent {
	if len(group.incs) > 0 {
		return detailedLivescoreEvents(group, team)
	}
	if group.min <= 0 {
		return nil
	}
	return []MatchEvent{{
		Type:   EventGoal,
		Minute: fmt.Sprintf("%d'", group.min),
		Team:   team,
	}}
}

func detailedLivescoreEvents(group scoredGroup, team string) []MatchEvent {
	assistMap := make(map[int]string)
	for _, incident := range group.incs {
		if incident.IT == 63 {
			assistMap[incident.Min] = incident.Pn
		}
	}
	events := make([]MatchEvent, 0, len(group.incs))
	for _, incident := range group.incs {
		eventType, ok := mapLivescoreIncidentType(incident.IT)
		if !ok {
			continue
		}
		events = append(events, MatchEvent{
			Type:   eventType,
			Player: incident.Pn,
			Assist: assistMap[incident.Min],
			Minute: fmt.Sprintf("%d'", incident.Min),
			Team:   team,
		})
	}
	return events
}

func mapLivescoreIncidentType(it int) (EventType, bool) {
	switch it {
	case 36:
		return EventGoal, true
	case 37:
		return EventOwnGoal, true
	case 39:
		return EventPenalty, true
	case 17:
		return EventYellowCard, true
	case 19:
		return EventRedCard, true
	default:
		return "", false
	}
}

func (l *Livescore) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")

	resp, err := l.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	return io.ReadAll(resp.Body)
}
