package formatter

import (
	"strings"
	"testing"
	"time"
)

func TestFormatEsportsScheduleNoMatches(t *testing.T) {
	date := time.Date(2026, 3, 18, 0, 0, 0, 0, time.UTC)
	result := FormatEsportsSchedule("LCK", date, nil)

	if !strings.Contains(result, "🎮") {
		t.Error("expected esports icon")
	}
	if !strings.Contains(result, "예정된 LCK 경기가 없습니다") {
		t.Error("expected no-match message")
	}
}

func TestFormatEsportsScheduleCompletedMatch(t *testing.T) {
	date := time.Date(2026, 3, 15, 0, 0, 0, 0, kst())

	matches := []EsportsMatchData{
		{
			Team1:  "T1",
			Team2:  "한화생명",
			Score1: 2,
			Score2: 0,
			BestOf: 3,
			Status: "finished",
		},
	}

	result := FormatEsportsSchedule("LCK", date, matches)

	if !strings.Contains(result, "[종료] T1 2 : 0 한화생명") {
		t.Errorf("expected completed match format, got: %s", result)
	}
}

func TestFormatEsportsScheduleLiveMatch(t *testing.T) {
	date := time.Date(2026, 3, 15, 0, 0, 0, 0, kst())

	matches := []EsportsMatchData{
		{
			Team1:  "Gen.G",
			Team2:  "DRX",
			Score1: 1,
			Score2: 0,
			BestOf: 3,
			Status: "live",
		},
	}

	result := FormatEsportsSchedule("LCK", date, matches)

	if !strings.Contains(result, "[진행 Game 2] Gen.G vs DRX") {
		t.Errorf("expected live match format, got: %s", result)
	}
}

func TestFormatEsportsScheduleScheduledMatch(t *testing.T) {
	date := time.Date(2026, 3, 15, 0, 0, 0, 0, kst())

	matches := []EsportsMatchData{
		{
			Team1:     "T1",
			Team2:     "DRX",
			Status:    "scheduled",
			StartTime: time.Date(2026, 3, 15, 17, 0, 0, 0, kst()),
		},
	}

	result := FormatEsportsSchedule("LCK", date, matches)

	if !strings.Contains(result, "[17:00 예정] T1 vs DRX") {
		t.Errorf("expected scheduled match format, got: %s", result)
	}
}
