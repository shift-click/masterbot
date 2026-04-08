package command

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
)

// fakeIndexProvider is a test double for indexProvider.
type fakeIndexProvider struct {
	domesticResult providers.IndexQuote
	domesticErr    error
	worldResult    providers.IndexQuote
	worldErr       error
	domesticCalled string
	worldCalled    string
}

func (f *fakeIndexProvider) FetchDomesticIndex(_ context.Context, code string) (providers.IndexQuote, error) {
	f.domesticCalled = code
	return f.domesticResult, f.domesticErr
}

func (f *fakeIndexProvider) FetchWorldIndex(_ context.Context, reutersCode string) (providers.IndexQuote, error) {
	f.worldCalled = reutersCode
	return f.worldResult, f.worldErr
}

func newTestIndexHandler(provider indexProvider) *IndexHandler {
	return NewIndexHandler(provider, nil)
}

func runIndex(t *testing.T, h *IndexHandler, cmd bot.CommandContext) bot.Reply {
	t.Helper()
	var replied bot.Reply
	cmd.Reply = func(_ context.Context, r bot.Reply) error {
		replied = r
		return nil
	}
	if cmd.Message.Raw == (transport.RawChatLog{}) {
		cmd.Message.Raw = transport.RawChatLog{UserID: "testuser"}
	}
	_ = h.Execute(context.Background(), cmd)
	return replied
}

// --- MatchBareQuery tests ---

func TestIndexHandlerMatchBareQuery_KnownAliases(t *testing.T) {
	t.Parallel()
	h := newTestIndexHandler(&fakeIndexProvider{})

	cases := []string{
		"코스피", "코스닥", "코스피200",
		"나스닥", "나스닥100",
		"다우", "다우존스",
		"sp500", "s&p500",
		"닛케이", "항셍", "항생", "상해", "상하이",
	}

	for _, input := range cases {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			args, ok := h.MatchBareQuery(context.Background(), input)
			if !ok {
				t.Fatalf("MatchBareQuery(%q) = false, want true", input)
			}
			if len(args) != 1 || args[0] != input {
				t.Fatalf("MatchBareQuery(%q) args = %v, want [%q]", input, args, input)
			}
		})
	}
}

func TestIndexHandlerMatchBareQuery_UnknownAlias(t *testing.T) {
	t.Parallel()
	h := newTestIndexHandler(&fakeIndexProvider{})

	cases := []string{"삼성전자", "AAPL", "bitcoin", "모르는거", ""}
	for _, input := range cases {
		input := input
		t.Run(input+"_no_match", func(t *testing.T) {
			t.Parallel()
			_, ok := h.MatchBareQuery(context.Background(), input)
			if ok {
				t.Fatalf("MatchBareQuery(%q) = true, want false", input)
			}
		})
	}
}

func TestIndexHandlerMatchBareQuery_MultiWord(t *testing.T) {
	t.Parallel()
	h := newTestIndexHandler(&fakeIndexProvider{})

	_, ok := h.MatchBareQuery(context.Background(), "코스피 지금")
	if ok {
		t.Fatal("MatchBareQuery multi-word should return false")
	}
}

// --- Execute tests ---

func TestIndexHandlerExecute_NoArgs_ReturnsHelp(t *testing.T) {
	t.Parallel()
	h := newTestIndexHandler(&fakeIndexProvider{})
	reply := runIndex(t, h, bot.CommandContext{Command: "지수"})
	if reply.Type != transport.ReplyTypeText {
		t.Fatalf("type = %v, want text", reply.Type)
	}
	if !strings.Contains(reply.Text, "지수명") {
		t.Fatalf("expected help text, got %q", reply.Text)
	}
}

func TestIndexHandlerExecute_DomesticIndex_Success(t *testing.T) {
	t.Parallel()
	provider := &fakeIndexProvider{
		domesticResult: providers.IndexQuote{
			Code:            "KOSPI",
			Name:            "코스피",
			Price:           "2,540.00",
			Change:          "12.34",
			ChangePercent:   "0.49",
			ChangeDirection: "RISING",
		},
	}
	h := newTestIndexHandler(provider)

	reply := runIndex(t, h, bot.CommandContext{
		Command: "지수",
		Args:    []string{"코스피"},
	})
	if reply.Type != transport.ReplyTypeText {
		t.Fatalf("type = %v, want text", reply.Type)
	}
	if !strings.Contains(reply.Text, "코스피") {
		t.Fatalf("expected 코스피 in reply, got %q", reply.Text)
	}
	if !strings.Contains(reply.Text, "2,540.00") {
		t.Fatalf("expected price in reply, got %q", reply.Text)
	}
	if provider.domesticCalled != "KOSPI" {
		t.Fatalf("expected FetchDomesticIndex called with KOSPI, got %q", provider.domesticCalled)
	}
	if provider.worldCalled != "" {
		t.Fatalf("expected FetchWorldIndex not called, but got %q", provider.worldCalled)
	}
}

