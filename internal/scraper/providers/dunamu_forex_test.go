package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchDunamuMulti(t *testing.T) {
	// Simulate Dunamu CDN response with 3 currencies.
	dunamuResp := []map[string]any{
		{
			"code": "FRX.KRWUSD", "currencyCode": "USD", "country": "미국",
			"basePrice": 1487.10, "currencyUnit": 1,
			"signedChangePrice": -2.30, "signedChangeRate": -0.0015,
		},
		{
			"code": "FRX.KRWJPY", "currencyCode": "JPY", "country": "일본",
			"basePrice": 935.11, "currencyUnit": 100,
			"signedChangePrice": 1.05, "signedChangeRate": 0.0011,
		},
		{
			"code": "FRX.KRWVND", "currencyCode": "VND", "country": "베트남",
			"basePrice": 5.67, "currencyUnit": 100,
			"signedChangePrice": 0.0, "signedChangeRate": 0.0,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(dunamuResp)
	}))
	defer srv.Close()

	d := NewDunamuForex(nil)

	// Override the fetch to use test server.
	body, err := d.doGet(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("doGet failed: %v", err)
	}

	var items []struct {
		Code              string  `json:"code"`
		CurrencyCode      string  `json:"currencyCode"`
		Country           string  `json:"country"`
		BasePrice         float64 `json:"basePrice"`
		CurrencyUnit      int     `json:"currencyUnit"`
		SignedChangePrice  float64 `json:"signedChangePrice"`
		SignedChangeRate   float64 `json:"signedChangeRate"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	// Verify USD.
	if items[0].CurrencyCode != "USD" || items[0].BasePrice != 1487.10 {
		t.Errorf("USD: got %+v", items[0])
	}
	if items[0].CurrencyUnit != 1 {
		t.Errorf("USD currencyUnit: got %d, want 1", items[0].CurrencyUnit)
	}

	// Verify JPY (100 unit).
	if items[1].CurrencyCode != "JPY" || items[1].CurrencyUnit != 100 {
		t.Errorf("JPY: got %+v", items[1])
	}

	// Verify VND (100 unit).
	if items[2].CurrencyCode != "VND" || items[2].BasePrice != 5.67 {
		t.Errorf("VND: got %+v", items[2])
	}
}

func TestFetchOpenERMulti(t *testing.T) {
	openERResp := map[string]any{
		"result": "success",
		"rates": map[string]float64{
			"KRW": 1487.10,
			"JPY": 159.17,
			"CNY": 6.90,
			"EUR": 0.87,
			"THB": 32.38,
			"TWD": 31.99,
			"HKD": 7.83,
			"VND": 26199.56,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(openERResp)
	}))
	defer srv.Close()

	d := NewDunamuForex(nil)

	body, err := d.doGet(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("doGet failed: %v", err)
	}

	var resp struct {
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	krw := resp.Rates["KRW"]
	jpy := resp.Rates["JPY"]

	// KRW per 100 JPY = (KRW/JPY) * 100
	krwPerJPY100 := (krw / jpy) * 100
	if krwPerJPY100 < 900 || krwPerJPY100 > 1000 {
		t.Errorf("KRW per 100 JPY = %.2f, expected ~934", krwPerJPY100)
	}

	// KRW per 100 VND should be small.
	vnd := resp.Rates["VND"]
	krwPerVND100 := (krw / vnd) * 100
	if krwPerVND100 < 3 || krwPerVND100 > 10 {
		t.Errorf("KRW per 100 VND = %.2f, expected ~5.68", krwPerVND100)
	}
}

func TestForexDisplayOrder(t *testing.T) {
	order := ForexDisplayOrder()
	if len(order) != 8 {
		t.Fatalf("expected 8 currencies, got %d", len(order))
	}
	if order[0] != "USD" {
		t.Errorf("first currency should be USD, got %s", order[0])
	}
	if order[7] != "VND" {
		t.Errorf("last currency should be VND, got %s", order[7])
	}
}

func TestRateBackwardCompat(t *testing.T) {
	d := NewDunamuForex(nil)

	// Simulate storing multi rates.
	d.mu.Lock()
	d.rates = MultiForexRates{
		Rates: map[string]CurrencyRate{
			"USD": {BasePrice: 1487.10, CurrencyUnit: 1},
			"JPY": {BasePrice: 935.11, CurrencyUnit: 100},
		},
	}
	d.rate = ForexRate{Rate: 1487.10}
	d.mu.Unlock()

	// Rate() should return USD rate.
	r := d.Rate()
	if r.Rate != 1487.10 {
		t.Errorf("Rate() = %.2f, want 1487.10", r.Rate)
	}

	// Rates() should return both.
	rates := d.Rates()
	if len(rates.Rates) != 2 {
		t.Errorf("Rates() has %d entries, want 2", len(rates.Rates))
	}
}
