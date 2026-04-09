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

// EsportsMatch represents a single LoL esports match.
type EsportsMatch struct {
	ID        string
	LeagueID  string // our internal league ID
	Team1     string // team name (Korean from API)
	Team1Code string // team abbreviation
	Team2     string
	Team2Code string
	Score1    int         // game wins for team1
	Score2    int         // game wins for team2
	BestOf    int         // 3 or 5
	Status    MatchStatus // scheduled, live, finished
	StartTime time.Time
	BlockName string // e.g. "13주 차"
}

// EsportsStanding represents a team's standing in a tournament.
type EsportsStanding struct {
	Rank     int
	TeamName string
	TeamCode string
	Wins     int
	Losses   int
}

// LoLEsports fetches match and standings data from the LoL Esports API.
type LoLEsports struct {
	client *BreakerHTTPClient
	logger *slog.Logger
	apiKey string
}

const (
	lolesportsBaseURL = "https://esports-api.lolesports.com/persisted/gw/"
	lolesportsAPIKey  = "0TvQnueqKa5mxJntVWt0w4LpLfEkrV1Ta8rQBb9Z"
)

// NewLoLEsports creates a new LoLEsports provider.
func NewLoLEsports(logger *slog.Logger) *LoLEsports {
	if logger == nil {
		logger = slog.Default()
	}
	return &LoLEsports{
		client: DefaultBreakerClient(10 * time.Second, "lol_esports", logger),
		logger: logger.With("component", "lol_esports"),
		apiKey: lolesportsAPIKey,
	}
}

// lolesports API response types

type lolesportsScheduleResponse struct {
	Data struct {
		Schedule struct {
			Events []lolesportsEvent `json:"events"`
		} `json:"schedule"`
	} `json:"data"`
}

type lolesportsEvent struct {
	StartTime string `json:"startTime"`
	State     string `json:"state"` // "completed", "inProgress", "unstarted"
	BlockName string `json:"blockName"`
	Match     struct {
		Teams    []lolesportsTeam `json:"teams"`
		Strategy struct {
			Type  string `json:"type"`  // "bestOf"
			Count int    `json:"count"` // 3 or 5
		} `json:"strategy"`
	} `json:"match"`
}

type lolesportsTeam struct {
	Name   string `json:"name"`
	Code   string `json:"code"`
	Result struct {
		Outcome  string `json:"outcome"` // "win", "loss", or empty
		GameWins int    `json:"gameWins"`
	} `json:"result"`
}

type lolesportsStandingsResponse struct {
	Data struct {
		Standings []struct {
			Stages []struct {
				Sections []struct {
					Rankings []lolesportsRanking `json:"rankings"`
				} `json:"sections"`
			} `json:"stages"`
		} `json:"standings"`
	} `json:"data"`
}

type lolesportsRanking struct {
	Ordinal int `json:"ordinal"`
	Teams   []struct {
		Name   string `json:"name"`
		Code   string `json:"code"`
		Record struct {
			Wins   int `json:"wins"`
			Losses int `json:"losses"`
		} `json:"record"`
	} `json:"teams"`
}

// FetchSchedule fetches match schedule for a given leagueId.
// Returns all matches (paginated), caller should filter by date.
func (l *LoLEsports) FetchSchedule(ctx context.Context, leagueID string) ([]EsportsMatch, error) {
	url := lolesportsBaseURL + "getSchedule?hl=ko-KR&leagueId=" + leagueID

	body, err := l.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("lol esports schedule: %w", err)
	}

	var resp lolesportsScheduleResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("lol esports schedule parse: %w", err)
	}

	return l.parseEvents(resp.Data.Schedule.Events), nil
}

// FetchLive fetches currently live matches across all leagues.
func (l *LoLEsports) FetchLive(ctx context.Context) ([]EsportsMatch, error) {
	url := lolesportsBaseURL + "getLive?hl=ko-KR"

	body, err := l.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("lol esports live: %w", err)
	}

	var resp lolesportsScheduleResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("lol esports live parse: %w", err)
	}

	return l.parseEvents(resp.Data.Schedule.Events), nil
}

// FetchStandings fetches standings for a given tournamentId.
func (l *LoLEsports) FetchStandings(ctx context.Context, tournamentID string) ([]EsportsStanding, error) {
	url := lolesportsBaseURL + "getStandings?hl=ko-KR&tournamentId=" + tournamentID

	body, err := l.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("lol esports standings: %w", err)
	}

	var resp lolesportsStandingsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("lol esports standings parse: %w", err)
	}

	standings := collectLoLEsportsStandings(resp)

	l.logger.Debug("lol esports standings fetched", "tournament_id", tournamentID, "teams", len(standings))
	return standings, nil
}

func collectLoLEsportsStandings(resp lolesportsStandingsResponse) []EsportsStanding {
	var standings []EsportsStanding
	for _, standing := range resp.Data.Standings {
		for _, stage := range standing.Stages {
			standings = append(standings, collectLoLEsportsStageStandings(stage)...)
		}
	}
	return standings
}

func collectLoLEsportsStageStandings(stage struct {
	Sections []struct {
		Rankings []lolesportsRanking `json:"rankings"`
	} `json:"sections"`
}) []EsportsStanding {
	var standings []EsportsStanding
	for _, section := range stage.Sections {
		for _, ranking := range section.Rankings {
			if len(ranking.Teams) == 0 {
				continue
			}
			team := ranking.Teams[0]
			standings = append(standings, EsportsStanding{
				Rank:     ranking.Ordinal,
				TeamName: team.Name,
				TeamCode: team.Code,
				Wins:     team.Record.Wins,
				Losses:   team.Record.Losses,
			})
		}
	}
	return standings
}

// parseEvents converts API events into EsportsMatch values.
func (l *LoLEsports) parseEvents(events []lolesportsEvent) []EsportsMatch {
	var matches []EsportsMatch
	for _, event := range events {
		if len(event.Match.Teams) < 2 {
			continue
		}

		team1 := event.Match.Teams[0]
		team2 := event.Match.Teams[1]

		var status MatchStatus
		switch event.State {
		case "completed":
			status = MatchFinished
		case "inProgress":
			status = MatchLive
		default:
			status = MatchScheduled
		}

		startTime, _ := time.Parse(time.RFC3339, event.StartTime)

		bestOf := event.Match.Strategy.Count
		if bestOf == 0 {
			bestOf = 3 // default
		}

		matches = append(matches, EsportsMatch{
			Team1:     team1.Name,
			Team1Code: team1.Code,
			Team2:     team2.Name,
			Team2Code: team2.Code,
			Score1:    team1.Result.GameWins,
			Score2:    team2.Result.GameWins,
			BestOf:    bestOf,
			Status:    status,
			StartTime: startTime,
			BlockName: event.BlockName,
		})
	}

	l.logger.Debug("lol esports events parsed", "matches", len(matches))
	return matches
}

func (l *LoLEsports) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", l.apiKey)
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