func TestIndexHandlerExecute_WorldIndex_Success(t *testing.T) {
	t.Parallel()
	provider := &fakeIndexProvider{
		worldResult: providers.IndexQuote{
			Code:            ".IXIC",
			Name:            "나스닥 종합",
			Price:           "21,879.18",
			Change:          "38.23",
			ChangePercent:   "0.18",
			ChangeDirection: "RISING",
		},
	}
	h := newTestIndexHandler(provider)

	reply := runIndex(t, h, bot.CommandContext{
		Command: "지수",
		Args:    []string{"나스닥"},
	})
	if !strings.Contains(reply.Text, "나스닥") {
		t.Fatalf("expected 나스닥 in reply, got %q", reply.Text)
	}
	if !strings.Contains(reply.Text, "21,879.18") {
		t.Fatalf("expected price in reply, got %q", reply.Text)
	}
	if provider.worldCalled != ".IXIC" {
		t.Fatalf("expected FetchWorldIndex called with .IXIC, got %q", provider.worldCalled)
	}
}

func TestIndexHandlerExecute_UnknownIndex_ReturnsError(t *testing.T) {
	t.Parallel()
	h := newTestIndexHandler(&fakeIndexProvider{})

	reply := runIndex(t, h, bot.CommandContext{
		Command: "지수",
		Args:    []string{"모르는지수"},
	})
	if !strings.Contains(reply.Text, "알 수 없는") {
		t.Fatalf("expected error message, got %q", reply.Text)
	}
}

func TestIndexHandlerExecute_FetchError_ReturnsError(t *testing.T) {
	t.Parallel()
	provider := &fakeIndexProvider{
		domesticErr: errors.New("network error"),
	}
	h := newTestIndexHandler(provider)

	reply := runIndex(t, h, bot.CommandContext{
		Command: "지수",
		Args:    []string{"코스닥"},
	})
	if reply.Text == "" {
		t.Fatal("expected error reply, got empty")
	}
}

func TestIndexHandlerExecute_DowJones_Routes_Correctly(t *testing.T) {
	t.Parallel()

	for _, alias := range []string{"다우", "다우존스", "dow", "dji"} {
		alias := alias
		t.Run(alias, func(t *testing.T) {
			t.Parallel()
			provider2 := &fakeIndexProvider{worldResult: providers.IndexQuote{Code: ".DJI", Name: "다우존스", Price: "46,504.00"}}
			h2 := newTestIndexHandler(provider2)
			reply := runIndex(t, h2, bot.CommandContext{
				Command: "지수",
				Args:    []string{alias},
			})
			if reply.Text == "" {
				t.Fatalf("alias %q returned empty reply", alias)
			}
			if provider2.worldCalled != ".DJI" {
				t.Fatalf("alias %q: expected .DJI, got %q", alias, provider2.worldCalled)
			}
		})
	}
}

func TestIndexHandlerExecute_BareQuery_ErrHandled(t *testing.T) {
	t.Parallel()
	provider := &fakeIndexProvider{
		domesticResult: providers.IndexQuote{
			Code:  "KOSPI",
			Name:  "코스피",
			Price: "2,540.00",
		},
	}
	h := newTestIndexHandler(provider)

	// Bare query (no Command set) should return ErrHandled after reply.
	var replied bot.Reply
	cmd := bot.CommandContext{
		// Command is empty — signals bare query path
		Args: []string{"코스피"},
		Reply: func(_ context.Context, r bot.Reply) error {
			replied = r
			return nil
		},
		Message: transport.Message{Raw: transport.RawChatLog{UserID: "user"}},
	}
	err := h.Execute(context.Background(), cmd)
	if !errors.Is(err, bot.ErrHandled) {
		t.Fatalf("bare query: err = %v, want ErrHandled", err)
	}
	if !strings.Contains(replied.Text, "코스피") {
		t.Fatalf("bare query: reply = %q", replied.Text)
	}
}
