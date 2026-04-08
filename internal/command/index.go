package command

import (
	"context"
	"log/slog"
	"strings"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/pkg/formatter"
)

// indexProvider abstracts Naver Finance index API methods used by IndexHandler.
type indexProvider interface {
	FetchDomesticIndex(ctx context.Context, code string) (providers.IndexQuote, error)
	FetchWorldIndex(ctx context.Context, reutersCode string) (providers.IndexQuote, error)
}

// indexEntry describes a single market index entry.
type indexEntry struct {
	code      string // e.g., "KOSPI" or ".IXIC"
	name      string // display name
	domestic  bool   // true: m.stock.naver.com/api/index, false: api.stock.naver.com/index
}

// indexAliases maps normalized user inputs to their index entries.
var indexAliases = buildIndexAliases()

func buildIndexAliases() map[string]indexEntry {
	entries := []struct {
		aliases  []string
		entry    indexEntry
	}{
		{
			aliases: []string{"코스피", "kospi"},
			entry:   indexEntry{code: "KOSPI", name: "코스피", domestic: true},
		},
		{
			aliases: []string{"코스닥", "kosdaq"},
			entry:   indexEntry{code: "KOSDAQ", name: "코스닥", domestic: true},
		},
		{
			aliases: []string{"코스피200", "kpi200"},
			entry:   indexEntry{code: "KPI200", name: "코스피200", domestic: true},
		},
		{
			aliases: []string{"나스닥", "nasdaq"},
			entry:   indexEntry{code: ".IXIC", name: "나스닥 종합", domestic: false},
		},
		{
			aliases: []string{"나스닥100", "nasdaq100", "ndx"},
			entry:   indexEntry{code: ".NDX", name: "나스닥 100", domestic: false},
		},
		{
			aliases: []string{"다우", "다우존스", "dow", "dji", "djia"},
			entry:   indexEntry{code: ".DJI", name: "다우존스", domestic: false},
		},
		{
			aliases: []string{"sp500", "s&p500", "s&p", "spx"},
			entry:   indexEntry{code: ".INX", name: "S&P 500", domestic: false},
		},
		{
			aliases: []string{"닛케이", "니케이", "nikkei", "n225"},
			entry:   indexEntry{code: ".N225", name: "닛케이 225", domestic: false},
		},
		{
			aliases: []string{"항셍", "항생", "hsi", "hangseng"},
			entry:   indexEntry{code: ".HSI", name: "항셍", domestic: false},
		},
		{
			aliases: []string{"상해", "상하이", "ssec", "shanghai"},
			entry:   indexEntry{code: ".SSEC", name: "상해종합", domestic: false},
		},
	}

	m := make(map[string]indexEntry, 40)
	for _, e := range entries {
		for _, alias := range e.aliases {
			m[strings.ToLower(alias)] = e.entry
		}
	}
	return m
}

// resolveIndexAlias looks up a user input string in the alias map.
func resolveIndexAlias(input string) (indexEntry, bool) {
	entry, ok := indexAliases[strings.ToLower(strings.TrimSpace(input))]
	return entry, ok
}

// IndexHandler handles market index quote lookups (KOSPI, KOSDAQ, NASDAQ, DOW, etc.).
// It implements BareQueryMatcher so single-word inputs like "코스피" are auto-routed.
type IndexHandler struct {
	descriptorSupport
	provider indexProvider
	logger   *slog.Logger
}

// NewIndexHandler creates a new index handler.
func NewIndexHandler(provider indexProvider, logger *slog.Logger) *IndexHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &IndexHandler{
		descriptorSupport: newDescriptorSupport("index"),
		provider:          provider,
		logger:            logger.With("component", "index_handler"),
	}
}

// MatchBareQuery returns true when content is a single-word known index alias.
func (h *IndexHandler) MatchBareQuery(_ context.Context, content string) ([]string, bool) {
	content = strings.TrimSpace(content)
	if content == "" || strings.ContainsAny(content, " \t\n") {
		return nil, false
	}
	if _, ok := resolveIndexAlias(content); !ok {
		return nil, false
	}
	return []string{content}, true
}

// Execute handles explicit index commands ("지수 코스피") and bare-query routing.
func (h *IndexHandler) Execute(ctx context.Context, cmd bot.CommandContext) error {
	if len(cmd.Args) == 0 {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "지수명을 입력해주세요.\n예: 코스피, 코스닥, 나스닥, 다우, S&P500, 닛케이",
		})
	}

	query := strings.Join(cmd.Args, " ")
	return h.lookup(ctx, cmd, query)
}

func (h *IndexHandler) lookup(ctx context.Context, cmd bot.CommandContext, query string) error {
	entry, ok := resolveIndexAlias(query)
	if !ok {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: formatter.Prefix("⚠️", "알 수 없는 지수입니다: "+query+"\n예: 코스피, 코스닥, 나스닥, 다우"),
		})
	}

	quote, err := h.fetchIndex(ctx, entry)
	if err != nil {
		h.logger.Warn("index fetch failed", "code", entry.code, "error", err)
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: formatter.Error(err),
		})
	}

	// Use the entry's display name when the API doesn't return one (domestic indices).
	displayName := quote.Name
	if displayName == "" {
		displayName = entry.name
	}

	text := formatter.FormatIndexQuote(formatter.IndexData{
		Name:            displayName,
		Price:           quote.Price,
		Change:          quote.Change,
		ChangePercent:   quote.ChangePercent,
		ChangeDirection: quote.ChangeDirection,
	})

	if err := cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
	}); err != nil {
		return err
	}

	if cmd.Command == "" {
		return bot.ErrHandled
	}
	return nil
}

func (h *IndexHandler) fetchIndex(ctx context.Context, entry indexEntry) (providers.IndexQuote, error) {
	if entry.domestic {
		return h.provider.FetchDomesticIndex(ctx, entry.code)
	}
	return h.provider.FetchWorldIndex(ctx, entry.code)
}
