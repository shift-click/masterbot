package command

import (
	"context"
	"sort"
	"strings"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/intent"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/pkg/formatter"
)

var helpCategories = []helpCategory{
	{name: "시세", emoji: "💰"},
	{name: "스포츠", emoji: "⚽"},
	{name: "정보", emoji: "📰"},
	{name: "로또", emoji: "🎱"},
	{name: "관리", emoji: "⚙️"},
}

type helpCategory struct {
	name  string
	emoji string
}

type HelpHandler struct {
	descriptorSupport
	entries func(chatID string) []intent.Entry
	resolve func(query string) (intent.Entry, bool)
}

func NewHelpHandler(
	entries func(chatID string) []intent.Entry,
	resolve func(query string) (intent.Entry, bool),
) *HelpHandler {
	return &HelpHandler{
		descriptorSupport: newDescriptorSupport("help"),
		entries:           entries,
		resolve:           resolve,
	}
}

func (h *HelpHandler) Execute(ctx context.Context, cmd bot.CommandContext) error {
	if len(cmd.Args) > 0 {
		return h.executeDetail(ctx, cmd)
	}
	return h.executeList(ctx, cmd)
}

func (h *HelpHandler) executeList(ctx context.Context, cmd bot.CommandContext) error {
	entries := h.entries(cmd.Message.Raw.ChatID)
	if len(entries) == 0 {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "이 방에서 허용된 기능이 없습니다.",
		})
	}

	grouped := make(map[string][]intent.Entry)
	for _, entry := range entries {
		grouped[entry.Category] = append(grouped[entry.Category], entry)
	}

	var buf strings.Builder
	buf.WriteString("📚 사용 가능한 명령어\n")

	for _, cat := range helpCategories {
		catEntries, ok := grouped[cat.name]
		if !ok || len(catEntries) == 0 {
			continue
		}
		sort.Slice(catEntries, func(i, j int) bool {
			return catEntries[i].Name < catEntries[j].Name
		})

		buf.WriteString("\n")
		buf.WriteString(cat.emoji)
		buf.WriteString(" ")
		buf.WriteString(cat.name)
		buf.WriteString("\n")

		rows := make([][]string, 0, len(catEntries))
		for _, entry := range catEntries {
			rows = append(rows, []string{
				entry.Name,
				helpExamples(entry),
				entry.Description,
			})
		}
		buf.WriteString(formatter.Table(nil, rows))
		buf.WriteString("\n")
	}

	appendLottoHelpBlock(&buf, entries)
	buf.WriteString("\n💡 도움 <기능명> — 상세 사용법")

	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: buf.String(),
	})
}

func (h *HelpHandler) executeDetail(ctx context.Context, cmd bot.CommandContext) error {
	query := strings.Join(cmd.Args, " ")
	entry, ok := h.resolve(query)
	if !ok || !entry.HelpVisible {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "알 수 없는 기능입니다. 도움 으로 사용 가능한 명령어를 확인하세요.",
		})
	}

	var buf strings.Builder
	buf.WriteString("📚 ")
	buf.WriteString(entry.Name)
	buf.WriteString(" — ")
	buf.WriteString(entry.Description)
	buf.WriteString("\n")

	if entry.Example != "" {
		buf.WriteString("\n예시\n  ")
		buf.WriteString(entry.Example)
		buf.WriteString("\n")
	}

	if len(entry.SlashAliases) > 0 {
		aliases := make([]string, 0, len(entry.SlashAliases)+1)
		aliases = append(aliases, entry.Name)
		for _, a := range entry.SlashAliases {
			aliases = append(aliases, a)
		}
		buf.WriteString("\n별칭\n  ")
		buf.WriteString(strings.Join(aliases, ", "))
		buf.WriteString("\n")
	}

	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: buf.String(),
	})
}

func helpExamples(entry intent.Entry) string {
	if entry.Example != "" {
		return strings.TrimPrefix(entry.Example, "/")
	}
	aliases := strings.Join(entry.SlashAliases, ", ")
	if aliases == "" {
		return ""
	}
	return entry.Name + " | " + aliases
}

func appendLottoHelpBlock(buf *strings.Builder, entries []intent.Entry) {
	if buf == nil || !hasHelpEntry(entries, "lotto") {
		return
	}
	buf.WriteString("\n🎱 로또\n")
	buf.WriteString("최신 당첨번호: 로또\n")
	buf.WriteString("랜덤 번호 등록: 로또 추천\n")
	buf.WriteString("내 번호 조회: !로또\n")
	buf.WriteString("번호 등록: !로또 <n1> <n2> <n3> <n4> <n5> <n6>\n")
	buf.WriteString("세트 삭제: !로또 <번호(옵션)> 삭제\n")
}

func hasHelpEntry(entries []intent.Entry, id string) bool {
	for _, entry := range entries {
		if entry.ID == id {
			return true
		}
	}
	return false
}
