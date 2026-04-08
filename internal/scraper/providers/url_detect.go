package providers

import (
	"regexp"
	"strings"
)

var reWebURL = regexp.MustCompile(`https?://[^\s<>"{}|\\^` + "`" + `\[\]]+`)

// IsWebURL checks whether the text contains an HTTP(S) URL.
func IsWebURL(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "http://") || strings.Contains(lower, "https://")
}

// ExtractWebURL extracts the first HTTP(S) URL from text.
func ExtractWebURL(text string) string {
	return reWebURL.FindString(text)
}
