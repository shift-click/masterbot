package scraper

import (
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/scraper/providers"
)

func TestFootballCacheSetGetMatches(t *testing.T) {
	cache := NewFootballCache(4 * time.Hour)

	matches := []providers.FootballMatch{
		{ID: "1", HomeTeam: "Arsenal", AwayTeam: "Chelsea", Status: providers.MatchScheduled},
		{ID: "2", HomeTeam: "Liverpool", AwayTeam: "ManCity", Status: providers.MatchLive},
	}

	cache.SetMatches("epl", "20260318", matches)
	got, ok := cache.GetMatches("epl", "20260318")

	if !ok {
		t.Fatal("expected matches to be cached")
	}
	if len(got) != 2 {
		t.Errorf("got %d matches, want 2", len(got))
	}
}

func TestFootballCacheDetectScoreChanges(t *testing.T) {
	cache := NewFootballCache(4 * time.Hour)

	// First set - no previous scores
	matches := []providers.FootballMatch{
		{ID: "1", HomeScore: 0, AwayScore: 0},
		{ID: "2", HomeScore: 0, AwayScore: 0},
	}
	changed := cache.DetectScoreChanges(matches)
	if len(changed) != 0 {
		t.Errorf("first set should have no changes, got %d", len(changed))
	}

	// Score change in match 1
	matches[0].HomeScore = 1
	changed = cache.DetectScoreChanges(matches)
	if len(changed) != 1 || changed[0] != "1" {
		t.Errorf("expected change in match 1, got %v", changed)
	}

	// No change
	changed = cache.DetectScoreChanges(matches)
	if len(changed) != 0 {
		t.Errorf("expected no changes, got %d", len(changed))
	}
}

func TestFootballCacheHasLiveMatches(t *testing.T) {
	cache := NewFootballCache(4 * time.Hour)

	cache.SetMatches("epl", "20260318", []providers.FootballMatch{
		{ID: "1", Status: providers.MatchScheduled},
	})
	if cache.HasLiveMatches("epl", "20260318") {
		t.Error("expected no live matches")
	}

	cache.SetMatches("epl", "20260318", []providers.FootballMatch{
		{ID: "1", Status: providers.MatchLive},
	})
	if !cache.HasLiveMatches("epl", "20260318") {
		t.Error("expected live matches")
	}
}

func TestFootballCacheOddsTTL(t *testing.T) {
	cache := NewFootballCache(1 * time.Millisecond)

	cache.SetOdds("match1", 1.50, 3.80, 5.00)

	h, d, a, ok := cache.GetOdds("match1")
	if !ok {
		t.Fatal("expected odds to be cached")
	}
	if h != 1.50 || d != 3.80 || a != 5.00 {
		t.Errorf("unexpected odds: %.2f %.2f %.2f", h, d, a)
	}

	// Wait for TTL
	time.Sleep(2 * time.Millisecond)
	_, _, _, ok = cache.GetOdds("match1")
	if ok {
		t.Error("expected odds to be expired")
	}
}

func TestEsportsCacheSetGetMatches(t *testing.T) {
	cache := NewEsportsCache()

	matches := []providers.EsportsMatch{
		{ID: "1", Team1: "T1", Team2: "Gen.G", Status: providers.MatchFinished},
	}

	cache.SetMatches("lck", "20260318", matches)
	got, ok := cache.GetMatches("lck", "20260318")

	if !ok {
		t.Fatal("expected matches to be cached")
	}
	if len(got) != 1 {
		t.Errorf("got %d matches, want 1", len(got))
	}
	if got[0].Team1 != "T1" {
		t.Errorf("team1 = %q, want T1", got[0].Team1)
	}
}
