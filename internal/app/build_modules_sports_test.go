package app

import (
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/scraper"
	"github.com/shift-click/masterbot/internal/scraper/providers"
)

func TestNormalizeDashedDate(t *testing.T) {
	if got := normalizeDashedDate("20260319"); got != "2026-03-19" {
		t.Fatalf("normalizeDashedDate = %q", got)
	}
	if got := normalizeDashedDate("2026-03-19"); got != "2026-03-19" {
		t.Fatalf("normalizeDashedDate = %q", got)
	}
}

func TestDedupeFootballMatchesByDate(t *testing.T) {
	loc := time.FixedZone("KST", 9*60*60)
	targetDate := "20260319"
	matches := []providers.FootballMatch{
		{ID: "m1", StartTime: time.Date(2026, 3, 19, 15, 0, 0, 0, loc)},
		{ID: "m1", StartTime: time.Date(2026, 3, 19, 15, 0, 0, 0, loc)},
		{ID: "m2", StartTime: time.Date(2026, 3, 20, 15, 0, 0, 0, loc)},
	}

	filtered := dedupeFootballMatchesByDate(matches, loc, targetDate)
	if len(filtered) != 1 || filtered[0].ID != "m1" {
		t.Fatalf("filtered = %+v", filtered)
	}
}

func TestHasIncompleteGoalScorer(t *testing.T) {
	events := []providers.MatchEvent{
		{Type: providers.EventGoal, Player: ""},
	}
	if !hasIncompleteGoalScorer(events) {
		t.Fatal("expected incomplete scorer to be detected")
	}
	events[0].Player = "손흥민"
	if hasIncompleteGoalScorer(events) {
		t.Fatal("expected complete scorer not to be detected")
	}
}

func TestFirstMissingOddsMatch(t *testing.T) {
	cache := scraper.NewFootballCache(time.Hour)
	cache.SetNextMatches("epl", []providers.FootballMatch{
		{ID: "next-1", HomeTeam: "A", AwayTeam: "B", Status: providers.MatchScheduled},
	}, time.Now().Add(24*time.Hour))

	today := []providers.FootballMatch{
		{ID: "done", Status: providers.MatchFinished},
		{ID: "t1", HomeTeam: "C", AwayTeam: "D", Status: providers.MatchScheduled},
	}

	target, ok := firstMissingOddsMatch(cache, "epl", today)
	if !ok {
		t.Fatal("expected target match")
	}
	if target.ID != "t1" {
		t.Fatalf("target = %s, want t1", target.ID)
	}

	cache.SetOdds("t1", 1.8, 3.2, 4.0)
	target, ok = firstMissingOddsMatch(cache, "epl", today)
	if !ok {
		t.Fatal("expected next target match")
	}
	if target.ID != "next-1" {
		t.Fatalf("target = %s, want next-1", target.ID)
	}
}
