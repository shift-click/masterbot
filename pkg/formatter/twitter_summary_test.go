package formatter_test

import (
	"strings"
	"testing"

	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/pkg/formatter"
)

func TestFormatTweet(t *testing.T) {
	t.Parallel()

	t.Run("basic tweet", func(t *testing.T) {
		t.Parallel()
		data := providers.TweetData{
			Text:             "just setting up my twttr",
			AuthorName:       "jack",
			AuthorScreenName: "jack",
			Likes:            310710,
			Retweets:         126878,
			CreatedAt:        "Tue Mar 21 20:50:14 +0000 2006",
		}
		out := formatter.FormatTweet(data)

		if !strings.Contains(out, "🐦 트윗 원문") {
			t.Errorf("missing header, got:\n%s", out)
		}
		if !strings.Contains(out, "@jack") {
			t.Errorf("missing author, got:\n%s", out)
		}
		if !strings.Contains(out, "just setting up my twttr") {
			t.Errorf("missing tweet text, got:\n%s", out)
		}
		if !strings.Contains(out, "❤️") {
			t.Errorf("missing likes emoji, got:\n%s", out)
		}
		if !strings.Contains(out, "🔁") {
			t.Errorf("missing retweets emoji, got:\n%s", out)
		}
		if !strings.Contains(out, "2006.03.21") {
			t.Errorf("missing date, got:\n%s", out)
		}
	})

	t.Run("author name differs from screen name", func(t *testing.T) {
		t.Parallel()
		data := providers.TweetData{
			Text:             "Hello world",
			AuthorName:       "Elon Musk",
			AuthorScreenName: "elonmusk",
			Likes:            0,
			Retweets:         0,
		}
		out := formatter.FormatTweet(data)
		if !strings.Contains(out, "@elonmusk (Elon Musk)") {
			t.Errorf("expected '@elonmusk (Elon Musk)', got:\n%s", out)
		}
	})

	t.Run("zero engagement omitted", func(t *testing.T) {
		t.Parallel()
		data := providers.TweetData{
			Text:             "A tweet",
			AuthorScreenName: "user",
			Likes:            0,
			Retweets:         0,
		}
		out := formatter.FormatTweet(data)
		if strings.Contains(out, "❤️") {
			t.Errorf("zero likes should be omitted, got:\n%s", out)
		}
		if strings.Contains(out, "🔁") {
			t.Errorf("zero retweets should be omitted, got:\n%s", out)
		}
	})

	t.Run("large count formatted with K/M suffix", func(t *testing.T) {
		t.Parallel()
		data := providers.TweetData{
			Text:             "Popular tweet",
			AuthorScreenName: "user",
			Likes:            1_500_000,
			Retweets:         2500,
		}
		out := formatter.FormatTweet(data)
		if !strings.Contains(out, "1.5M") {
			t.Errorf("expected 1.5M for likes, got:\n%s", out)
		}
		if !strings.Contains(out, "2.5K") {
			t.Errorf("expected 2.5K for retweets, got:\n%s", out)
		}
	})
}

func TestTweetNeedsAISummary(t *testing.T) {
	t.Parallel()

	shortTweet := strings.Repeat("a", 499)
	if formatter.TweetNeedsAISummary(shortTweet) {
		t.Error("499-char tweet should not need AI summary")
	}

	longTweet := strings.Repeat("a", 500)
	if !formatter.TweetNeedsAISummary(longTweet) {
		t.Error("500-char tweet should need AI summary")
	}
}
