package command

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/lotto"
	"github.com/shift-click/masterbot/internal/transport"
)

type stubLottoService struct {
	summaryCalls   int
	recommendCalls int
	registerCalls  int
	queryCalls     int
	deleteCalls    int

	lastValues []int
	lastLineNo *int
}

func (s *stubLottoService) Summary(context.Context, string, time.Time) (lotto.SummaryResult, error) {
	s.summaryCalls++
	return lotto.SummaryResult{
		Draw: &lotto.Draw{
			Round:       1218,
			DrawDate:    time.Date(2026, 4, 4, 0, 0, 0, 0, time.FixedZone("KST", 9*60*60)),
			Numbers:     [6]int{3, 28, 31, 32, 42, 45},
			BonusNumber: 25,
			Rank1Prize:  1714482042,
			Rank2Prize:  64293077,
			Rank3Prize:  1780356,
		},
	}, nil
}

func (s *stubLottoService) Recommend(context.Context, string, string, string, time.Time) (lotto.RecommendResult, error) {
	s.recommendCalls++
	return lotto.RecommendResult{
		DisplayName: "홍길동",
		Round:       1219,
		Tickets: []lotto.TicketLine{
			{LineNo: 1, Numbers: [6]int{1, 2, 3, 4, 5, 6}},
		},
	}, nil
}

func (s *stubLottoService) RegisterManual(_ context.Context, _, _, _ string, values []int, _ time.Time) (lotto.RegisterResult, error) {
	s.registerCalls++
	s.lastValues = append([]int(nil), values...)
	return lotto.RegisterResult{
		DisplayName: "홍길동",
		Round:       1219,
		TotalLines:  1,
		DrawDate:    time.Date(2026, 4, 11, 0, 0, 0, 0, time.FixedZone("KST", 9*60*60)),
		Added: []lotto.TicketLine{
			{LineNo: 1, Numbers: [6]int{1, 2, 3, 4, 5, 6}},
		},
	}, nil
}

func (s *stubLottoService) QueryMine(context.Context, string, string, string, time.Time) (lotto.QueryResult, error) {
	s.queryCalls++
	return lotto.QueryResult{
		DisplayName:         "홍길동",
		LatestOfficialRound: 1218,
		CurrentRound:        1219,
		CurrentTickets: []lotto.TicketLine{
			{LineNo: 1, Numbers: [6]int{1, 2, 3, 4, 5, 6}},
		},
		AwaitingDraw: true,
	}, nil
}

func (s *stubLottoService) DeleteMine(context.Context, string, string, string, *int, time.Time) (lotto.DeleteResult, error) {
	s.deleteCalls++
	return lotto.DeleteResult{
		DisplayName: "홍길동",
		Deleted: []lotto.TicketLine{
			{LineNo: 1, Numbers: [6]int{1, 2, 3, 4, 5, 6}},
		},
	}, nil
}

func executeLotto(t *testing.T, handler *LottoHandler, msg string) bot.Reply {
	t.Helper()
	var captured bot.Reply
	err := handler.Execute(context.Background(), bot.CommandContext{
		Message: transport.Message{
			Msg:    msg,
			Sender: "홍길동",
			Raw: transport.RawChatLog{
				ChatID: "room-1",
				UserID: "user-1",
			},
		},
		Args:  strings.Fields(strings.TrimSpace(strings.TrimPrefix(msg, strings.Fields(msg)[0]))),
		Reply: func(_ context.Context, reply bot.Reply) error { captured = reply; return nil },
		Now: func() time.Time {
			return time.Date(2026, 4, 8, 10, 0, 0, 0, time.FixedZone("KST", 9*60*60))
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	return captured
}

func TestLottoHandlerRoutesRecommendAndQuery(t *testing.T) {
	t.Parallel()

	service := &stubLottoService{}
	handler := NewLottoHandler(service, nil)

	reply := executeLotto(t, handler, "로또 추천")
	if service.recommendCalls != 1 {
		t.Fatalf("recommendCalls = %d, want 1", service.recommendCalls)
	}
	if !strings.Contains(reply.Text, "추천 번호") {
		t.Fatalf("unexpected recommend reply: %q", reply.Text)
	}

	reply = executeLotto(t, handler, "!로또")
	if service.queryCalls != 1 {
		t.Fatalf("queryCalls = %d, want 1", service.queryCalls)
	}
	if !strings.Contains(reply.Text, "아직 추첨 전입니다") {
		t.Fatalf("unexpected query reply: %q", reply.Text)
	}
}

func TestLottoHandlerRoutesRegisterAndDelete(t *testing.T) {
	t.Parallel()

	service := &stubLottoService{}
	handler := NewLottoHandler(service, nil)

	reply := executeLotto(t, handler, "!로또 1 2 3 4 5 6")
	if service.registerCalls != 1 {
		t.Fatalf("registerCalls = %d, want 1", service.registerCalls)
	}
	if len(service.lastValues) != 6 || service.lastValues[0] != 1 || service.lastValues[5] != 6 {
		t.Fatalf("lastValues = %v", service.lastValues)
	}
	if !strings.Contains(reply.Text, "등록되었습니다") {
		t.Fatalf("unexpected register reply: %q", reply.Text)
	}

	reply = executeLotto(t, handler, "!로또 삭제")
	if service.deleteCalls != 1 {
		t.Fatalf("deleteCalls = %d, want 1", service.deleteCalls)
	}
	if !strings.Contains(reply.Text, "삭제완료") {
		t.Fatalf("unexpected delete reply: %q", reply.Text)
	}
}

func TestLottoHandlerRejectsInvalidNumbers(t *testing.T) {
	t.Parallel()

	service := &stubLottoService{}
	handler := NewLottoHandler(service, nil)

	reply := executeLotto(t, handler, "!로또 1 2 셋 4 5 6")
	if !strings.Contains(reply.Text, "올바르지 못한 숫자입니다") {
		t.Fatalf("unexpected invalid reply: %q", reply.Text)
	}
	if service.registerCalls != 0 {
		t.Fatalf("registerCalls = %d, want 0", service.registerCalls)
	}
}
