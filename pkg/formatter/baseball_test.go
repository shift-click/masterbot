package formatter

import (
	"strings"
	"testing"
	"time"
)

func kstTime(hour, min int) time.Time {
	loc, _ := time.LoadLocation("Asia/Seoul")
	if loc == nil {
		loc = time.FixedZone("KST", 9*60*60)
	}
	return time.Date(2026, 3, 19, hour, min, 0, 0, loc)
}

func TestFormatBaseballSchedule_Scheduled(t *testing.T) {
	date := kstTime(12, 0)
	matches := []BaseballMatchData{
		{HomeTeam: "SSG", AwayTeam: "LG", Status: "scheduled", StartTime: kstTime(13, 0)},
		{HomeTeam: "롯데", AwayTeam: "두산", Status: "scheduled", StartTime: kstTime(13, 0)},
	}

	result := FormatBaseballSchedule("KBO", date, matches, nil, time.Time{})

	if !strings.Contains(result, "⚾ 오늘의 KBO") {
		t.Errorf("missing KBO header, got: %q", result)
	}
	if !strings.Contains(result, "[13:00 예정] LG vs SSG") {
		t.Errorf("missing scheduled match, got: %q", result)
	}
}

func TestFormatBaseballSchedule_Live(t *testing.T) {
	date := kstTime(14, 0)
	matches := []BaseballMatchData{
		{HomeTeam: "신시내티", AwayTeam: "콜로라도", HomeScore: 6, AwayScore: 8, Status: "live", Inning: 9, Half: "top"},
	}

	result := FormatBaseballSchedule("MLB", date, matches, nil, time.Time{})

	if !strings.Contains(result, "🔴[9 회] 콜로라도 8 : 6 신시내티") {
		t.Errorf("missing live match format, got: %q", result)
	}
}

func TestFormatBaseballSchedule_Finished(t *testing.T) {
	date := kstTime(14, 0)
	matches := []BaseballMatchData{
		{HomeTeam: "세인트루이스", AwayTeam: "휴스턴", HomeScore: 4, AwayScore: 1, Status: "finished"},
	}

	result := FormatBaseballSchedule("MLB", date, matches, nil, time.Time{})

	if !strings.Contains(result, "[종료] 휴스턴 1 : 4 세인트루이스") {
		t.Errorf("missing finished match format, got: %q", result)
	}
}

func TestFormatBaseballSchedule_Cancelled(t *testing.T) {
	date := kstTime(14, 0)
	matches := []BaseballMatchData{
		{HomeTeam: "워싱턴", AwayTeam: "마이애미", Status: "cancelled"},
	}

	result := FormatBaseballSchedule("MLB", date, matches, nil, time.Time{})

	if !strings.Contains(result, "[취소 예정] 마이애미 vs 워싱턴") {
		t.Errorf("missing cancelled format, got: %q", result)
	}
}

func TestFormatBaseballSchedule_NoMatches(t *testing.T) {
	date := kstTime(12, 0)
	nextDate := time.Date(2026, 3, 20, 0, 0, 0, 0, kst())
	nextMatches := []BaseballMatchData{
		{HomeTeam: "세이부", AwayTeam: "요코하마", Status: "scheduled", StartTime: time.Date(2026, 3, 20, 13, 0, 0, 0, kst())},
	}

	result := FormatBaseballSchedule("NPB", date, nil, nextMatches, nextDate)

	if !strings.Contains(result, "경기가 없습니다") {
		t.Errorf("missing no-match message, got: %q", result)
	}
	if !strings.Contains(result, "다음 NPB 경기") {
		t.Errorf("missing next schedule, got: %q", result)
	}
	if !strings.Contains(result, "03월 20일") {
		t.Errorf("missing next date, got: %q", result)
	}
}

func TestFormatBaseballSchedule_MLBHeader(t *testing.T) {
	date := kstTime(14, 0)
	matches := []BaseballMatchData{
		{HomeTeam: "양키스", AwayTeam: "보스턴", HomeScore: 1, AwayScore: 0, Status: "finished"},
	}

	result := FormatBaseballSchedule("MLB", date, matches, nil, time.Time{})

	// MLB uses "MM월 DD일(요일)의 MLB" format
	if !strings.Contains(result, "03월 19일") || !strings.Contains(result, "MLB") {
		t.Errorf("missing MLB header format, got: %q", result)
	}
}
