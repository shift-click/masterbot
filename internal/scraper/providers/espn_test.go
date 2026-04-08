package providers

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAmericanToDecimalOdds(t *testing.T) {
	tests := []struct {
		american float64
		want     float64
	}{
		{150, 2.50},
		{-110, 1.9091},
		{270, 3.70},
		{290, 3.90},
		{100, 2.00},
		{-200, 1.50},
	}

	for _, tt := range tests {
		got := americanToDecimal(tt.american)
		diff := got - tt.want
		if diff < -0.01 || diff > 0.01 {
			t.Errorf("americanToDecimal(%.0f) = %.4f, want ~%.4f", tt.american, got, tt.want)
		}
	}
}

func TestTranslateTeamName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Arsenal", "아스널"},
		{"arsenal", "아스널"},
		{"Manchester City", "맨시티"},
		{"Unknown Team FC", "Unknown Team FC"},
		{"FC Seoul", "FC서울"},
		{"PSG", "PSG"},
	}

	for _, tt := range tests {
		got := TranslateTeamName(tt.input)
		if got != tt.want {
			t.Errorf("TranslateTeamName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseMoneylineOdds(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want float64
	}{
		{name: "nested-close", raw: `{"close":{"odds":"+150"}}`, want: 2.5},
		{name: "object-value", raw: `{"value":-110}`, want: 1.9091},
		{name: "number", raw: `-200`, want: 1.5},
		{name: "string", raw: `"+220"`, want: 3.2},
		{name: "invalid", raw: `{"foo":"bar"}`, want: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseMoneylineOdds(json.RawMessage(tc.raw))
			if tc.want == 0 {
				if got != 0 {
					t.Fatalf("parseMoneylineOdds(%s) = %v, want 0", tc.raw, got)
				}
				return
			}
			diff := got - tc.want
			if diff < -0.01 || diff > 0.01 {
				t.Fatalf("parseMoneylineOdds(%s) = %v, want ~%v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseESPNEvent(t *testing.T) {
	event := espnEvent{
		ID: "event-1",
		Competitions: []espnCompetition{
			{
				Competitors: []espnCompetitor{
					{HomeAway: "home", Team: espnTeam{DisplayName: "Arsenal"}, Score: "2"},
					{HomeAway: "away", Team: espnTeam{DisplayName: "Chelsea"}, Score: "1"},
				},
				Status:    espnStatus{Type: espnStatusType{State: "in", Detail: "45'"}},
				StartDate: "2026-03-19T12:30:00Z",
				Odds: []espnOdds{
					{Moneyline: &espnMoneyline{
						Home: json.RawMessage(`"+120"`),
						Draw: json.RawMessage(`"+210"`),
						Away: json.RawMessage(`"+300"`),
					}},
				},
				Details: []espnDetail{
					{
						Type:             espnDetailType{ID: "70"},
						Clock:            espnClock{DisplayValue: "36'"},
						Team:             espnDetailTeam{DisplayName: "Arsenal"},
						AthletesInvolved: []espnAthlete{{DisplayName: "Saka"}},
					},
				},
			},
		},
	}

	match, ok := parseESPNEvent(event)
	if !ok {
		t.Fatal("expected parseESPNEvent to succeed")
	}
	if match.ID != "event-1" || match.HomeTeam != "Arsenal" || match.AwayTeam != "Chelsea" {
		t.Fatalf("match = %+v", match)
	}
	if match.Status != MatchLive || match.StatusDetail != "45'" {
		t.Fatalf("status = (%s,%s)", match.Status, match.StatusDetail)
	}
	if match.StartTime.IsZero() {
		t.Fatal("expected parsed start time")
	}
	if len(match.Events) != 1 || match.Events[0].Player != "Saka" {
		t.Fatalf("events = %+v", match.Events)
	}
}

func TestParseESPNStartTime(t *testing.T) {
	got := parseESPNStartTime("2026-03-19T12:30:00Z")
	if got.IsZero() {
		t.Fatal("expected parsed time")
	}
	if got.UTC().Format(time.RFC3339) != "2026-03-19T12:30:00Z" {
		t.Fatalf("got = %s", got.UTC().Format(time.RFC3339))
	}
}
