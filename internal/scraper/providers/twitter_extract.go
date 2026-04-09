package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"
)

const (
	fxtwitterBaseURL   = "https://api.fxtwitter.com/status/"
	twitterFetchTimeout = 15 * time.Second
)

var reTweetID = regexp.MustCompile(`/status/(\d+)`)

// TweetData holds the essential fields from a fxtwitter API response.
type TweetData struct {
	ID               string
	Text             string
	AuthorName       string
	AuthorScreenName string
	Likes            int64
	Retweets         int64
	CreatedAt        string
	Lang             string
}

type fxtwitterResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Tweet   struct {
		ID        string `json:"id"`
		Text      string `json:"text"`
		Author    struct {
			Name       string `json:"name"`
			ScreenName string `json:"screen_name"`
		} `json:"author"`
		Likes     int64  `json:"likes"`
		Retweets  int64  `json:"retweets"`
		CreatedAt string `json:"created_at"`
		Lang      string `json:"lang"`
	} `json:"tweet"`
}

// ExtractTweetID extracts the numeric tweet ID from an x.com or twitter.com URL.
// Returns an empty string if no /status/{id} segment is found.
func ExtractTweetID(rawURL string) string {
	m := reTweetID.FindStringSubmatch(rawURL)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// FetchTweet retrieves tweet data from the fxtwitter API for the given URL.
// Returns an error if the URL has no tweet ID, or if the API call fails.
func FetchTweet(ctx context.Context, rawURL string) (TweetData, error) {
	return FetchTweetFromBase(ctx, rawURL, fxtwitterBaseURL)
}

// FetchTweetFromBase is like FetchTweet but uses a custom base URL for the API.
// This allows tests to substitute a local HTTP server.
func FetchTweetFromBase(ctx context.Context, rawURL, baseURL string) (TweetData, error) {
	tweetID := ExtractTweetID(rawURL)
	if tweetID == "" {
		return TweetData{}, fmt.Errorf("twitter extract: no tweet ID in URL: %s", rawURL)
	}

	ctx, cancel := context.WithTimeout(ctx, twitterFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+tweetID, nil)
	if err != nil {
		return TweetData{}, fmt.Errorf("twitter extract: create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; JucoBot/2.0)")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return TweetData{}, fmt.Errorf("twitter extract: fetch: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return TweetData{}, fmt.Errorf("twitter extract: read body: %w", err)
	}

	var data fxtwitterResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return TweetData{}, fmt.Errorf("twitter extract: parse JSON: %w", err)
	}

	if data.Code != 200 {
		return TweetData{}, fmt.Errorf("twitter extract: API error %d: %s", data.Code, data.Message)
	}

	return TweetData{
		ID:               data.Tweet.ID,
		Text:             data.Tweet.Text,
		AuthorName:       data.Tweet.Author.Name,
		AuthorScreenName: data.Tweet.Author.ScreenName,
		Likes:            data.Tweet.Likes,
		Retweets:         data.Tweet.Retweets,
		CreatedAt:        data.Tweet.CreatedAt,
		Lang:             data.Tweet.Lang,
	}, nil
}
