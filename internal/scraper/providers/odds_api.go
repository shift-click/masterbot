package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// OddsAPI fetches 1X2 betting odds from The Odds API.
type OddsAPI struct {
	client *BreakerHTTPClient
	logger    *slog.Logger
	apiKey    string
	mu        sync.RWMutex
	remaining int // remaining monthly credits from response header
}

// NewOddsAPI creates a new OddsAPI provider.
func NewOddsAPI(apiKey string, logger *slog.Logger) *OddsAPI {
	if logger == nil {
		logger = slog.Default()
	}
	return &OddsAPI{
		client:    DefaultBreakerClient(10 * time.Second, "odds_api", logger),
		logger:    logger.With("component", "odds_api"),
		apiKey:    apiKey,
		remaining: -1, // unknown until first response
	}
}

// MatchOdds holds odds for a single match.
type MatchOdds struct {
	HomeTeam  string
	AwayTeam  string
	OddsHome  float64
	OddsDraw  float64
	OddsAway  float64
	StartTime time.Time
}

// oddsAPIEvent is a single event in the API response.
type oddsAPIEvent struct {
	HomeTeam     string             `json:"home_team"`
	AwayTeam     string             `json:"away_team"`
	CommenceTime string             `json:"commence_time"`
	Bookmakers   []oddsAPIBookmaker `json:"bookmakers"`
}

type oddsAPIBookmaker struct {
	Markets []oddsAPIMarket `json:"markets"`
}

type oddsAPIMarket struct {
	Key      string           `json:"key"`
	Outcomes []oddsAPIOutcome `json:"outcomes"`
}

type oddsAPIOutcome struct {
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

// FetchOdds fetches 1X2 odds for a given sport key.
// sportKey examples: "soccer_korea_kleague1", "soccer_epl"
func (o *OddsAPI) FetchOdds(ctx context.Context, sportKey string) ([]MatchOdds, error) {
	if o.apiKey == "" {
		return nil, nil
	}

	url := fmt.Sprintf(
		"https://api.the-odds-api.com/v4/sports/%s/odds?regions=uk&markets=h2h&oddsFormat=decimal&apiKey=%s",
		sportKey, o.apiKey,
	)

	body, remaining, err := o.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("odds-api: %w", err)
	}

	// Update remaining credits from response header.
	if remaining >= 0 {
		o.mu.Lock()
		o.remaining = remaining
		o.mu.Unlock()
	}

	var events []oddsAPIEvent
	if err := json.Unmarshal(body, &events); err != nil {
		return nil, fmt.Errorf("odds-api parse: %w", err)
	}

	odds := make([]MatchOdds, 0, len(events))
	for _, event := range events {
		odds = append(odds, buildMatchOdds(event))
	}

	o.logger.Debug("odds-api fetched", "sport", sportKey, "matches", len(odds))
	return odds, nil
}

func buildMatchOdds(event oddsAPIEvent) MatchOdds {
	odds := MatchOdds{
		HomeTeam: event.HomeTeam,
		AwayTeam: event.AwayTeam,
	}
	odds.StartTime = parseOddsCommenceTime(event.CommenceTime)
	applyH2HOdds(&odds, event)
	return odds
}

func parseOddsCommenceTime(commenceTime string) time.Time {
	parsed, err := time.Parse(time.RFC3339, commenceTime)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func applyH2HOdds(match *MatchOdds, event oddsAPIEvent) {
	h2h := findH2HMarket(event.Bookmakers)
	if h2h == nil {
		return
	}
	for _, outcome := range h2h.Outcomes {
		switch outcome.Name {
		case event.HomeTeam:
			match.OddsHome = outcome.Price
		case event.AwayTeam:
			match.OddsAway = outcome.Price
		case "Draw":
			match.OddsDraw = outcome.Price
		}
	}
}

// CreditsRemaining returns the remaining monthly API credits.
func (o *OddsAPI) CreditsRemaining() int {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.remaining
}

// findH2HMarket returns the h2h market from the first bookmaker, or nil.
func findH2HMarket(bookmakers []oddsAPIBookmaker) *oddsAPIMarket {
	if len(bookmakers) == 0 {
		return nil
	}
	for i := range bookmakers[0].Markets {
		if bookmakers[0].Markets[i].Key == "h2h" {
			return &bookmakers[0].Markets[i]
		}
	}
	return nil
}

func (o *OddsAPI) doGet(ctx context.Context, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, -1, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, -1, err
	}
	defer resp.Body.Close()

	// Parse remaining credits from response header.
	remaining := -1
	if h := resp.Header.Get("x-requests-remaining"); h != "" {
		if v, err := strconv.Atoi(h); err == nil {
			remaining = v
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, remaining, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, remaining, err
	}

	return body, remaining, nil
}
