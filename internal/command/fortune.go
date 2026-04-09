package command

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/fortune"
	"github.com/shift-click/masterbot/internal/transport"
)

type fortuneService interface {
	Today(context.Context, string, time.Time) (fortune.DailyFortune, error)
}

type FortuneHandler struct {
	descriptorSupport
	service fortuneService
	logger  *slog.Logger
}

func NewFortuneHandler(service fortuneService, logger *slog.Logger) *FortuneHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &FortuneHandler{
		descriptorSupport: newDescriptorSupport("fortune"),
		service:           service,
		logger:            logger.With("component", "fortune_handler"),
	}
}

func (h *FortuneHandler) Execute(ctx context.Context, cmd bot.CommandContext) error {
	userID := strings.TrimSpace(cmd.Message.Raw.UserID)
	if userID == "" {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "운세 기능을 위한 사용자 정보를 확인할 수 없습니다.",
		})
	}

	now := time.Now()
	if cmd.Now != nil {
		now = cmd.Now()
	}

	result, err := h.service.Today(ctx, userID, now)
	if err != nil {
		h.logger.Warn("fortune lookup failed", "error", err)
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "오늘의 운세를 가져올 수 없습니다.",
		})
	}

	displayName := strings.TrimSpace(cmd.Message.Sender)
	if displayName == "" {
		displayName = "사용자"
	}

	text := fmt.Sprintf("🔮 %s님의 오늘의 운세\n━━━━━━━━━━━━━━\n%s", displayName, strings.TrimSpace(result.Text))
	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
	})
}
