package bot

import (
	"regexp"
	"strings"
)

var quantifiedQueryPattern = regexp.MustCompile(`^[^\s()\d][^\s*xX×]*\s*[*xX×]\s*[\d.]+$`)

const (
	httpURLPrefix  = "http://"
	httpsURLPrefix = "https://"
)

func containsHTTPURL(content string) bool {
	return strings.Contains(content, httpURLPrefix) || strings.Contains(content, httpsURLPrefix)
}

func looksLikeExplicitBareQuery(content, prefix string) bool {
	content = strings.TrimSpace(content)
	if content == "" {
		return false
	}
	if prefix != "" && strings.HasPrefix(content, prefix) {
		return false
	}
	if strings.Contains(content, "\n") {
		return false
	}
	if containsHTTPURL(content) {
		return false
	}
	if quantifiedQueryPattern.MatchString(content) {
		return true
	}
	if strings.ContainsAny(content, "?!.,") {
		return false
	}

	fields := strings.Fields(content)
	if len(fields) == 0 || len(fields) > 2 {
		return false
	}
	for _, field := range fields {
		if bareQueryStopword(field) {
			return false
		}
	}
	return true
}

func looksLikeThemeShapedBareQuery(content, prefix string) bool {
	content = strings.TrimSpace(content)
	if content == "" {
		return false
	}
	if prefix != "" && strings.HasPrefix(content, prefix) {
		return false
	}
	if strings.Contains(content, "\n") {
		return false
	}
	if containsHTTPURL(content) {
		return false
	}
	if strings.ContainsAny(content, "?!.,") {
		return false
	}
	keyword, ok := extractThemeKeyword(content)
	if !ok {
		return false
	}
	return strings.TrimSpace(keyword) != ""
}

func extractThemeKeyword(content string) (string, bool) {
	content = strings.TrimSpace(content)
	for _, suffix := range []string{"관련주", "테마", "수혜주"} {
		if !strings.HasSuffix(content, suffix) {
			continue
		}
		keyword := strings.TrimSpace(strings.TrimSuffix(content, suffix))
		if keyword == "" {
			return "", false
		}
		return keyword, true
	}
	return "", false
}

func isJamoOnly(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, r := range s {
		if r == ' ' || r == '\t' {
			continue
		}
		if r < 0x3131 || r > 0x3163 {
			return false
		}
	}
	return true
}

func bareQueryStopword(token string) bool {
	switch strings.TrimSpace(token) {
	case "응", "네", "그래", "아니", "왜", "뭐", "뭐야", "뭐임", "오늘", "내일", "어제", "지금", "얼마", "어때", "추천", "알려줘", "알려", "살까", "사도", "어디",
		"오키", "오케이", "아아", "아하", "헐", "대박", "진짜", "레전드":
		return true
	default:
		return false
	}
}

func looksLikeLocalAutoCandidate(content string) bool {
	content = strings.TrimSpace(content)
	if content == "" {
		return false
	}
	if strings.Contains(content, "\n") {
		return false
	}
	if containsHTTPURL(content) {
		return false
	}
	fields := strings.Fields(content)
	if len(fields) == 0 || len(fields) > 3 {
		return false
	}

	candidates := 0
	for _, field := range fields {
		normalized := strings.Trim(field, "?!.,:;~()[]{}<>\"'")
		if normalized == "" || autoQueryWrapperToken(normalized) || bareQueryStopword(normalized) {
			continue
		}
		candidates++
	}
	return candidates == 1
}

func autoQueryWrapperToken(token string) bool {
	switch strings.TrimSpace(token) {
	case "오늘", "내일", "어제", "지금", "가격", "시세", "얼마", "어때", "왜", "뭐", "뭐야", "뭐임", "좀", "봐", "봐줘", "알려", "알려줘":
		return true
	default:
		return false
	}
}
