package command

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/fortune"
	"github.com/shift-click/masterbot/internal/transport"
)

type stubFortuneService struct {
	calls int
}

func (s *stubFortuneService) Today(context.Context, string, time.Time) (fortune.DailyFortune, error) {
	s.calls++
	return fortune.DailyFortune{
		DateKey: "260409",
		Index:   12,
		Text: strings.Join([]string{
			"오늘은 차분한 리듬이 도움이 됩니다.",
			"",
			"🍽️ 추천 음식: 초밥",
			"└ 가볍게 기분 전환이 됩니다.",
			"🏃 추천 행동: 산책하기",
			"└ 머리를 맑게 해줍니다.",
			"🎨 추천 색상: 블루",
			"└ 마음을 가라앉혀 줍니다.",
			"🍀 행운의 숫자: 3, 7",
		}, "\n"),
	}, nil
}

func TestFortuneHandlerFormatsReply(t *testing.T) {
	t.Parallel()

	service := &stubFortuneService{}
	handler := NewFortuneHandler(service, nil)

	var reply bot.Reply
	err := handler.Execute(context.Background(), bot.CommandContext{
		Message: transport.Message{
			Msg:    "운세",
			Sender: "홍길동",
			Raw: transport.RawChatLog{
				ChatID: "room-1",
				UserID: "user-1",
			},
		},
		Reply: func(_ context.Context, r bot.Reply) error {
			reply = r
			return nil
		},
		Now: func() time.Time {
			return time.Date(2026, 4, 9, 10, 0, 0, 0, time.FixedZone("KST", 9*60*60))
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if service.calls != 1 {
		t.Fatalf("Today() calls = %d, want 1", service.calls)
	}
	if !strings.Contains(reply.Text, "🔮 홍길동님의 오늘의 운세") {
		t.Fatalf("header missing:\n%s", reply.Text)
	}
	for _, want := range []string{"추천 음식", "추천 행동", "추천 색상", "행운의 숫자"} {
		if !strings.Contains(reply.Text, want) {
			t.Fatalf("expected %q in reply:\n%s", want, reply.Text)
		}
	}
}

func TestFortuneHandlerRejectsMissingUserID(t *testing.T) {
	t.Parallel()

	handler := NewFortuneHandler(&stubFortuneService{}, nil)

	var reply bot.Reply
	err := handler.Execute(context.Background(), bot.CommandContext{
		Message: transport.Message{
			Msg:    "운세",
			Sender: "홍길동",
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
	if !strings.Contains(reply.Text, "사용자 정보를 확인할 수 없습니다") {
		t.Fatalf("unexpected reply:\n%s", reply.Text)
	}
}
