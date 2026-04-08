package providers

import "testing"

func TestParseNaverGame_Before(t *testing.T) {
	g := naverGame{
		GameID:        "20260319HTHH02026",
		CategoryID:    "kbo",
		GameDateTime:  "2026-03-19T13:00:00",
		HomeTeamName:  "한화",
		AwayTeamName:  "KIA",
		StatusCode:    "BEFORE",
		CurrentInning: "",
	}

	match := parseNaverGame(g)

	if match.Status != BaseballScheduled {
		t.Errorf("status = %q, want %q", match.Status, BaseballScheduled)
	}
	if match.HomeTeam != "한화" {
		t.Errorf("HomeTeam = %q, want %q", match.HomeTeam, "한화")
	}
	if match.AwayTeam != "KIA" {
		t.Errorf("AwayTeam = %q, want %q", match.AwayTeam, "KIA")
	}
	if match.StartTime.Hour() != 13 {
		t.Errorf("StartTime hour = %d, want 13", match.StartTime.Hour())
	}
}

func TestParseNaverGame_Live(t *testing.T) {
	g := naverGame{
		GameID:        "20260319LGSK02026",
		CategoryID:    "kbo",
		GameDateTime:  "2026-03-19T13:00:00",
		HomeTeamName:  "SSG",
		AwayTeamName:  "LG",
		HomeTeamScore: 3,
		AwayTeamScore: 5,
		StatusCode:    "PLAY",
		CurrentInning: "7회말",
	}

	match := parseNaverGame(g)

	if match.Status != BaseballLive {
		t.Errorf("status = %q, want %q", match.Status, BaseballLive)
	}
	if match.Inning != 7 {
		t.Errorf("inning = %d, want 7", match.Inning)
	}
	if match.Half != InningBottom {
		t.Errorf("half = %q, want %q", match.Half, InningBottom)
	}
	if match.HomeScore != 3 || match.AwayScore != 5 {
		t.Errorf("score = %d:%d, want 3:5", match.HomeScore, match.AwayScore)
	}
}

func TestParseNaverGame_Finished(t *testing.T) {
	g := naverGame{
		GameID:        "20260319OBLT02026",
		CategoryID:    "kbo",
		GameDateTime:  "2026-03-19T13:00:00",
		HomeTeamName:  "롯데",
		AwayTeamName:  "두산",
		HomeTeamScore: 2,
		AwayTeamScore: 7,
		StatusCode:    "RESULT",
	}

	match := parseNaverGame(g)

	if match.Status != BaseballFinished {
		t.Errorf("status = %q, want %q", match.Status, BaseballFinished)
	}
}

func TestParseNaverGame_Cancelled(t *testing.T) {
	g := naverGame{
		GameID:       "20260319TEST02026",
		CategoryID:   "kbo",
		GameDateTime: "2026-03-19T18:30:00",
		HomeTeamName: "LG",
		AwayTeamName: "삼성",
		Cancel:       true,
		StatusCode:   "CANCEL",
	}

	match := parseNaverGame(g)

	if match.Status != BaseballCancelled {
		t.Errorf("status = %q, want %q", match.Status, BaseballCancelled)
	}
}

func TestKBOInningParsing(t *testing.T) {
	tests := []struct {
		input  string
		inning int
		half   InningHalf
	}{
		{"5회초", 5, InningTop},
		{"7회말", 7, InningBottom},
		{"1회초", 1, InningTop},
		{"12회말", 12, InningBottom},
	}
	for _, tt := range tests {
		g := naverGame{
			GameID:        "test",
			CategoryID:    "kbo",
			GameDateTime:  "2026-03-19T13:00:00",
			StatusCode:    "PLAY",
			CurrentInning: tt.input,
		}
		match := parseNaverGame(g)
		if match.Inning != tt.inning {
			t.Errorf("input=%q: inning = %d, want %d", tt.input, match.Inning, tt.inning)
		}
		if match.Half != tt.half {
			t.Errorf("input=%q: half = %q, want %q", tt.input, match.Half, tt.half)
		}
	}
}
