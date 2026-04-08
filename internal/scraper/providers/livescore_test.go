package providers

import (
	"encoding/json"
	"testing"
)

func TestParseLivescoreStatus(t *testing.T) {
	tests := []struct {
		eps        string
		wantStatus MatchStatus
		wantDetail string
	}{
		{eps: "NS", wantStatus: MatchScheduled, wantDetail: "NS"},
		{eps: "FT", wantStatus: MatchFinished, wantDetail: "FT"},
		{eps: "HT", wantStatus: MatchLive, wantDetail: "HT"},
		{eps: "45'", wantStatus: MatchLive, wantDetail: "45'"},
		{eps: "", wantStatus: MatchScheduled, wantDetail: ""},
	}

	for _, tc := range tests {
		status, detail := parseLivescoreStatus(tc.eps)
		if status != tc.wantStatus || detail != tc.wantDetail {
			t.Fatalf("parseLivescoreStatus(%q) = (%s,%q), want (%s,%q)", tc.eps, status, detail, tc.wantStatus, tc.wantDetail)
		}
	}
}

func TestParseLivescoreTime(t *testing.T) {
	if got := parseLivescoreTime(json.RawMessage(`"20260319193000"`)); got.IsZero() {
		t.Fatal("expected string timestamp to parse")
	}
	if got := parseLivescoreTime(json.RawMessage(`20260319193000`)); got.IsZero() {
		t.Fatal("expected numeric timestamp to parse")
	}
	if got := parseLivescoreTime(json.RawMessage(`"bad"`)); !got.IsZero() {
		t.Fatalf("expected zero time for invalid input, got %v", got)
	}
}

func TestParseLivescoreStageMatches(t *testing.T) {
	stage := livescoreStage{
		Snm: "K-League 1",
		Events: []livescoreEvent{
			{
				Eid: "m1",
				T1:  []livescoreTeam{{Nm: "울산"}},
				T2:  []livescoreTeam{{Nm: "전북"}},
				Tr1: "2",
				Tr2: "1",
				Eps: "FT",
				Esd: json.RawMessage(`"20260319193000"`),
			},
		},
	}

	matches := parseLivescoreStageMatches(stage)
	if len(matches) != 1 {
		t.Fatalf("matches len = %d", len(matches))
	}
	got := matches[0]
	if got.ID != "m1" || got.HomeTeam != "울산" || got.AwayTeam != "전북" {
		t.Fatalf("match = %+v", got)
	}
	if got.Status != MatchFinished {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestInferScoringTeam(t *testing.T) {
	home := inferScoringTeam("HOME", "AWAY", []int{1, 0}, []int{1, 1})
	if home != "AWAY" {
		t.Fatalf("inferScoringTeam should pick away, got %q", home)
	}
	home = inferScoringTeam("HOME", "AWAY", []int{0, 0}, []int{1, 0})
	if home != "HOME" {
		t.Fatalf("inferScoringTeam should pick home, got %q", home)
	}
}

func TestLivescoreGroupEvents(t *testing.T) {
	group := scoredGroup{
		min: 36,
		sc:  []int{1, 0},
		incs: []livescoreIncident{
			{Min: 36, IT: 63, Pn: "Assist"},
			{Min: 36, IT: 36, Pn: "Scorer"},
		},
	}
	events := livescoreGroupEvents(group, "HOME")
	if len(events) != 1 {
		t.Fatalf("events len = %d", len(events))
	}
	if events[0].Player != "Scorer" || events[0].Assist != "Assist" {
		t.Fatalf("event = %+v", events[0])
	}

	fallback := livescoreGroupEvents(scoredGroup{min: 12, sc: []int{1, 0}}, "AWAY")
	if len(fallback) != 1 || fallback[0].Type != EventGoal {
		t.Fatalf("fallback events = %+v", fallback)
	}
}
