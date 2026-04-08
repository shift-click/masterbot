package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNaverGoldParsing(t *testing.T) {
	responseJSON := `{
		"result": {
			"time": "2026-03-18",
			"items": [
				{
					"localTradedAt": "2026-03-18",
					"closePrice": "238893.00"
				}
			]
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, responseJSON)
	}))
	defer server.Close()

	ng := NewNaverGold(nil)
	price, err := ng.fetchPriceFromAPI(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("fetchPriceFromAPI: %v", err)
	}
	if price != 238893.00 {
		t.Errorf("price = %f, want 238893.00", price)
	}
}

func TestNaverGoldEmptyResponse(t *testing.T) {
	responseJSON := `{"result": {"time": "", "items": []}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, responseJSON)
	}))
	defer server.Close()

	ng := NewNaverGold(nil)
	_, err := ng.fetchPriceFromAPI(context.Background(), server.URL)
	if err == nil {
		t.Error("expected error for empty items")
	}
}

func TestNaverGoldCacheHit(t *testing.T) {
	ng := NewNaverGold(nil)

	// Manually populate cache.
	ng.gold = &GoldPrice{
		Metal:       "gold",
		PricePerG:   238893,
		PricePerDon: 238893 * gramsPerDon,
	}
	ng.silver = &GoldPrice{
		Metal:       "silver",
		PricePerG:   1520,
		PricePerDon: 1520 * gramsPerDon,
	}

	// With zero updatedAt, cache should miss.
	if p := ng.cachedGold(); p != nil {
		t.Error("expected nil for stale cache")
	}

	// Stale data should still be available.
	if p := ng.staleGold(); p == nil {
		t.Error("expected stale gold data")
	} else if p.PricePerG != 238893 {
		t.Errorf("stale gold price = %f, want 238893", p.PricePerG)
	}
}

func TestGoldPricePerDon(t *testing.T) {
	price := GoldPrice{
		Metal:       "gold",
		PricePerG:   238893,
		PricePerDon: 238893 * gramsPerDon,
	}

	expected := 238893.0 * 3.75
	if price.PricePerDon != expected {
		t.Errorf("PricePerDon = %f, want %f", price.PricePerDon, expected)
	}
}
