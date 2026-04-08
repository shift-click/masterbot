package command

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
)

func TestExtractAutoCandidate(t *testing.T) {
	t.Parallel()

	candidate, ok := extractAutoCandidate("  삼성전자 가격  ", 3)
	if !ok || candidate != "삼성전자" {
		t.Fatalf("candidate = (%q,%v)", candidate, ok)
	}

	candidate, ok = extractAutoCandidate("오늘 삼성전자", 2)
	if !ok || candidate != "삼성전자" {
		t.Fatalf("candidate = (%q,%v)", candidate, ok)
	}

	if _, ok := extractAutoCandidate("삼성전자 애플", 4); ok {
		t.Fatal("expected multi-candidate input to fail")
	}
}

func TestDescriptorSupportAccessors(t *testing.T) {
	t.Parallel()

	support := newDescriptorSupport("coin")
	if support.Name() == "" {
		t.Fatal("expected descriptor name")
	}
	if support.Description() == "" {
		t.Fatal("expected descriptor description")
	}

	aliases := support.Aliases()
	if len(aliases) == 0 {
		t.Fatal("expected descriptor aliases")
	}
	aliases[0] = "mutated"
	if support.Aliases()[0] == "mutated" {
		t.Fatal("aliases should be returned as a copy")
	}
}

func TestStubHandlersExecute(t *testing.T) {
	t.Parallel()

	for _, handler := range []bot.Handler{NewFinanceHandler(), NewAIHandler()} {
		reply := runSimpleCommand(t, handler, nil)
		if !strings.Contains(reply.Text, "아직 구현 중") {
			t.Fatalf("unexpected reply: %q", reply.Text)
		}
	}
}

func TestFootballHandlerExecuteAndMatchBareQuery(t *testing.T) {
	t.Parallel()

	cache := scraper.NewFootballCache(time.Hour)
	handler := NewFootballHandler(cache, slog.Default())
	today := todayKST()
	dateKey := today.Format("20060102")

	epl, ok := providers.LookupFootballLeague("epl")
	if !ok {
		t.Fatal("expected EPL league")
	}
	cache.SetMatches(epl.ID, dateKey, []providers.FootballMatch{
		{
			ID:        "m1",
			HomeTeam:  "Tottenham",
			AwayTeam:  "Arsenal",
			HomeScore: 2,
			AwayScore: 1,
			Status:    providers.MatchFinished,
		},
	})
	cache.SetEvents("m1", []providers.MatchEvent{{Type: providers.EventGoal, Player: "Son", Minute: "12'"}})
	cache.SetOdds("m1", 1.9, 3.4, 4.0)

	reply := runSimpleCommand(t, handler, []string{"EPL"})
	if !strings.Contains(reply.Text, "EPL") {
		t.Fatalf("unexpected reply: %q", reply.Text)
	}
	if !strings.Contains(reply.Text, "토트넘") && !strings.Contains(reply.Text, "Tottenham") {
		t.Fatalf("unexpected reply: %q", reply.Text)
	}

	if _, ok := handler.MatchBareQuery(context.Background(), "EPL"); !ok {
		t.Fatal("expected bare query match for EPL")
	}
	if _, ok := handler.MatchBareQuery(context.Background(), ""); ok {
		t.Fatal("expected empty query to be rejected")
	}

	badReply := runSimpleCommand(t, handler, []string{"없는리그"})
	if !strings.Contains(badReply.Text, "알 수 없는 리그") {
		t.Fatalf("unexpected invalid league reply: %q", badReply.Text)
	}
}

func TestBaseballHandlerExecuteAndMatchBareQuery(t *testing.T) {
	t.Parallel()

	cache := scraper.NewBaseballCache()
	handler := NewBaseballHandler(cache, slog.Default())
	today := todayKST()
	dateKey := today.Format("20060102")

	kbo, ok := providers.LookupBaseballLeague("kbo")
	if !ok {
		t.Fatal("expected KBO league")
	}
	cache.SetMatches(kbo.ID, dateKey, []providers.BaseballMatch{
		{
			ID:        "b1",
			League:    "kbo",
			HomeTeam:  "두산",
			AwayTeam:  "LG",
			HomeScore: 4,
			AwayScore: 3,
			Status:    providers.BaseballFinished,
		},
	})

	reply := runSimpleCommand(t, handler, []string{"KBO"})
	if !strings.Contains(reply.Text, "KBO") {
		t.Fatalf("unexpected reply: %q", reply.Text)
	}
	if !strings.Contains(reply.Text, "두산") {
		t.Fatalf("unexpected reply: %q", reply.Text)
	}

	if _, ok := handler.MatchBareQuery(context.Background(), "KBO"); !ok {
		t.Fatal("expected bare query match for KBO")
	}

	badReply := runSimpleCommand(t, handler, []string{"없는리그"})
	if !strings.Contains(badReply.Text, "알 수 없는 리그") {
		t.Fatalf("unexpected invalid league reply: %q", badReply.Text)
	}
}

