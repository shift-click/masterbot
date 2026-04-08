package formatter

import (
	"fmt"
	"strings"
	"time"
)

// TrendChange mirrors providers.TrendChange for formatter-level use.
type TrendChange int

const (
	TrendChangeNone TrendChange = iota
	TrendChangeUp
	TrendChangeDown
	TrendChangeNew
)

// TrendItemData holds a single trending item for formatting.
type TrendItemData struct {
	Rank    int
	Title   string
	Change  TrendChange
}

// FormatTrending formats a list of trending search items.
func FormatTrending(items []TrendItemData, t time.Time) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("🔍 실시간 검색 트렌드 Top%d (%s)\n", len(items), t.Format("15:04")))

	for _, item := range items {
		changeIcon := changeEmoji(item.Change)
		if changeIcon != "" {
			b.WriteString(fmt.Sprintf("\n%2d위 %s %s", item.Rank, changeIcon, item.Title))
		} else {
			b.WriteString(fmt.Sprintf("\n%2d위    %s", item.Rank, item.Title))
		}
	}

	return b.String()
}

func changeEmoji(c TrendChange) string {
	switch c {
	case TrendChangeUp:
		return "🔺"
	case TrendChangeDown:
		return "🔻"
	case TrendChangeNone:
		return "➡️"
	case TrendChangeNew:
		return "🆕"
	default:
		return ""
	}
}
