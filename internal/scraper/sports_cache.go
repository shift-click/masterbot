package scraper

import (
	"sync"
	"time"

	"github.com/shift-click/masterbot/internal/scraper/providers"
)

// FootballCache holds cached football schedule and event data.
type FootballCache struct {
	mu sync.RWMutex

	// matches keyed by league ID -> date string "2006-01-02"
	matches map[string]map[string][]providers.FootballMatch

	// nextMatches stores the next upcoming matches when today has none.
	// Keyed by league ID.
	nextMatches map[string][]providers.FootballMatch
	nextDates   map[string]time.Time

	// events keyed by match ID -> events list (permanent cache for the day)
	events map[string][]providers.MatchEvent

	// odds keyed by match ID -> odds data with TTL
	odds    map[string]oddsEntry
	oddsTTL time.Duration

	// previous scores for change detection, keyed by match ID
	prevScores map[string][2]int
}

type oddsEntry struct {
	home, draw, away float64
	fetchedAt        time.Time
}

// NewFootballCache creates a new football cache.
func NewFootballCache(oddsTTL time.Duration) *FootballCache {
	return &FootballCache{
		matches:     make(map[string]map[string][]providers.FootballMatch),
		nextMatches: make(map[string][]providers.FootballMatch),
		nextDates:   make(map[string]time.Time),
		events:      make(map[string][]providers.MatchEvent),
		odds:        make(map[string]oddsEntry),
		oddsTTL:     oddsTTL,
		prevScores:  make(map[string][2]int),
	}
}

// SetMatches updates cached matches for a league and date.
func (c *FootballCache) SetMatches(leagueID, date string, matches []providers.FootballMatch) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.matches[leagueID] == nil {
		c.matches[leagueID] = make(map[string][]providers.FootballMatch)
	}
	cp := make([]providers.FootballMatch, len(matches))
	copy(cp, matches)
	c.matches[leagueID][date] = cp
}

// GetMatches returns cached matches for a league and date.
func (c *FootballCache) GetMatches(leagueID, date string) ([]providers.FootballMatch, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if byDate, ok := c.matches[leagueID]; ok {
		if matches, ok := byDate[date]; ok {
			cp := make([]providers.FootballMatch, len(matches))
			copy(cp, matches)
			return cp, true
		}
	}
	return nil, false
}

// SetNextMatches stores the next upcoming matches for a league when today has none.
func (c *FootballCache) SetNextMatches(leagueID string, matches []providers.FootballMatch, date time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]providers.FootballMatch, len(matches))
	copy(cp, matches)
	c.nextMatches[leagueID] = cp
	c.nextDates[leagueID] = date
}

// GetNextMatches returns the next upcoming matches for a league.
func (c *FootballCache) GetNextMatches(leagueID string) ([]providers.FootballMatch, time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	matches, ok := c.nextMatches[leagueID]
	if !ok || len(matches) == 0 {
		return nil, time.Time{}, false
	}
	cp := make([]providers.FootballMatch, len(matches))
	copy(cp, matches)
	return cp, c.nextDates[leagueID], true
}

// SetEvents stores match events (permanent for the day).
func (c *FootballCache) SetEvents(matchID string, events []providers.MatchEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]providers.MatchEvent, len(events))
	copy(cp, events)
	c.events[matchID] = cp
}

// GetEvents returns cached events for a match.
func (c *FootballCache) GetEvents(matchID string) ([]providers.MatchEvent, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	events, ok := c.events[matchID]
	if !ok {
		return nil, false
	}
	cp := make([]providers.MatchEvent, len(events))
	copy(cp, events)
	return cp, true
}

// SetOdds stores odds for a match with TTL.
func (c *FootballCache) SetOdds(matchID string, home, draw, away float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.odds[matchID] = oddsEntry{home: home, draw: draw, away: away, fetchedAt: time.Now()}
}

// GetOdds returns cached odds if not expired.
func (c *FootballCache) GetOdds(matchID string) (home, draw, away float64, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, exists := c.odds[matchID]
	if !exists || time.Since(entry.fetchedAt) > c.oddsTTL {
		return 0, 0, 0, false
	}
	return entry.home, entry.draw, entry.away, true
}

// DetectScoreChanges compares current matches against previous scores.
// Returns match IDs where scores changed.
func (c *FootballCache) DetectScoreChanges(matches []providers.FootballMatch) []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	var changed []string
	for _, m := range matches {
		prev, exists := c.prevScores[m.ID]
		current := [2]int{m.HomeScore, m.AwayScore}
		if exists && prev != current {
			changed = append(changed, m.ID)
		}
		c.prevScores[m.ID] = current
	}
	return changed
}

