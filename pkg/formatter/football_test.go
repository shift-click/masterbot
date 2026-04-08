package formatter

import (
	"strings"
	"testing"
	"time"
)

func TestFormatFootballScheduleNoMatches(t *testing.T) {
	date := time.Date(2026, 3, 18, 0, 0, 0, 0, time.UTC)
	nextDate := time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC)

	nextMatches := []FootballMatchData{
		{
			HomeTeam:  "본머스",
			AwayTeam:  "맨유",
			Status:    "scheduled",
			StartTime: time.Date(2026, 3, 21, 5, 0, 0, 0, kst()),
			OddsHome:  3.20,
			OddsDraw:  3.80,
			OddsAway:  2.10,
		},
	}

	result := FormatFootballSchedule("EPL", date, nil, nextMatches, nextDate)

	if !strings.Contains(result, "경기가 없습니다") {
		t.Error("expected '경기가 없습니다' in output")
	}
	if !strings.Contains(result, "다음 EPL 경기") {
		t.Error("expected '다음 EPL 경기' in output")
	}
	if !strings.Contains(result, "본머스 vs 맨유") {
		t.Error("expected '본머스 vs 맨유' in output")
	}
	if !strings.Contains(result, "[3.20 3.80 2.10]") {
		t.Error("expected odds '[3.20 3.80 2.10]' in output")
	}
}

func TestFormatFootballScheduleFinishedMatch(t *testing.T) {
	date := time.Date(2026, 3, 18, 0, 0, 0, 0, kst())

	matches := []FootballMatchData{
		{
			HomeTeam:  "아스널",
			AwayTeam:  "레버쿠젠",
			HomeScore: 2,
			AwayScore: 0,
			Status:    "finished",
			Events: []FootballEventData{
				{Type: "goal", Player: "E. Eze", Assist: "L. Trossard", Minute: "36'", Team: "아스널"},
				{Type: "goal", Player: "D. Rice", Minute: "63'", Team: "아스널"},
			},
		},
	}

	result := FormatFootballSchedule("챔피언스리그", date, matches, nil, time.Time{})

	if !strings.Contains(result, "[종료] 아스널 2 : 0 레버쿠젠") {
		t.Error("expected finished match format")
	}
	if !strings.Contains(result, "⚽ E. Eze (AS: L. Trossard) 36'") {
		t.Error("expected goal with assist")
	}
	if !strings.Contains(result, "⚽ D. Rice 63'") {
		t.Error("expected goal without assist")
	}
}

func TestFormatFootballScheduleLiveMatch(t *testing.T) {
	date := time.Date(2026, 3, 18, 0, 0, 0, 0, kst())

	matches := []FootballMatchData{
		{
			HomeTeam:     "맨시티",
			AwayTeam:     "레알 마드리드",
			HomeScore:    0,
			AwayScore:    1,
			Status:       "live",
			StatusDetail: "45'",
			Events: []FootballEventData{
				{Type: "penalty", Player: "Vinícius Jr.", Minute: "22'", Team: "레알 마드리드"},
			},
		},
	}

	result := FormatFootballSchedule("챔피언스리그", date, matches, nil, time.Time{})

	if !strings.Contains(result, "[진행 45']") {
		t.Error("expected live match with minute")
	}
	if !strings.Contains(result, "🅿️ Vinícius Jr.") {
		t.Error("expected penalty icon")
	}
}

func TestEventIcons(t *testing.T) {
	tests := []struct {
		eventType string
		want      string
	}{
		{"goal", "⚽"},
		{"penalty", "🅿️"},
		{"own_goal", "🔴"},
		{"red_card", "🟥"},
		{"yellow_card", "🟨"},
	}

	for _, tt := range tests {
		got := eventIcon(tt.eventType)
		if got != tt.want {
			t.Errorf("eventIcon(%q) = %q, want %q", tt.eventType, got, tt.want)
		}
	}
}
