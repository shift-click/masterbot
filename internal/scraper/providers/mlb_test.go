package providers

import (
	"testing"
	"time"
)

func TestParseMLBGame_Finished(t *testing.T) {
	g := mlbGame{
		GamePk:   831556,
		GameDate: "2026-03-18T17:05:00Z",
		Status:   struct{ StatusCode string `json:"statusCode"` }{StatusCode: "F"},
		Teams: struct {
			Away mlbTeamEntry `json:"away"`
			Home mlbTeamEntry `json:"home"`
		}{
			Away: mlbTeamEntry{Team: struct{ Name string `json:"name"` }{Name: "Houston Astros"}, Score: 1},
			Home: mlbTeamEntry{Team: struct{ Name string `json:"name"` }{Name: "St. Louis Cardinals"}, Score: 4},
		},
		Linescore: mlbLinescore{CurrentInning: 9, IsTopInning: true},
	}

	match := parseMLBGame(g)

	if match.Status != BaseballFinished {
		t.Errorf("status = %q, want %q", match.Status, BaseballFinished)
	}
	if match.HomeTeam != "St. Louis Cardinals" {
		t.Errorf("HomeTeam = %q, want %q", match.HomeTeam, "St. Louis Cardinals")
	}
	if match.AwayTeam != "Houston Astros" {
		t.Errorf("AwayTeam = %q, want %q", match.AwayTeam, "Houston Astros")
	}
	if match.HomeScore != 4 || match.AwayScore != 1 {
		t.Errorf("score = %d:%d, want 4:1", match.HomeScore, match.AwayScore)
	}
	if match.Inning != 9 {
		t.Errorf("inning = %d, want 9", match.Inning)
	}
}

func TestParseMLBGame_Live(t *testing.T) {
	g := mlbGame{
		GamePk:   831600,
		GameDate: "2026-03-18T21:05:00Z",
		Status:   struct{ StatusCode string `json:"statusCode"` }{StatusCode: "I"},
		Teams: struct {
			Away mlbTeamEntry `json:"away"`
			Home mlbTeamEntry `json:"home"`
		}{
			Away: mlbTeamEntry{Team: struct{ Name string `json:"name"` }{Name: "Colorado Rockies"}, Score: 8},
			Home: mlbTeamEntry{Team: struct{ Name string `json:"name"` }{Name: "Cincinnati Reds"}, Score: 6},
		},
		Linescore: mlbLinescore{CurrentInning: 7, IsTopInning: false},
	}

	match := parseMLBGame(g)

	if match.Status != BaseballLive {
		t.Errorf("status = %q, want %q", match.Status, BaseballLive)
	}
	if match.Inning != 7 {
		t.Errorf("inning = %d, want 7", match.Inning)
	}
	if match.Half != InningBottom {
		t.Errorf("half = %q, want %q", match.Half, InningBottom)
	}
}

func TestParseMLBGame_Scheduled(t *testing.T) {
	g := mlbGame{
		GamePk:   831700,
		GameDate: "2026-03-19T01:10:00Z",
		Status:   struct{ StatusCode string `json:"statusCode"` }{StatusCode: "S"},
		Teams: struct {
			Away mlbTeamEntry `json:"away"`
			Home mlbTeamEntry `json:"home"`
		}{
			Away: mlbTeamEntry{Team: struct{ Name string `json:"name"` }{Name: "Kansas City Royals"}},
			Home: mlbTeamEntry{Team: struct{ Name string `json:"name"` }{Name: "Texas Rangers"}},
		},
	}

	match := parseMLBGame(g)

	if match.Status != BaseballScheduled {
		t.Errorf("status = %q, want %q", match.Status, BaseballScheduled)
	}
	if match.StartTime.IsZero() {
		t.Error("StartTime should not be zero")
	}
	expected := time.Date(2026, 3, 19, 1, 10, 0, 0, time.UTC)
	if !match.StartTime.Equal(expected) {
		t.Errorf("StartTime = %v, want %v", match.StartTime, expected)
	}
}

func TestMapMLBStatus(t *testing.T) {
	tests := []struct {
		code string
		want BaseballMatchStatus
	}{
		{"S", BaseballScheduled},
		{"P", BaseballScheduled},
		{"I", BaseballLive},
		{"F", BaseballFinished},
		{"FO", BaseballFinished},
		{"DR", BaseballCancelled},
		{"DI", BaseballCancelled},
		{"X", BaseballScheduled}, // unknown defaults to scheduled
	}
	for _, tt := range tests {
		if got := mapMLBStatus(tt.code); got != tt.want {
			t.Errorf("mapMLBStatus(%q) = %q, want %q", tt.code, got, tt.want)
		}
	}
}
