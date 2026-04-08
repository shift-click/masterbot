package formatter

import "strings"

// FormatYouTubeSummary formats a Gemini summary response for KakaoTalk output.
func FormatYouTubeSummary(title, summary string) string {
	var b strings.Builder

	b.WriteString("📺 ")
	b.WriteString(title)
	b.WriteString("\n─────────────────────\n\n")
	b.WriteString(summary)

	return b.String()
}
