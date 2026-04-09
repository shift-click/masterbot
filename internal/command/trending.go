package command

import (
	"context"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/pkg/formatter"
)

// TrendingHandler handles the /실검 command.
type TrendingHandler struct {
	descriptorSupport
	provider *providers.GoogleTrends
}

func NewTrendingHandler(provider *providers.GoogleTrends) *TrendingHandler {
	return &TrendingHandler{
		descriptorSupport: newDescriptorSupport("trending"),
		provider:          provider,
	}
}

func (h *TrendingHandler) Execute(ctx context.Context, cmd bot.CommandContext) error {
	items, err := h.provider.Trends(ctx)
	if err != nil {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "실시간 검색 트렌드를 가져올 수 없습니다.",
		})
	}

	data := make([]formatter.TrendItemData, len(items))
	for i, item := range items {
		data[i] = formatter.TrendItemData{
			Rank:   i + 1,
			Title:  item.Title,
			Change: formatter.TrendChange(item.Change),
		}
	}

	kst, _ := time.LoadLocation("Asia/Seoul")
	if kst == nil {
		kst = time.FixedZone("KST", 9*60*60)
	}

	text := formatter.FormatTrending(data, time.Now().In(kst))
	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
	})
}

var _ bot.Handler = (*TrendingHandler)(nil)
