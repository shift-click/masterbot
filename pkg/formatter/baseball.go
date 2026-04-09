package formatter

import (
	"fmt"
	"strings"
	"time"
)

// BaseballMatchData holds data for formatting a single baseball match.
type BaseballMatchData struct {
	HomeTeam  string
	AwayTeam  string
	HomeScore int
	AwayScore int
	Status    string // "scheduled", "live", "finished", "cancelled"
	Inning    int    // current inning (0 if not started)
	Half      string // "top" or "bottom"
	StartTime time.Time
}

// FormatBaseballSchedule formats a list of baseball matches for a given league.
func FormatBaseballSchedule(leagueName string, date time.Time, matches []BaseballMatchData, nextMatches []BaseballMatchData, nextDate time.Time) string {
	var b strings.Builder
	dateStr := formatKoreanDate(date)

	if len(matches) == 0 {
		b.WriteString(fmt.Sprintf("⚾ %s 경기가 없습니다.", dateStr))
		if len(nextMatches) > 0 {
			nextDateStr := formatKoreanDateWithDay(nextDate)
			b.WriteString(fmt.Sprintf("\n다음 %s 경기 %s\n", leagueName, nextDateStr))
			for i, m := range nextMatches {
				if i > 0 {
					b.WriteString("\n")
				}
				b.WriteString(formatSingleBaseballMatch(m))
			}
		}
		return b.String()
	}

	// Has matches today
	dayName := weekdayKorean(date.Weekday())
	if leagueName == "KBO" {
		b.WriteString(fmt.Sprintf("⚾ 오늘의 %s\n", leagueName))
	} else {
		b.WriteString(fmt.Sprintf("⚾ %s(%s)의 %s\n", dateStr, dayName, leagueName))
	}

	for i, m := range matches {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(formatSingleBaseballMatch(m))
	}

	return b.String()
}

func formatSingleBaseballMatch(m BaseballMatchData) string {
	var b strings.Builder

	switch m.Status {
	case "scheduled":
		timeStr := m.StartTime.In(kst()).Format("15:04")
		b.WriteString(fmt.Sprintf("[%s 예정] %s vs %s", timeStr, m.AwayTeam, m.HomeTeam))

	case "live":
		inningStr := fmt.Sprintf("%d", m.Inning)
		if inningStr == "0" {
			inningStr = "?"
		}
		b.WriteString(fmt.Sprintf("🔴[%s 회] %s %d : %d %s", inningStr, m.AwayTeam, m.AwayScore, m.HomeScore, m.HomeTeam))

	case "finished":
		b.WriteString(fmt.Sprintf("[종료] %s %d : %d %s", m.AwayTeam, m.AwayScore, m.HomeScore, m.HomeTeam))

	case "cancelled":
		b.WriteString(fmt.Sprintf("[취소 예정] %s vs %s", m.AwayTeam, m.HomeTeam))
	}

	return b.String()
}