func TestEsportsHandlerExecuteAndMatchBareQuery(t *testing.T) {
	t.Parallel()

	cache := scraper.NewEsportsCache()
	handler := NewEsportsHandler(cache, slog.Default())
	today := todayKST()
	dateKey := today.Format("20060102")

	lck, ok := providers.LookupEsportsLeague("lck")
	if !ok {
		t.Fatal("expected LCK league")
	}
	cache.SetMatches(lck.ID, dateKey, []providers.EsportsMatch{
		{
			ID:        "e1",
			LeagueID:  lck.ID,
			Team1:     "T1",
			Team1Code: "T1",
			Team2:     "GEN",
			Team2Code: "GEN",
			Score1:    2,
			Score2:    1,
			BestOf:    3,
			Status:    providers.MatchFinished,
		},
	})

	reply := runSimpleCommand(t, handler, []string{"LCK"})
	if !strings.Contains(reply.Text, "LCK") {
		t.Fatalf("unexpected reply: %q", reply.Text)
	}
	if !strings.Contains(reply.Text, "T1") {
		t.Fatalf("unexpected reply: %q", reply.Text)
	}

	if _, ok := handler.MatchBareQuery(context.Background(), "LCK"); !ok {
		t.Fatal("expected bare query match for LCK")
	}

	badReply := runSimpleCommand(t, handler, []string{"없는리그"})
	if !strings.Contains(badReply.Text, "알 수 없는 리그") {
		t.Fatalf("unexpected invalid league reply: %q", badReply.Text)
	}
}

func TestForexHandlerExecute(t *testing.T) {
	t.Parallel()

	forex := providers.NewDunamuForex(slog.Default())
	handler := NewForexHandler(forex)

	emptyReply := runSimpleCommand(t, handler, nil)
	if !strings.Contains(emptyReply.Text, "아직 가져오지 못했습니다") {
		t.Fatalf("unexpected empty-rates reply: %q", emptyReply.Text)
	}

	forex.SetRatesForTest(providers.MultiForexRates{
		Rates: map[string]providers.CurrencyRate{
			"USD": {CurrencyCode: "USD", BasePrice: 1320.5, CurrencyUnit: 1},
			"JPY": {CurrencyCode: "JPY", BasePrice: 910.1, CurrencyUnit: 100},
		},
		UpdatedAt: time.Now(),
	})
	reply := runSimpleCommand(t, handler, nil)
	if !strings.Contains(reply.Text, "USD") {
		t.Fatalf("unexpected forex reply: %q", reply.Text)
	}
	if _, ok := handler.MatchBareQuery(context.Background(), "환율"); ok {
		t.Fatal("forex bare query should always be false")
	}
}

func TestToBaseballMatchDataTranslatesNonKBO(t *testing.T) {
	t.Parallel()

	data := toBaseballMatchData([]providers.BaseballMatch{
		{League: "mlb", HomeTeam: "Los Angeles Dodgers", AwayTeam: "San Diego Padres"},
	})
	if len(data) != 1 {
		t.Fatalf("len(data) = %d", len(data))
	}
	if data[0].HomeTeam == "Los Angeles Dodgers" {
		t.Fatalf("expected translated name, got %q", data[0].HomeTeam)
	}
}

func TestTodayKST(t *testing.T) {
	t.Parallel()

	now := todayKST()
	if now.Location() == time.UTC {
		t.Fatal("expected KST-like location, got UTC")
	}
}

func runSimpleCommand(t *testing.T, handler bot.Handler, args []string) bot.Reply {
	t.Helper()

	var reply bot.Reply
	err := handler.Execute(context.Background(), bot.CommandContext{
		Args: args,
		Message: transport.Message{
			Raw: transport.RawChatLog{
				ChatID: "room-1",
			},
		},
		Reply: func(_ context.Context, r bot.Reply) error {
			reply = r
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	return reply
}
