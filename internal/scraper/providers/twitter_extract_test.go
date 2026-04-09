package providers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shift-click/masterbot/internal/scraper/providers"
)

func TestExtractTweetID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "x.com tweet URL",
			url:      "https://x.com/jack/status/20",
			expected: "20",
		},
		{
			name:     "twitter.com tweet URL",
			url:      "https://twitter.com/Interior/status/507185938620886016",
			expected: "507185938620886016",
		},
		{
			name:     "x.com with query string",
			url:      "https://x.com/user/status/12345?s=20&t=abc",
			expected: "12345",
		},
		{
			name:     "profile URL without status",
			url:      "https://x.com/jack",
			expected: "",
		},
		{
			name:     "empty URL",
			url:      "",
			expected: "",
		},
		{
			name:     "unrelated URL",
			url:      "https://example.com/article/123",
			expected: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := providers.ExtractTweetID(tc.url)
			if got != tc.expected {
				t.Errorf("ExtractTweetID(%q) = %q, want %q", tc.url, got, tc.expected)
			}
		})
	}
}

func TestFetchTweet(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/20") {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			resp := map[string]any{
				"code":    200,
				"message": "OK",
				"tweet": map[string]any{
					"id":         "20",
					"text":       "just setting up my twttr",
					"created_at": "Tue Mar 21 20:50:14 +0000 2006",
					"lang":       "en",
					"likes":      310710,
					"retweets":   126878,
					"author": map[string]any{
						"name":        "jack",
						"screen_name": "jack",
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer srv.Close()

		// Patch base URL for test — replace the package-level constant via indirect test.
		// We use a wrapper that points to the test server.
		data, err := providers.FetchTweetFromBase(context.Background(), "https://x.com/jack/status/20", srv.URL+"/status/")
		if err != nil {
			t.Fatalf("FetchTweetFromBase: %v", err)
		}
		if data.Text != "just setting up my twttr" {
			t.Errorf("Text = %q, want %q", data.Text, "just setting up my twttr")
		}
		if data.AuthorScreenName != "jack" {
			t.Errorf("AuthorScreenName = %q, want %q", data.AuthorScreenName, "jack")
		}
		if data.Likes != 310710 {
			t.Errorf("Likes = %d, want %d", data.Likes, 310710)
		}
	})

	t.Run("api error code", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]any{
				"code":    404,
				"message": "Tweet not found",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer srv.Close()

		_, err := providers.FetchTweetFromBase(context.Background(), "https://x.com/user/status/9999", srv.URL+"/status/")
		if err == nil {
			t.Fatal("expected error for non-200 API code, got nil")
		}
	})

	t.Run("no tweet ID in URL", func(t *testing.T) {
		t.Parallel()
		_, err := providers.FetchTweet(context.Background(), "https://x.com/jack")
		if err == nil {
			t.Fatal("expected error for URL without tweet ID, got nil")
		}
	})

	t.Run("invalid JSON response", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("not json"))
		}))
		defer srv.Close()

		_, err := providers.FetchTweetFromBase(context.Background(), "https://x.com/user/status/123", srv.URL+"/status/")
		if err == nil {
			t.Fatal("expected error for invalid JSON, got nil")
		}
	})
}
