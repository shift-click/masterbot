package providers

import (
	"context"
	"testing"
	"time"
)

// TestNaverIndexAPILive performs a live integration check against the Naver index APIs.
// It validates that FetchDomesticIndex and FetchWorldIndex return non-empty prices.
func TestNaverIndexAPILive(t *testing.T) {
	naver := NewNaverStock(nil)

	t.Run("domestic/KOSPI", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		q, err := naver.FetchDomesticIndex(ctx, "KOSPI")
		if err != nil {
			t.Fatalf("FetchDomesticIndex(KOSPI): %v", err)
		}
		if q.Price == "" {
			t.Fatal("FetchDomesticIndex(KOSPI): empty price")
		}
		// Domestic index API may not return indexName — caller supplies display name.
		t.Logf("KOSPI: name=%q price=%q change=%q pct=%q dir=%q status=%q",
			q.Name, q.Price, q.Change, q.ChangePercent, q.ChangeDirection, q.MarketStatus)
	})

	t.Run("domestic/KOSDAQ", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		q, err := naver.FetchDomesticIndex(ctx, "KOSDAQ")
		if err != nil {
			t.Fatalf("FetchDomesticIndex(KOSDAQ): %v", err)
		}
		if q.Price == "" {
			t.Fatal("FetchDomesticIndex(KOSDAQ): empty price")
		}
		t.Logf("KOSDAQ: name=%q price=%q change=%q pct=%q dir=%q",
			q.Name, q.Price, q.Change, q.ChangePercent, q.ChangeDirection)
	})

	t.Run("world/NASDAQ", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		q, err := naver.FetchWorldIndex(ctx, ".IXIC")
		if err != nil {
			t.Fatalf("FetchWorldIndex(.IXIC): %v", err)
		}
		if q.Price == "" {
			t.Fatal("FetchWorldIndex(.IXIC): empty price")
		}
		if q.Name == "" {
			t.Error("FetchWorldIndex(.IXIC): empty name (world index should return indexName)")
		}
		t.Logf("NASDAQ: name=%q price=%q change=%q pct=%q dir=%q",
			q.Name, q.Price, q.Change, q.ChangePercent, q.ChangeDirection)
	})

	t.Run("world/DowJones", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		q, err := naver.FetchWorldIndex(ctx, ".DJI")
		if err != nil {
			t.Fatalf("FetchWorldIndex(.DJI): %v", err)
		}
		if q.Price == "" {
			t.Fatal("FetchWorldIndex(.DJI): empty price")
		}
		t.Logf("DowJones: name=%q price=%q change=%q pct=%q",
			q.Name, q.Price, q.Change, q.ChangePercent)
	})

	t.Run("world/SP500", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		q, err := naver.FetchWorldIndex(ctx, ".INX")
		if err != nil {
			t.Fatalf("FetchWorldIndex(.INX): %v", err)
		}
		if q.Price == "" {
			t.Fatal("FetchWorldIndex(.INX): empty price")
		}
		t.Logf("S&P500: name=%q price=%q change=%q pct=%q",
			q.Name, q.Price, q.Change, q.ChangePercent)
	})
}
