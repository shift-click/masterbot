package command

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/pkg/formatter"
)

// FootballHandler handles football schedule lookups.
type FootballHandler struct {
	descriptorSupport
	cache  *scraper.FootballCache
	logger *slog.Logger
}

// NewFootballHandler creates a new football handler.
func NewFootballHandler(cache *scraper.FootballCache, logger *slog.Logger) *FootballHandler {
	return &FootballHandler{
		descriptorSupport: newDescriptorSupport("football"),
		cache:             cache,
		logger:            logger,
	}
}

func (h *FootballHandler) Execute(ctx context.Context, cmd bot.CommandContext) error {
	args := cmd.Args
	today := todayKST()
	dateStr := today.Format("20060102")

	leagues := providers.FootballLeagues()
	if len(args) > 0 {
		input := strings.Join(args, " ")
		league, ok := providers.LookupFootballLeague(input)
		if !ok {
			return cmd.Reply(ctx, bot.Reply{
				Type: transport.ReplyTypeText,
				Text: "알 수 없는 리그입니다. 지원: EPL, 라리가, 세리에, 분데스, K리그, 챔스",
			})
		}
		leagues = []providers.FootballLeague{league}
	}

	var parts []string
	for _, league := range leagues {
		matches, _ := h.cache.GetMatches(league.ID, dateStr)
		matchData := toFootballMatchData(matches, h.cache)

		var nextData []formatter.FootballMatchData
		var nextDate time.Time
		if len(matchData) == 0 {
			if nextMatches, nd, ok := h.cache.GetNextMatches(league.ID); ok {
				nextData = toFootballMatchData(nextMatches, h.cache)
				nextDate = nd
			}
		}

		text := formatter.FormatFootballSchedule(league.Name, today, matchData, nextData, nextDate)
		parts = append(parts, text)
	}

	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: strings.Join(parts, "\n"),
	})
}

func (h *FootballHandler) MatchBareQuery(_ context.Context, content string) ([]string, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, false
	}
	if _, ok := providers.LookupFootballLeague(content); ok {
		return []string{content}, true
	}
	return nil, false
}

func toFootballMatchData(matches []providers.FootballMatch, cache *scraper.FootballCache) []formatter.FootballMatchData {
	data := make([]formatter.FootballMatchData, 0, len(matches))
	for _, m := range matches {
		d := formatter.FootballMatchData{
			HomeTeam:     providers.TranslateTeamName(m.HomeTeam),
			AwayTeam:     providers.TranslateTeamName(m.AwayTeam),
			HomeScore:    m.HomeScore,
			AwayScore:    m.AwayScore,
			Status:       string(m.Status),
			StatusDetail: m.StatusDetail,
			StartTime:    m.StartTime,
			OddsHome:     m.OddsHome,
			OddsDraw:     m.OddsDraw,
			OddsAway:     m.OddsAway,
		}

		// Merge cached events
		if events, ok := cache.GetEvents(m.ID); ok {
			for _, ev := range events {
				d.Events = append(d.Events, formatter.FootballEventData{
					Type:   string(ev.Type),
					Player: ev.Player,
					Assist: ev.Assist,
					Minute: ev.Minute,
					Team:   providers.TranslateTeamName(ev.Team),
				})
			}
		}

		// Merge cached odds if match has none but cache has them
		if d.OddsHome == 0 {
			if h, dr, a, ok := cache.GetOdds(m.ID); ok {
				d.OddsHome = h
				d.OddsDraw = dr
				d.OddsAway = a
			}
		}

		data = append(data, d)
	}
	return data
}

func todayKST() time.Time {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		loc = time.FixedZone("KST", 9*60*60)
	}
	return time.Now().In(loc)
}
