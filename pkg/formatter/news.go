package formatter

import (
	"fmt"
	"strings"
	"time"
)

// NewsItemData holds a single news item for formatting.
type NewsItemData struct {
	Rank   int
	Title  string
	Source string
	Link   string
}

// FormatNews formats a list of popular news items.
func FormatNews(items []NewsItemData, t time.Time) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("📰 실시간 인기뉴스 Top%d (%s)", len(items), t.Format("15:04")))

	for _, item := range items {
		b.WriteString("\n\n")
		if item.Source != "" {
			b.WriteString(fmt.Sprintf("%d위. [%s] %s", item.Rank, item.Source, item.Title))
		} else {
			b.WriteString(fmt.Sprintf("%d위. %s", item.Rank, item.Title))
		}
		if item.Link != "" {
			b.WriteByte('\n')
			b.WriteString(item.Link)
		}
	}

	return b.String()
}
