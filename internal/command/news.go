package command

import (
	"context"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/pkg/formatter"
)

const defaultNewsCount = 5

// NewsHandler handles the /뉴스 command.
type NewsHandler struct {
	descriptorSupport
	provider *providers.GoogleNews
}

func NewNewsHandlerReal(provider *providers.GoogleNews) *NewsHandler {
	return &NewsHandler{
		descriptorSupport: newDescriptorSupport("news"),
		provider:          provider,
	}
}

func (h *NewsHandler) Execute(ctx context.Context, cmd bot.CommandContext) error {
	items, err := h.provider.TopNews(ctx, defaultNewsCount)
	if err != nil {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "인기뉴스를 가져올 수 없습니다.",
		})
	}

	data := make([]formatter.NewsItemData, len(items))
	for i, item := range items {
		data[i] = formatter.NewsItemData{
			Rank:   i + 1,
			Title:  item.Title,
			Source: item.Source,
			Link:   item.Link,
		}
	}

	kst, _ := time.LoadLocation("Asia/Seoul")
	if kst == nil {
		kst = time.FixedZone("KST", 9*60*60)
	}

	text := formatter.FormatNews(data, time.Now().In(kst))
	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
	})
}

var _ bot.Handler = (*NewsHandler)(nil)
