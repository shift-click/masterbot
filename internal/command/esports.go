package command

import (
	"context"
	"log/slog"
	"strings"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/pkg/formatter"
)

// EsportsHandler handles esports schedule lookups.
type EsportsHandler struct {
	descriptorSupport
	cache  *scraper.EsportsCache
	logger *slog.Logger
}

// NewEsportsHandler creates a new esports handler.
func NewEsportsHandler(cache *scraper.EsportsCache, logger *slog.Logger) *EsportsHandler {
	return &EsportsHandler{
		descriptorSupport: newDescriptorSupport("esports"),
		cache:             cache,
		logger:            logger,
	}
}

func (h *EsportsHandler) Execute(ctx context.Context, cmd bot.CommandContext) error {
	args := cmd.Args
	today := todayKST()
	dateStr := today.Format("20060102")

	leagues := providers.EsportsLeagues()
	if len(args) > 0 {
		input := strings.Join(args, " ")
		league, ok := providers.LookupEsportsLeague(input)
		if !ok {
			return cmd.Reply(ctx, bot.Reply{
				Type: transport.ReplyTypeText,
				Text: "알 수 없는 리그입니다. 지원: LCK, LPL, LEC, LCS, LCP",
			})
		}
		leagues = []providers.EsportsLeague{league}
	}

	var parts []string
	for _, league := range leagues {
		matches, _ := h.cache.GetMatches(league.ID, dateStr)

		matchData := make([]formatter.EsportsMatchData, 0, len(matches))
		for _, m := range matches {
			matchData = append(matchData, formatter.EsportsMatchData{
				Team1:     m.Team1,
				Team1Code: m.Team1Code,
				Team2:     m.Team2,
				Team2Code: m.Team2Code,
				Score1:    m.Score1,
				Score2:    m.Score2,
				BestOf:    m.BestOf,
				Status:    string(m.Status),
				StartTime: m.StartTime,
				BlockName: m.BlockName,
			})
		}

		text := formatter.FormatEsportsSchedule(league.Name, today, matchData)
		parts = append(parts, text)
	}

	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: strings.Join(parts, "\n"),
	})
}

func (h *EsportsHandler) MatchBareQuery(_ context.Context, content string) ([]string, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, false
	}
	if _, ok := providers.LookupEsportsLeague(content); ok {
		return []string{content}, true
	}
	return nil, false
}
