package scraper

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
)

func TestPollAll_UsesWorldFetcherForWorldStocks(t *testing.T) {
	t.Parallel()

	var koreanCalls, worldCalls atomic.Int32

	koreanFetcher := func(_ context.Context, code string) (json.RawMessage, error) {
		koreanCalls.Add(1)
		return json.RawMessage(`{"code":"` + code + `"}`), nil
	}
	worldFetcher := func(_ context.Context, code string) (json.RawMessage, error) {
		worldCalls.Add(1)
		return json.RawMessage(`{"code":"` + code + `"}`), nil
	}

	hl := NewHotList(koreanFetcher, DefaultHotListConfig(), nil)
	hl.SetWorldFetcher(worldFetcher)

	// Register a Korean stock and a world stock.
	hl.Register("005930", json.RawMessage(`{}`))
	hl.RegisterWithMeta("KO", json.RawMessage(`{}`), true, "")

	hl.pollAll(context.Background())

	if koreanCalls.Load() != 1 {
		t.Errorf("expected 1 Korean fetch call, got %d", koreanCalls.Load())
	}
	if worldCalls.Load() != 1 {
		t.Errorf("expected 1 world fetch call, got %d", worldCalls.Load())
	}
}

func TestPollAll_FallsBackToDefaultFetcherWithoutWorldFetcher(t *testing.T) {
	t.Parallel()

	var defaultCalls atomic.Int32

	defaultFetcher := func(_ context.Context, code string) (json.RawMessage, error) {
		defaultCalls.Add(1)
		return json.RawMessage(`{"code":"` + code + `"}`), nil
	}

	hl := NewHotList(defaultFetcher, DefaultHotListConfig(), nil)
	// No world fetcher set.

	hl.RegisterWithMeta("KO", json.RawMessage(`{}`), true, "")

	hl.pollAll(context.Background())

	if defaultCalls.Load() != 1 {
		t.Errorf("expected 1 default fetch call when no world fetcher, got %d", defaultCalls.Load())
	}
}
