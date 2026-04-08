package formatter

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shift-click/masterbot/internal/scraper/providers"
)

// FormatTweet formats a TweetData into a human-readable KakaoTalk message.
func FormatTweet(data providers.TweetData) string {
	var b strings.Builder
	b.WriteString("🐦 트윗 원문\n─────────────────────\n\n")

	// Author line
	if data.AuthorScreenName != "" || data.AuthorName != "" {
		if data.AuthorScreenName != "" && data.AuthorName != "" && data.AuthorName != data.AuthorScreenName {
			fmt.Fprintf(&b, "@%s (%s)\n", data.AuthorScreenName, data.AuthorName)
		} else if data.AuthorScreenName != "" {
			fmt.Fprintf(&b, "@%s\n", data.AuthorScreenName)
		} else {
			fmt.Fprintf(&b, "%s\n", data.AuthorName)
		}
		b.WriteString("─────────\n")
	}

	// Tweet body
	b.WriteString(data.Text)
	b.WriteString("\n\n")

	// Engagement stats
	var stats []string
	if data.Likes > 0 {
		stats = append(stats, fmt.Sprintf("❤️ %s", formatCount(data.Likes)))
	}
	if data.Retweets > 0 {
		stats = append(stats, fmt.Sprintf("🔁 %s", formatCount(data.Retweets)))
	}
	if data.CreatedAt != "" {
		if t, err := time.Parse("Mon Jan 02 15:04:05 +0000 2006", data.CreatedAt); err == nil {
			stats = append(stats, fmt.Sprintf("📅 %s", t.Format("2006.01.02")))
		}
	}
	if len(stats) > 0 {
		b.WriteString(strings.Join(stats, "  "))
	}

	return strings.TrimRight(b.String(), "\n")
}

// TweetNeedsAISummary returns true when the tweet text is long enough that
// an AI summary adds value over the raw text.
func TweetNeedsAISummary(text string) bool {
	return utf8.RuneCountInString(text) >= 500
}

func formatCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
