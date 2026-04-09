package providers

import (
	"regexp"
	"strings"
)

var (
	youtubeURLPatterns = []string{
		"youtube.com/watch",
		"youtu.be/",
		"youtube.com/shorts/",
		"m.youtube.com/watch",
	}

	reYouTubeURL = regexp.MustCompile(
		`https?://(?:` +
			`(?:www\.|m\.)?youtube\.com/(?:watch\?[^\s]*v=[A-Za-z0-9_-]{11}|shorts/[A-Za-z0-9_-]{11})` +
			`|youtu\.be/[A-Za-z0-9_-]{11}` +
			`)\S*`)

	reVideoID = regexp.MustCompile(`(?:v=|youtu\.be/|shorts/)([A-Za-z0-9_-]{11})`)
)

// IsYouTubeURL checks whether the text contains a YouTube URL.
func IsYouTubeURL(text string) bool {
	lower := strings.ToLower(text)
	for _, pattern := range youtubeURLPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// ExtractYouTubeURL extracts the first YouTube URL from text.
func ExtractYouTubeURL(text string) string {
	return reYouTubeURL.FindString(text)
}

// ExtractVideoID extracts the YouTube video ID from a URL string.
func ExtractVideoID(rawURL string) string {
	m := reVideoID.FindStringSubmatch(rawURL)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}
