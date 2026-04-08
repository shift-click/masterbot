package command

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/intent"
	"github.com/shift-click/masterbot/internal/transport"
)

func testHelpCatalog(t *testing.T) *intent.Catalog {
	t.Helper()
	catalog, err := intent.NewCatalog([]intent.Entry{
		{ID: "help", Name: "도움", SlashAliases: []string{"help", "h"}, Description: "명령어 목록 조회"},
		{ID: "coin", Name: "코인", SlashAliases: []string{"ㅋ", "coin"}, Description: "코인 시세 조회", Example: "비트, BTC", Category: "시세", HelpVisible: true},
		{ID: "stock", Name: "주식", SlashAliases: []string{"ㅈ", "stock"}, Description: "주식 시세 조회", Example: "삼전, 005930", Category: "시세", HelpVisible: true},
		{ID: "football", Name: "축구", SlashAliases: []string{"축구"}, Description: "축구 경기 일정/스코어", Example: "EPL, K리그", Category: "스포츠", HelpVisible: true},
		{ID: "news", Name: "뉴스", SlashAliases: []string{"news"}, Description: "실시간 인기뉴스 Top5", Example: "/뉴스", Category: "정보", HelpVisible: true},
		{ID: "admin", Name: "관리", SlashAliases: []string{"admin"}, Description: "운영 관리", Example: "관리 방 목록", Category: "관리", HelpVisible: true},
		{ID: "ai", Name: "AI", SlashAliases: []string{"ai"}, Description: "AI 대화"},
	})
	if err != nil {
		t.Fatalf("new catalog: %v", err)
	}
	return catalog
}

func testVisibleEntries(catalog *intent.Catalog) func(string) []intent.Entry {
	return func(_ string) []intent.Entry {
		var entries []intent.Entry
		for _, e := range catalog.Entries() {
			if e.HelpVisible {
				entries = append(entries, e)
			}
		}
		return entries
	}
}

func executeHelp(t *testing.T, handler *HelpHandler, args []string) bot.Reply {
	t.Helper()
	var captured bot.Reply
	ctx := context.Background()
	cmd := bot.CommandContext{
		Message: transport.Message{
			Raw: transport.RawChatLog{ChatID: "test-room"},
		},
		Args: args,
		Reply: func(_ context.Context, r bot.Reply) error {
			captured = r
			return nil
		},
		Now: time.Now,
	}
	if err := handler.Execute(ctx, cmd); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return captured
}

func TestHelpListGroupsByCategory(t *testing.T) {
	t.Parallel()

	catalog := testHelpCatalog(t)
	handler := NewHelpHandler(testVisibleEntries(catalog), catalog.Resolve)
	reply := executeHelp(t, handler, nil)

	text := reply.Text
	// 카테고리 헤더가 올바른 순서로 존재
	idxMarket := strings.Index(text, "💰 시세")
	idxSports := strings.Index(text, "⚽ 스포츠")
	idxInfo := strings.Index(text, "📰 정보")
	idxAdmin := strings.Index(text, "⚙️ 관리")

	if idxMarket < 0 || idxSports < 0 || idxInfo < 0 || idxAdmin < 0 {
		t.Fatalf("missing category headers in:\n%s", text)
	}
	if !(idxMarket < idxSports && idxSports < idxInfo && idxInfo < idxAdmin) {
		t.Fatalf("category order wrong: market=%d sports=%d info=%d admin=%d", idxMarket, idxSports, idxInfo, idxAdmin)
	}
}

func TestHelpListExcludesHiddenCommands(t *testing.T) {
	t.Parallel()

	catalog := testHelpCatalog(t)
	handler := NewHelpHandler(testVisibleEntries(catalog), catalog.Resolve)
	reply := executeHelp(t, handler, nil)

	if strings.Contains(reply.Text, "AI") {
		t.Fatalf("hidden AI command should not appear in help list:\n%s", reply.Text)
	}
	if strings.Contains(reply.Text, "명령어 목록 조회") {
		t.Fatalf("hidden help command description should not appear in help list:\n%s", reply.Text)
	}
}

