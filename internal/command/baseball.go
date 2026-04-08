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

// BaseballHandler handles baseball schedule lookups.
type BaseballHandler struct {
	descriptorSupport
	cache  *scraper.BaseballCache
	logger *slog.Logger
}

// NewBaseballHandler creates a new baseball handler.
func NewBaseballHandler(cache *scraper.BaseballCache, logger *slog.Logger) *BaseballHandler {
	return &BaseballHandler{
		descriptorSupport: newDescriptorSupport("baseball"),
		cache:             cache,
		logger:            logger,
	}
}

func (h *BaseballHandler) Execute(ctx context.Context, cmd bot.CommandContext) error {
	args := cmd.Args
	today := todayKST()
	dateStr := today.Format("20060102")

	leagues := providers.BaseballLeagues()
	if len(args) > 0 {
		input := strings.Join(args, " ")
		league, ok := providers.LookupBaseballLeague(input)
		if !ok {
			return cmd.Reply(ctx, bot.Reply{
				Type: transport.ReplyTypeText,
				Text: "알 수 없는 리그입니다. 지원: MLB, KBO, NPB",
			})
		}
		leagues = []providers.BaseballLeague{league}
	}

	var parts []string
	for _, league := range leagues {
		matches, _ := h.cache.GetMatches(league.ID, dateStr)
		matchData := toBaseballMatchData(matches)

		var nextData []formatter.BaseballMatchData
		var nextDate time.Time
		if len(matchData) == 0 {
			if nextMatches, nd, ok := h.cache.GetNextMatches(league.ID); ok {
				nextData = toBaseballMatchData(nextMatches)
				nextDate = nd
			}
		}

		text := formatter.FormatBaseballSchedule(league.Name, today, matchData, nextData, nextDate)
		parts = append(parts, text)
	}

	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: strings.Join(parts, "\n"),
	})
}

func (h *BaseballHandler) MatchBareQuery(_ context.Context, content string) ([]string, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, false
	}
	if _, ok := providers.LookupBaseballLeague(content); ok {
		return []string{content}, true
	}
	return nil, false
}

func toBaseballMatchData(matches []providers.BaseballMatch) []formatter.BaseballMatchData {
	data := make([]formatter.BaseballMatchData, 0, len(matches))
	for _, m := range matches {
		homeTeam := m.HomeTeam
		awayTeam := m.AwayTeam

		// KBO already provides Korean names; translate MLB/NPB
		if m.League != "kbo" {
			homeTeam = providers.TranslateBaseballTeamName(homeTeam)
			awayTeam = providers.TranslateBaseballTeamName(awayTeam)
		}

		data = append(data, formatter.BaseballMatchData{
			HomeTeam:  homeTeam,
			AwayTeam:  awayTeam,
			HomeScore: m.HomeScore,
			AwayScore: m.AwayScore,
			Status:    string(m.Status),
			Inning:    m.Inning,
			Half:      string(m.Half),
			StartTime: m.StartTime,
		})
	}
	return data
}