// HasLiveMatches checks if any cached matches for a league and date are currently live.
func (c *FootballCache) HasLiveMatches(leagueID, date string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if byDate, ok := c.matches[leagueID]; ok {
		if matches, ok := byDate[date]; ok {
			for _, m := range matches {
				if m.Status == providers.MatchLive {
					return true
				}
			}
		}
	}
	return false
}

// BaseballCache holds cached baseball schedule data.
type BaseballCache struct {
	mu sync.RWMutex

	// matches keyed by league ID -> date string "20060102"
	matches map[string]map[string][]providers.BaseballMatch

	// nextMatches stores the next upcoming matches when today has none.
	nextMatches map[string][]providers.BaseballMatch
	nextDates   map[string]time.Time
}

// NewBaseballCache creates a new baseball cache.
func NewBaseballCache() *BaseballCache {
	return &BaseballCache{
		matches:     make(map[string]map[string][]providers.BaseballMatch),
		nextMatches: make(map[string][]providers.BaseballMatch),
		nextDates:   make(map[string]time.Time),
	}
}

// SetMatches updates cached matches for a league and date.
func (c *BaseballCache) SetMatches(leagueID, date string, matches []providers.BaseballMatch) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.matches[leagueID] == nil {
		c.matches[leagueID] = make(map[string][]providers.BaseballMatch)
	}
	cp := make([]providers.BaseballMatch, len(matches))
	copy(cp, matches)
	c.matches[leagueID][date] = cp
}

// GetMatches returns cached matches for a league and date.
func (c *BaseballCache) GetMatches(leagueID, date string) ([]providers.BaseballMatch, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if byDate, ok := c.matches[leagueID]; ok {
		if matches, ok := byDate[date]; ok {
			cp := make([]providers.BaseballMatch, len(matches))
			copy(cp, matches)
			return cp, true
		}
	}
	return nil, false
}

// SetNextMatches stores the next upcoming matches for a league when today has none.
func (c *BaseballCache) SetNextMatches(leagueID string, matches []providers.BaseballMatch, date time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]providers.BaseballMatch, len(matches))
	copy(cp, matches)
	c.nextMatches[leagueID] = cp
	c.nextDates[leagueID] = date
}

// GetNextMatches returns the next upcoming matches for a league.
func (c *BaseballCache) GetNextMatches(leagueID string) ([]providers.BaseballMatch, time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	matches, ok := c.nextMatches[leagueID]
	if !ok || len(matches) == 0 {
		return nil, time.Time{}, false
	}
	cp := make([]providers.BaseballMatch, len(matches))
	copy(cp, matches)
	return cp, c.nextDates[leagueID], true
}

// HasLiveMatches checks if any cached matches for a league and date are currently live.
func (c *BaseballCache) HasLiveMatches(leagueID, date string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if byDate, ok := c.matches[leagueID]; ok {
		if matches, ok := byDate[date]; ok {
			for _, m := range matches {
				if m.Status == providers.BaseballLive {
					return true
				}
			}
		}
	}
	return false
}

// EsportsCache holds cached esports schedule data.
type EsportsCache struct {
	mu sync.RWMutex

	// matches keyed by league ID -> date string "2006-01-02"
	matches map[string]map[string][]providers.EsportsMatch
}

// NewEsportsCache creates a new esports cache.
func NewEsportsCache() *EsportsCache {
	return &EsportsCache{
		matches: make(map[string]map[string][]providers.EsportsMatch),
	}
}

// SetMatches updates cached matches for a league and date.
func (c *EsportsCache) SetMatches(leagueID, date string, matches []providers.EsportsMatch) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.matches[leagueID] == nil {
		c.matches[leagueID] = make(map[string][]providers.EsportsMatch)
	}
	cp := make([]providers.EsportsMatch, len(matches))
	copy(cp, matches)
	c.matches[leagueID][date] = cp
}

// GetMatches returns cached matches for a league and date.
func (c *EsportsCache) GetMatches(leagueID, date string) ([]providers.EsportsMatch, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if byDate, ok := c.matches[leagueID]; ok {
		if matches, ok := byDate[date]; ok {
			cp := make([]providers.EsportsMatch, len(matches))
			copy(cp, matches)
			return cp, true
		}
	}
	return nil, false
}

// HasLiveMatches checks if any cached matches for a league and date are currently live.
func (c *EsportsCache) HasLiveMatches(leagueID, date string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if byDate, ok := c.matches[leagueID]; ok {
		if matches, ok := byDate[date]; ok {
			for _, m := range matches {
				if m.Status == providers.MatchLive {
					return true
				}
			}
		}
	}
	return false
}
