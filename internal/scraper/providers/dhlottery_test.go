package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDHLotteryFetchLatestDraw(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"list": [{
					"ltEpsd": 1218,
					"tm1WnNo": 3,
					"tm2WnNo": 28,
					"tm3WnNo": 31,
					"tm4WnNo": 32,
					"tm5WnNo": 42,
					"tm6WnNo": 45,
					"bnsWnNo": 25,
					"ltRflYmd": "20260404",
					"rnk1WnNope": 18,
					"rnk1WnAmt": 1714482042,
					"rnk2WnNope": 80,
					"rnk2WnAmt": 64293077,
					"rnk3WnNope": 2889,
					"rnk3WnAmt": 1780356,
					"rnk4WnNope": 150326,
					"rnk4WnAmt": 50000,
					"rnk5WnNope": 2600819,
					"rnk5WnAmt": 5000,
					"sumWnNope": 2754132,
					"rlvtEpsdSumNtslAmt": 61667966400
				}]
			}
		}`))
	}))
	defer server.Close()

	client := NewDHLottery(nil)
	client.latestDrawURL = server.URL + "/latest"

	draw, err := client.FetchLatestDraw(context.Background())
	if err != nil {
		t.Fatalf("FetchLatestDraw() error = %v", err)
	}
	if draw.Round != 1218 {
		t.Fatalf("round = %d, want 1218", draw.Round)
	}
	if draw.Numbers != [6]int{3, 28, 31, 32, 42, 45} {
		t.Fatalf("numbers = %v", draw.Numbers)
	}
	if draw.BonusNumber != 25 {
		t.Fatalf("bonus = %d, want 25", draw.BonusNumber)
	}
	if draw.Rank1Prize != 1714482042 {
		t.Fatalf("rank1 prize = %d, want 1714482042", draw.Rank1Prize)
	}
}

func TestDHLotteryFetchFirstPrizeShops(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/shops" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("srchLtEpsd"); got != "1218" {
			t.Fatalf("round query = %q, want 1218", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"list": [{
					"ltShpId": "11140694",
					"shpNm": "꿈이있는 로또점(복권판매점)",
					"region": "서울",
					"tm1ShpLctnAddr": "서울",
					"tm2ShpLctnAddr": "은평구",
					"tm3ShpLctnAddr": "통일로 699 1층",
					"tm4ShpLctnAddr": null,
					"shpAddr": "서울 은평구 통일로 699 1층",
					"atmtPsvYn": "Q",
					"atmtPsvYnTxt": "자동",
					"shpLat": 37.608381,
					"shpLot": 126.931468
				}]
			}
		}`))
	}))
	defer server.Close()

	client := NewDHLottery(nil)
	client.shopBaseURL = server.URL + "/shops"

	shops, err := client.FetchFirstPrizeShops(context.Background(), 1218)
	if err != nil {
		t.Fatalf("FetchFirstPrizeShops() error = %v", err)
	}
	if len(shops) != 1 {
		t.Fatalf("len(shops) = %d, want 1", len(shops))
	}
	if shops[0].Region != "서울" || shops[0].District != "은평구" {
		t.Fatalf("shop region = %s %s", shops[0].Region, shops[0].District)
	}
	if shops[0].WinMethodText != "자동" {
		t.Fatalf("win method = %q, want 자동", shops[0].WinMethodText)
	}
}
