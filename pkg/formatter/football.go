package formatter

import (
	"fmt"
	"strings"
	"time"
)

// FootballMatchData holds data for formatting a single football match.
type FootballMatchData struct {
	HomeTeam     string
	AwayTeam     string
	HomeScore    int
	AwayScore    int
	Status       string // "scheduled", "live", "finished"
	StatusDetail string // e.g. "45'" for live, "FT" for finished
	StartTime    time.Time
	Events       []FootballEventData
	OddsHome     float64
	OddsDraw     float64
	OddsAway     float64
}

// FootballEventData holds data for a match event.
type FootballEventData struct {
	Type   string // "goal", "penalty", "own_goal", "red_card"
	Player string
	Assist string
	Minute string
	Team   string
}

// FormatFootballSchedule formats a list of football matches for a given league.
// leagueName: display name (e.g. "EPL", "K리그")
// date: the date being displayed
// matches: list of matches for this league on this date
// nextMatches: if no matches today, these are upcoming matches (may be nil)
// nextDate: date of next matches (used if no matches today)
func FormatFootballSchedule(leagueName string, date time.Time, matches []FootballMatchData, nextMatches []FootballMatchData, nextDate time.Time) string {
	var b strings.Builder
	dateStr := formatKoreanDate(date)

	if len(matches) == 0 {
		b.WriteString(fmt.Sprintf("⚽ %s 경기가 없습니다.", dateStr))
		if len(nextMatches) > 0 {
			nextDateStr := formatKoreanDateWithDay(nextDate)
			b.WriteString(fmt.Sprintf("\n다음 %s 경기 %s\n", leagueName, nextDateStr))
			for i, m := range nextMatches {
				if i > 0 {
					b.WriteString("\n-\n")
				}
				b.WriteString(formatSingleMatch(m))
			}
		}
		return b.String()
	}

	// Has matches today
	dayName := weekdayKorean(date.Weekday())
	b.WriteString(fmt.Sprintf("⚽ %s(%s) %s\n", dateStr, dayName, leagueName))

	for i, m := range matches {
		if i > 0 {
			b.WriteString("\n-\n")
		}
		b.WriteString(formatSingleMatch(m))
	}

	return b.String()
}

func formatSingleMatch(m FootballMatchData) string {
	var b strings.Builder

	switch m.Status {
	case "scheduled":
		timeStr := m.StartTime.In(kst()).Format("15:04")
		b.WriteString(fmt.Sprintf("[%s 예정] %s vs %s", timeStr, m.HomeTeam, m.AwayTeam))
		if m.OddsHome > 0 && m.OddsDraw > 0 && m.OddsAway > 0 {
			b.WriteString(fmt.Sprintf("\n            [%.2f %.2f %.2f]", m.OddsHome, m.OddsDraw, m.OddsAway))
		}

	case "live":
		detail := m.StatusDetail
		if detail == "" {
			detail = "진행"
		}
		b.WriteString(fmt.Sprintf("[진행 %s] %s %d : %d %s", detail, m.HomeTeam, m.HomeScore, m.AwayScore, m.AwayTeam))
		writeEvents(&b, m.Events)

	case "finished":
		b.WriteString(fmt.Sprintf("[종료] %s %d : %d %s", m.HomeTeam, m.HomeScore, m.AwayScore, m.AwayTeam))
		writeEvents(&b, m.Events)
	}

	return b.String()
}

func writeEvents(b *strings.Builder, events []FootballEventData) {
	if len(events) == 0 {
		return
	}

	// Group events by team
	teamEvents := make(map[string][]FootballEventData)
	var teamOrder []string
	for _, ev := range events {
		if _, seen := teamEvents[ev.Team]; !seen {
			teamOrder = append(teamOrder, ev.Team)
		}
		teamEvents[ev.Team] = append(teamEvents[ev.Team], ev)
	}

	for _, team := range teamOrder {
		evts := teamEvents[team]
		b.WriteString(fmt.Sprintf("\n   └ [%s] ", team))
		for i, ev := range evts {
			if i > 0 {
				b.WriteString(", ")
			}
			icon := eventIcon(ev.Type)
			b.WriteString(icon)
			b.WriteString(" ")
			b.WriteString(ev.Player)
			if ev.Assist != "" {
				b.WriteString(fmt.Sprintf(" (AS: %s)", ev.Assist))
			}
			b.WriteString(" ")
			b.WriteString(ev.Minute)
		}
	}
}

func eventIcon(eventType string) string {
	switch eventType {
	case "goal":
		return "⚽"
	case "penalty":
		return "🅿️"
	case "own_goal":
		return "🔴"
	case "red_card":
		return "🟥"
	case "yellow_card":
		return "🟨"
	default:
		return "⚽"
	}
}

func formatKoreanDate(t time.Time) string {
	return fmt.Sprintf("%02d월 %02d일", t.Month(), t.Day())
}

func formatKoreanDateWithDay(t time.Time) string {
	day := weekdayKorean(t.Weekday())
	return fmt.Sprintf("%02d월 %02d일 (%s)", t.Month(), t.Day(), day)
}

func weekdayKorean(w time.Weekday) string {
	days := [...]string{"일", "월", "화", "수", "목", "금", "토"}
	return days[w]
}

func kst() *time.Location {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return time.FixedZone("KST", 9*60*60)
	}
	return loc
}
