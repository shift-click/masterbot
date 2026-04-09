package command

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
)

func TestCoinHandlerExecuteRequiresArgs(t *testing.T) {
	h := newCoinHandlerForTest()

	reply := runCoinCommand(t, h, bot.CommandContext{Command: "코인"})
	if !strings.Contains(reply.Text, "코인명을 입력") {
		t.Fatalf("reply = %q", reply.Text)
	}
}

func TestCoinHandlerExecuteReturnsNotFoundOnUnknown(t *testing.T) {
	h := newCoinHandlerForTest()

	reply := runCoinCommand(t, h, bot.CommandContext{Command: "코인", Args: []string{"없는코인"}})
	if !strings.Contains(reply.Text, coinNotFoundText) {
		t.Fatalf("reply = %q", reply.Text)
	}
}

func TestCoinHandlerExecuteFormatsCEXQuote(t *testing.T) {
	h := newCoinHandlerForTest()
	seedBTCQuote(h.cache)

	reply := runCoinCommand(t, h, bot.CommandContext{Command: "코인", Args: []string{"BTC"}})
	if reply.Text == "" {
		t.Fatal("expected non-empty reply")
	}
	if !strings.Contains(strings.ToUpper(reply.Text), "BTC") {
		t.Fatalf("reply = %q", reply.Text)
	}

	reply = runCoinCommand(t, h, bot.CommandContext{Command: "코인", Args: []string{"빗코"}})
	if !strings.Contains(strings.ToUpper(reply.Text), "BTC") {
		t.Fatalf("generated alias reply = %q", reply.Text)
	}
}

func TestCoinHandlerExecuteQuantityUsesCachedPrice(t *testing.T) {
	h := newCoinHandlerForTest()
	seedBTCQuote(h.cache)

	reply := runCoinCommand(t, h, bot.CommandContext{Command: "코인", Args: []string{"BTC", "*", "2"}})
	if reply.Text == "" {
		t.Fatal("expected non-empty reply")
	}
	if !strings.Contains(strings.ToUpper(reply.Text), "BTC") {
		t.Fatalf("reply = %q", reply.Text)
	}
}

func TestCoinHandlerMatchBareQuery(t *testing.T) {
	h := newCoinHandlerForTest()

	args, ok := h.MatchBareQuery(context.Background(), "BTC")
	if !ok || len(args) != 1 {
		t.Fatalf("MatchBareQuery(BTC) = (%v, %v)", args, ok)
	}
	args, ok = h.MatchBareQuery(context.Background(), "monad")
	if !ok || len(args) != 1 {
		t.Fatalf("MatchBareQuery(monad) = (%v, %v)", args, ok)
	}
	args, ok = h.MatchBareQuery(context.Background(), "모나드")
	if !ok || len(args) != 1 {
		t.Fatalf("MatchBareQuery(모나드) = (%v, %v)", args, ok)
	}
	args, ok = h.MatchBareQuery(context.Background(), "MON")
	if !ok || len(args) != 1 {
		t.Fatalf("MatchBareQuery(MON) = (%v, %v)", args, ok)
	}
	args, ok = h.MatchBareQuery(context.Background(), "빗코")
	if !ok || len(args) != 1 {
		t.Fatalf("MatchBareQuery(빗코) = (%v, %v)", args, ok)
	}
	if _, ok := h.MatchBareQuery(context.Background(), "마나"); ok {
		t.Fatal("expected guarded generated alias 마나 to be rejected")
	}

	if _, ok := h.MatchBareQuery(context.Background(), "오늘 비트 뭐야"); ok {
		t.Fatal("expected multi-word non-quantity query to be rejected")
	}
}

func TestCoinHandlerMatchAutoQueryCandidateUsesLocalExactOnly(t *testing.T) {
	h := newCoinHandlerForTest()

	if ok := h.MatchAutoQueryCandidate(context.Background(), "오늘 비트 가격"); !ok {
		t.Fatal("expected local exact auto candidate for 오늘 비트 가격")
	}
	if ok := h.MatchAutoQueryCandidate(context.Background(), "오늘 모나드 가격"); !ok {
		t.Fatal("expected local exact auto candidate for 오늘 모나드 가격")
	}
	if ok := h.MatchAutoQueryCandidate(context.Background(), "오늘 빗코 가격"); !ok {
		t.Fatal("expected generated alias auto candidate for 오늘 빗코 가격")
	}
	if ok := h.MatchAutoQueryCandidate(context.Background(), "오늘 ondo 가격"); !ok {
		t.Fatal("expected generated catalog auto candidate for 오늘 ondo 가격")
	}
	if ok := h.MatchAutoQueryCandidate(context.Background(), "오늘 마나 가격"); ok {
		t.Fatal("expected guarded generated alias 마나 to be rejected")
	}
}

func newCoinHandlerForTest() *CoinHandler {
	logger := slog.Default()
	resolver := providers.NewCoinResolver(providers.NewCoinAliases(), nil, nil, logger)
	cache := scraper.NewCoinCache(logger)
	return NewCoinHandler(resolver, cache, nil, nil, nil, logger)
}

func seedBTCQuote(cache *scraper.CoinCache) {
	cache.OnBinanceUpdate(providers.BinanceTickerUpdate{
		Symbol:    "BTC",
		Price:     50000,
		PrevClose: 49000,
		Change:    1000,
		ChangePct: 2,
	})
	cache.OnUpbitUpdate(providers.UpbitTickerUpdate{
		Symbol:     "BTC",
		TradePrice: 70000000,
		PrevClose:  68000000,
		Change:     2000000,
		ChangePct:  0.029,
	})
	cache.UpdateForexRate(1400)
}

func runCoinCommand(t *testing.T, h *CoinHandler, cmd bot.CommandContext) bot.Reply {
	t.Helper()

	var reply bot.Reply
	cmd.Reply = func(_ context.Context, r bot.Reply) error {
		reply = r
		return nil
	}
	if cmd.Message.Msg == "" {
		cmd.Message = transport.Message{Msg: strings.Join(cmd.Args, " ")}
	}

	if err := h.Execute(context.Background(), cmd); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	return reply
}
