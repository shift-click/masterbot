package formatter

import (
	"fmt"
	"strings"
	"time"
)

// EsportsMatchData holds data for formatting a single esports match.
type EsportsMatchData struct {
	Team1     string // team name (Korean)
	Team1Code string
	Team2     string
	Team2Code string
	Score1    int    // game wins
	Score2    int
	BestOf    int    // 3 or 5
	Status    string // "scheduled", "live", "finished"
	StartTime time.Time
	BlockName string // e.g. "13주 차"
}

// FormatEsportsSchedule formats a list of esports matches for a given league.
func FormatEsportsSchedule(leagueName string, date time.Time, matches []EsportsMatchData) string {
	var b strings.Builder
	dateStr := formatKoreanDate(date)

	if len(matches) == 0 {
		b.WriteString(fmt.Sprintf("🎮 %s 예정된 %s 경기가 없습니다.", dateStr, leagueName))
		return b.String()
	}

	dayName := weekdayKorean(date.Weekday())
	b.WriteString(fmt.Sprintf("🎮 %s(%s) %s\n", dateStr, dayName, leagueName))

	for i, m := range matches {
		if i > 0 {
			b.WriteString("\n-\n")
		}
		b.WriteString(formatSingleEsportsMatch(m))
	}

	return b.String()
}

func formatSingleEsportsMatch(m EsportsMatchData) string {
	var b strings.Builder

	switch m.Status {
	case "scheduled":
		timeStr := m.StartTime.In(kst()).Format("15:04")
		b.WriteString(fmt.Sprintf("[%s 예정] %s vs %s", timeStr, m.Team1, m.Team2))

	case "live":
		// Determine current game number from scores
		currentGame := m.Score1 + m.Score2 + 1
		b.WriteString(fmt.Sprintf("[진행 Game %d] %s vs %s", currentGame, m.Team1, m.Team2))

	case "finished":
		b.WriteString(fmt.Sprintf("[종료] %s %d : %d %s", m.Team1, m.Score1, m.Score2, m.Team2))
	}

	return b.String()
}