func TestHelpListSkipsEmptyCategory(t *testing.T) {
	t.Parallel()

	catalog, err := intent.NewCatalog([]intent.Entry{
		{ID: "help", Name: "도움", SlashAliases: []string{"help"}},
		{ID: "coin", Name: "코인", SlashAliases: []string{"coin"}, Description: "코인 시세 조회", Category: "시세", HelpVisible: true},
	})
	if err != nil {
		t.Fatalf("new catalog: %v", err)
	}

	handler := NewHelpHandler(testVisibleEntries(catalog), catalog.Resolve)
	reply := executeHelp(t, handler, nil)

	if strings.Contains(reply.Text, "스포츠") {
		t.Fatalf("empty sports category should not appear:\n%s", reply.Text)
	}
	if strings.Contains(reply.Text, "관리") {
		t.Fatalf("empty admin category should not appear:\n%s", reply.Text)
	}
}

func TestHelpListShowsFooterTip(t *testing.T) {
	t.Parallel()

	catalog := testHelpCatalog(t)
	handler := NewHelpHandler(testVisibleEntries(catalog), catalog.Resolve)
	reply := executeHelp(t, handler, nil)

	if !strings.Contains(reply.Text, "💡 도움 <기능명>") {
		t.Fatalf("footer tip missing:\n%s", reply.Text)
	}
}

func TestHelpDetailByName(t *testing.T) {
	t.Parallel()

	catalog := testHelpCatalog(t)
	handler := NewHelpHandler(testVisibleEntries(catalog), catalog.Resolve)
	reply := executeHelp(t, handler, []string{"코인"})

	if !strings.Contains(reply.Text, "📚 코인") {
		t.Fatalf("detail header missing:\n%s", reply.Text)
	}
	if !strings.Contains(reply.Text, "코인 시세 조회") {
		t.Fatalf("description missing:\n%s", reply.Text)
	}
	if !strings.Contains(reply.Text, "비트, BTC") {
		t.Fatalf("example missing:\n%s", reply.Text)
	}
}

func TestHelpDetailByAlias(t *testing.T) {
	t.Parallel()

	catalog := testHelpCatalog(t)
	handler := NewHelpHandler(testVisibleEntries(catalog), catalog.Resolve)
	reply := executeHelp(t, handler, []string{"ㅋ"})

	if !strings.Contains(reply.Text, "📚 코인") {
		t.Fatalf("detail by alias should resolve to 코인:\n%s", reply.Text)
	}
}

func TestHelpDetailByID(t *testing.T) {
	t.Parallel()

	catalog := testHelpCatalog(t)
	handler := NewHelpHandler(testVisibleEntries(catalog), catalog.Resolve)
	reply := executeHelp(t, handler, []string{"coin"})

	if !strings.Contains(reply.Text, "📚 코인") {
		t.Fatalf("detail by ID should resolve to 코인:\n%s", reply.Text)
	}
}

func TestHelpDetailUnknownCommand(t *testing.T) {
	t.Parallel()

	catalog := testHelpCatalog(t)
	handler := NewHelpHandler(testVisibleEntries(catalog), catalog.Resolve)
	reply := executeHelp(t, handler, []string{"없는기능"})

	if !strings.Contains(reply.Text, "알 수 없는 기능입니다") {
		t.Fatalf("unknown command should show error:\n%s", reply.Text)
	}
}

func TestHelpDetailBlocksHiddenCommand(t *testing.T) {
	t.Parallel()

	catalog := testHelpCatalog(t)
	handler := NewHelpHandler(testVisibleEntries(catalog), catalog.Resolve)
	reply := executeHelp(t, handler, []string{"AI"})

	if !strings.Contains(reply.Text, "알 수 없는 기능입니다") {
		t.Fatalf("hidden command should not be accessible via detail:\n%s", reply.Text)
	}
}

func TestHelpDetailShowsAliases(t *testing.T) {
	t.Parallel()

	catalog := testHelpCatalog(t)
	handler := NewHelpHandler(testVisibleEntries(catalog), catalog.Resolve)
	reply := executeHelp(t, handler, []string{"코인"})

	if !strings.Contains(reply.Text, "별칭") {
		t.Fatalf("aliases section missing:\n%s", reply.Text)
	}
	if !strings.Contains(reply.Text, "ㅋ") {
		t.Fatalf("alias ㅋ missing:\n%s", reply.Text)
	}
}
