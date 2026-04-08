package formatter

import "strings"

// FormatURLSummary formats a Gemini URL summary response for KakaoTalk output.
func FormatURLSummary(summary string) string {
	var b strings.Builder
	b.WriteString("🔗 링크 요약\n─────────────────────\n\n")
	b.WriteString(summary)
	return b.String()
}
