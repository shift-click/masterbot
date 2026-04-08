package providers

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// NPB fetches baseball data by scraping Yahoo Japan Baseball.
type NPB struct {
	client *BreakerHTTPClient
	logger *slog.Logger
}

// NewNPB creates a new NPB (Yahoo Japan) provider.
func NewNPB(logger *slog.Logger) *NPB {
	if logger == nil {
		logger = slog.Default()
	}
	return &NPB{
		client: DefaultBreakerClient(15 * time.Second, "npb", logger),
		logger: logger.With("component", "npb"),
	}
}

// FetchSchedule fetches NPB games for a given date (YYYY-MM-DD format).
func (n *NPB) FetchSchedule(ctx context.Context, date string) ([]BaseballMatch, error) {
	// Yahoo Japan expects date as YYYY-MM-DD
	url := fmt.Sprintf("https://baseball.yahoo.co.jp/npb/schedule/?date=%s", date)

	body, err := n.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("npb schedule: %w", err)
	}

	matches, err := parseNPBHTML(string(body), date)
	if err != nil {
		return nil, fmt.Errorf("npb parse: %w", err)
	}
	return matches, nil
}

func (n *NPB) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "ja")

	resp, err := n.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

// Regex patterns for parsing Yahoo Japan NPB HTML.
var (
	npbGameItemRe   = regexp.MustCompile(`(?s)<li class="bb-score__item">\s*<a[^>]*>(.*?)</a>\s*</li>`)
	npbHomeTeamRe   = regexp.MustCompile(`<p class="bb-score__homeLogo[^"]*">([^<]+)</p>`)
	npbAwayTeamRe   = regexp.MustCompile(`<p class="bb-score__awayLogo[^"]*">([^<]+)</p>`)
	npbScoreLeftRe  = regexp.MustCompile(`bb-score__score--left[^>]*>(\d+)<`)
	npbScoreRightRe = regexp.MustCompile(`bb-score__score--right[^>]*>(\d+)<`)
	npbLinkTextRe   = regexp.MustCompile(`<p class="bb-score__link">([^<]+)</p>`)
	npbVenueRe      = regexp.MustCompile(`<span class="bb-score__venue">([^<]+)</span>`)
	npbInningRe     = regexp.MustCompile(`(\d+)回([表裏])`)
	npbTimeRe       = regexp.MustCompile(`(\d{1,2}):(\d{2})`)
)

func parseNPBHTML(html string, date string) ([]BaseballMatch, error) {
	items := npbGameItemRe.FindAllStringSubmatch(html, -1)
	if len(items) == 0 {
		return nil, nil // No games found (not an error)
	}

	jst, _ := time.LoadLocation("Asia/Tokyo")
	if jst == nil {
		jst = time.FixedZone("JST", 9*60*60)
	}

	var matches []BaseballMatch
	for i, item := range items {
		match, ok := parseNPBMatch(item[1], date, i, jst)
		if !ok {
			continue
		}
		matches = append(matches, match)
	}

	return matches, nil
}

func parseNPBMatch(content, date string, index int, jst *time.Location) (BaseballMatch, bool) {
	home, away, ok := parseNPBTeams(content)
	if !ok {
		return BaseballMatch{}, false
	}
	match := BaseballMatch{
		ID:       fmt.Sprintf("npb-%s-%d", date, index),
		League:   "npb",
		HomeTeam: home,
		AwayTeam: away,
	}
	applyNPBScores(content, &match)
	applyNPBStatus(npbLinkText(content), &match)
	applyNPBStartTime(content, date, jst, &match)
	return match, true
}

func parseNPBTeams(content string) (string, string, bool) {
	homeMatch := npbHomeTeamRe.FindStringSubmatch(content)
	awayMatch := npbAwayTeamRe.FindStringSubmatch(content)
	if homeMatch == nil || awayMatch == nil {
		return "", "", false
	}
	return strings.TrimSpace(homeMatch[1]), strings.TrimSpace(awayMatch[1]), true
}

func applyNPBScores(content string, match *BaseballMatch) {
	if m := npbScoreLeftRe.FindStringSubmatch(content); m != nil {
		match.HomeScore, _ = strconv.Atoi(m[1])
	}
	if m := npbScoreRightRe.FindStringSubmatch(content); m != nil {
		match.AwayScore, _ = strconv.Atoi(m[1])
	}
}

func npbLinkText(content string) string {
	linkMatch := npbLinkTextRe.FindStringSubmatch(content)
	if linkMatch == nil {
		return ""
	}
	return strings.TrimSpace(linkMatch[1])
}

func applyNPBStatus(linkText string, match *BaseballMatch) {
	switch {
	case strings.Contains(linkText, "試合終了"):
		match.Status = BaseballFinished
	case strings.Contains(linkText, "中止"):
		match.Status = BaseballCancelled
	case npbInningRe.MatchString(linkText):
		match.Status = BaseballLive
		applyNPBInning(linkText, match)
	default:
		match.Status = BaseballScheduled
	}
}

func applyNPBInning(linkText string, match *BaseballMatch) {
	m := npbInningRe.FindStringSubmatch(linkText)
	if len(m) != 3 {
		return
	}
	match.Inning, _ = strconv.Atoi(m[1])
	if m[2] == "表" {
		match.Half = InningTop
		return
	}
	match.Half = InningBottom
}

func applyNPBStartTime(content, date string, jst *time.Location, match *BaseballMatch) {
	if match.Status != BaseballScheduled {
		return
	}
	m := npbTimeRe.FindStringSubmatch(content)
	if m == nil {
		return
	}
	hour, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	day, err := time.ParseInLocation("2006-01-02", date, jst)
	if err != nil {
		return
	}
	match.StartTime = day.Add(time.Duration(hour)*time.Hour + time.Duration(min)*time.Minute)
}
