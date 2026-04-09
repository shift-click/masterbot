package command

import "strings"

func extractAutoCandidate(content string, maxFields int) (string, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", false
	}

	fields := strings.Fields(content)
	if len(fields) == 0 || len(fields) > maxFields {
		return "", false
	}

	candidates := make([]string, 0, len(fields))
	for _, field := range fields {
		normalized := strings.Trim(field, "?!.,:;~()[]{}<>\"'")
		if normalized == "" || autoQueryNoiseToken(normalized) {
			continue
		}
		candidates = append(candidates, normalized)
	}

	if len(candidates) != 1 {
		return "", false
	}
	return candidates[0], true
}

func autoQueryNoiseToken(token string) bool {
	switch strings.TrimSpace(token) {
	case "오늘", "내일", "어제", "지금", "가격", "시세", "얼마", "어때", "왜", "뭐", "뭐야", "뭐임", "좀", "봐", "봐줘", "알려", "알려줘",
		"응", "네", "그래", "아니", "헐", "대박", "진짜", "레전드":
		return true
	default:
		return false
	}
}
