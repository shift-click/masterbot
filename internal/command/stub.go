package command

import (
	"context"
	"fmt"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/commandmeta"
	"github.com/shift-click/masterbot/internal/transport"
)

type StubHandler struct {
	descriptorSupport
}

func NewFinanceHandler() bot.Handler {
	return StubHandler{descriptorSupport: descriptorSupport{descriptor: commandmeta.Must("finance")}}
}


func NewAIHandler() bot.Handler {
	return StubHandler{descriptorSupport: descriptorSupport{descriptor: commandmeta.Must("ai")}}
}

func (h StubHandler) Execute(ctx context.Context, cmd bot.CommandContext) error {
	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: fmt.Sprintf("%s 명령어는 아직 구현 중입니다.", h.Name()),
	})
}
