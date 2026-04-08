package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/genai"
)

func TestOpenMeteoFetchPaths(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/v1/air-quality"):
			fmt.Fprint(w, `{"hourly":{"time":["2026-03-19T00:00","2026-03-19T01:00"],"pm10":[20,21],"pm2_5":[10,11],"ozone":[60,61]}}`)
		case strings.Contains(r.URL.Path, "/v1/forecast") && strings.Contains(r.URL.RawQuery, "start_date="):
			temps := make([]float64, 24)
			for i := range temps {
				temps[i] = float64(i)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"hourly": map[string]any{
					"time":           make([]string, 24),
					"temperature_2m": temps,
				},
			})
		case strings.Contains(r.URL.Path, "/v1/forecast") && strings.Contains(r.URL.RawQuery, "latitude=37.5665,35.1796"):
			fmt.Fprint(w, `[{"current":{"temperature_2m":1,"apparent_temperature":0,"relative_humidity_2m":55,"weather_code":1},"daily":{"temperature_2m_max":[10],"temperature_2m_min":[2],"weather_code":[1],"precipitation_probability_max":[10]},"hourly":{"time":[],"precipitation_probability":[]}}, {"current":{"temperature_2m":3,"apparent_temperature":2,"relative_humidity_2m":65,"weather_code":3},"daily":{"temperature_2m_max":[12],"temperature_2m_min":[4],"weather_code":[3],"precipitation_probability_max":[30]},"hourly":{"time":[],"precipitation_probability":[]}}]`)
		case strings.Contains(r.URL.Path, "/v1/forecast"):
			fmt.Fprint(w, `{"current":{"temperature_2m":3.5,"apparent_temperature":1.2,"relative_humidity_2m":52,"weather_code":0},"daily":{"temperature_2m_max":[11],"temperature_2m_min":[1],"weather_code":[0],"precipitation_probability_max":[20]},"hourly":{"time":[],"precipitation_probability":[0,0,0,0,0,0,5,10,15,10,5,0,20,25,20,15,10,5,0,0,0,0,0,0]}}`)
		case strings.Contains(r.URL.Path, "/fail"):
			w.WriteHeader(http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	om := NewOpenMeteo(nil)
	om.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	om.client.Unwrap().Transport = rewriteTransport{base: srv.URL}

	if _, err := om.FetchBatchForecast(context.Background(), []float64{37.5665}, nil); err == nil {
		t.Fatal("expected invalid coordinates error")
	}

	forecast, err := om.FetchForecast(context.Background(), 37.5665, 126.9780)
	if err != nil {
		t.Fatalf("FetchForecast: %v", err)
	}
	if forecast.CurrentTemp != 3.5 {
		t.Fatalf("unexpected forecast temp: %+v", forecast)
	}

	batch, err := om.FetchBatchForecast(
		context.Background(),
		[]float64{37.5665, 35.1796},
		[]float64{126.9780, 129.0756},
	)
	if err != nil {
		t.Fatalf("FetchBatchForecast: %v", err)
	}
	if len(batch) != 2 {
		t.Fatalf("batch len = %d", len(batch))
	}

	aq, err := om.FetchAirQuality(context.Background(), 37.5665, 126.9780)
	if err != nil {
		t.Fatalf("FetchAirQuality: %v", err)
	}
	if aq.PM10 == 0 || aq.PM25 == 0 || aq.Ozone == 0 {
		t.Fatalf("unexpected air quality: %+v", aq)
	}

	yesterdayTemp, err := om.FetchYesterdayTemperature(context.Background(), 37.5665, 126.9780)
	if err != nil {
		t.Fatalf("FetchYesterdayTemperature: %v", err)
	}
	if yesterdayTemp < 0 || yesterdayTemp > 23 {
		t.Fatalf("unexpected yesterdayTemp: %v", yesterdayTemp)
	}

	if _, err := om.doGet(context.Background(), srv.URL+"/fail"); err == nil {
		t.Fatal("expected doGet status error")
	}
}

func TestDunamuForexFetchFallbackAndPolling(t *testing.T) {
	var failDunamu atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/v1/forex/recent"):
			if failDunamu.Load() {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			fmt.Fprint(w, `[{"code":"FRX.KRWUSD","currencyCode":"USD","country":"미국","basePrice":1500.1,"currencyUnit":1,"signedChangePrice":1.2,"signedChangeRate":0.001},{"code":"FRX.KRWJPY","currencyCode":"JPY","country":"일본","basePrice":950.0,"currencyUnit":100,"signedChangePrice":0.8,"signedChangeRate":0.001}]`)
		case strings.Contains(r.URL.Path, "/v6/latest/USD"):
			fmt.Fprint(w, `{"rates":{"KRW":1500.0,"USD":1,"JPY":155.0,"CNY":7.2,"EUR":0.9,"THB":33.0,"TWD":31.0,"HKD":7.8,"VND":26000.0}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	d := NewDunamuForex(nil)
	d.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	d.client.Unwrap().Transport = rewriteTransport{base: srv.URL}

	rates, err := d.fetchDunamuMulti(context.Background())
	if err != nil {
		t.Fatalf("fetchDunamuMulti: %v", err)
	}
	if _, ok := rates.Rates["USD"]; !ok {
		t.Fatalf("expected USD in rates: %+v", rates)
	}

	r, err := d.FetchRate(context.Background())
	if err != nil {
		t.Fatalf("FetchRate dunamu: %v", err)
	}
	if r.Rate <= 0 {
		t.Fatalf("unexpected rate: %+v", r)
	}

	failDunamu.Store(true)
	r, err = d.FetchRate(context.Background())
	if err != nil {
		t.Fatalf("FetchRate fallback: %v", err)
	}
	if r.Rate <= 0 {
		t.Fatalf("unexpected fallback rate: %+v", r)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		d.StartPolling(ctx, 10*time.Millisecond)
		close(done)
	}()
	time.Sleep(25 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("StartPolling did not stop")
	}
}

func TestNaverGoldPrimaryAndFallbackPaths(t *testing.T) {
	t.Parallel()

	var failDomestic atomic.Bool
	var failAll atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failAll.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		code := r.URL.Query().Get("reutersCode")
		switch code {
		case naverDomesticGoldCode:
			if failDomestic.Load() {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			fmt.Fprint(w, `{"result":{"time":"2026-03-19","items":[{"localTradedAt":"2026-03-19","closePrice":"100.0"}]}}`)
		case naverDomesticSilverCode:
			if failDomestic.Load() {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			fmt.Fprint(w, `{"result":{"time":"2026-03-19","items":[{"localTradedAt":"2026-03-19","closePrice":"5.0"}]}}`)
		case naverGoldCode:
			fmt.Fprint(w, `{"result":{"time":"2026-03-19","items":[{"localTradedAt":"2026-03-19","closePrice":"2000.0"}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ng := NewNaverGold(nil)
	ng.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	ng.client.Unwrap().Transport = rewriteTransport{base: srv.URL}

	gold, err := ng.Gold(context.Background())
	if err != nil {
		t.Fatalf("Gold: %v", err)
	}
	if gold.PricePerG != 100 {
		t.Fatalf("unexpected gold price: %+v", gold)
	}

	silver, err := ng.Silver(context.Background())
	if err != nil {
		t.Fatalf("Silver: %v", err)
	}
	if silver.PricePerG != 5 {
		t.Fatalf("unexpected silver price: %+v", silver)
	}

	failAll.Store(true)
	goldCached, err := ng.Gold(context.Background())
	if err != nil {
		t.Fatalf("Gold cached path: %v", err)
	}
	if goldCached.PricePerG != 100 {
		t.Fatalf("unexpected cached gold price: %+v", goldCached)
	}

	ng2 := NewNaverGold(nil)
	ng2.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	ng2.client.Unwrap().Transport = rewriteTransport{base: srv.URL}
	failAll.Store(false)
	failDomestic.Store(true)

	goldFallback, err := ng2.Gold(context.Background())
	if err != nil {
		t.Fatalf("Gold fallback path: %v", err)
	}
	if goldFallback.PricePerG <= 0 {
		t.Fatalf("unexpected fallback gold price: %+v", goldFallback)
	}

	ng3 := NewNaverGold(nil)
	ng3.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	ng3.client.Unwrap().Transport = rewriteTransport{base: srv.URL}
	ng3.gold = &GoldPrice{Metal: "gold", PricePerG: 321, PricePerDon: 321 * gramsPerDon}
	ng3.updatedAt = time.Now().Add(-time.Hour)
	failAll.Store(true)

	stale, err := ng3.Gold(context.Background())
	if err != nil {
		t.Fatalf("Gold stale fallback: %v", err)
	}
	if stale.PricePerG != 321 {
		t.Fatalf("unexpected stale gold: %+v", stale)
	}
}

func TestOddsAPIAndHelpers(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/status/500") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("x-requests-remaining", "42")
		fmt.Fprint(w, `[{"home_team":"A","away_team":"B","commence_time":"2026-03-19T12:00:00Z","bookmakers":[{"markets":[{"key":"h2h","outcomes":[{"name":"A","price":1.8},{"name":"Draw","price":3.2},{"name":"B","price":4.1}]}]}]}]`)
	}))
	defer srv.Close()

	o := NewOddsAPI("key", nil)
	o.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	o.client.Unwrap().Transport = rewriteTransport{base: srv.URL}

	odds, err := o.FetchOdds(context.Background(), "soccer_epl")
	if err != nil {
		t.Fatalf("FetchOdds: %v", err)
	}
	if len(odds) != 1 {
		t.Fatalf("odds len = %d", len(odds))
	}
	if odds[0].OddsHome != 1.8 || odds[0].OddsDraw != 3.2 || odds[0].OddsAway != 4.1 {
		t.Fatalf("unexpected odds: %+v", odds[0])
	}
	if got := o.CreditsRemaining(); got != 42 {
		t.Fatalf("remaining = %d", got)
	}

	o2 := NewOddsAPI("", nil)
	empty, err := o2.FetchOdds(context.Background(), "soccer_epl")
	if err != nil {
		t.Fatalf("FetchOdds empty key: %v", err)
	}
	if empty != nil {
		t.Fatalf("expected nil odds for empty key, got %+v", empty)
	}

	if _, remaining, err := o.doGet(context.Background(), srv.URL+"/status/500"); err == nil || remaining != -1 {
		t.Fatalf("expected doGet status error, remaining=%d err=%v", remaining, err)
	}

	if !parseOddsCommenceTime("bad").IsZero() {
		t.Fatal("invalid commence time should be zero")
	}
	if got := findH2HMarket(nil); got != nil {
		t.Fatalf("expected nil h2h market, got %+v", got)
	}
}

func TestJudalScraperPaths(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		view := r.URL.Query().Get("view")
		themeIdx := r.URL.Query().Get("themeIdx")
		switch {
		case view == "themeList":
			fmt.Fprint(w, `<a href="/?view=stockList&themeIdx=12" title="AI 테마토크"></a><a href="/?view=stockList&themeIdx=12" title="AI 테마토크"></a><a href="/?view=stockList&themeIdx=13" title="반도체 테마토크"></a>`)
		case view == "stockList" && themeIdx == "12":
			fmt.Fprint(w, `code=005930 code=000660 code=005930`)
		case view == "stockList" && themeIdx == "500":
			w.WriteHeader(http.StatusInternalServerError)
		case view == "stockList":
			fmt.Fprint(w, `no-codes`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	j := NewJudalScraper(nil)
	j.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	j.client.Unwrap().Transport = rewriteTransport{base: srv.URL}

	themes, err := j.FetchThemeList(context.Background())
	if err != nil {
		t.Fatalf("FetchThemeList: %v", err)
	}
	if len(themes) != 2 {
		t.Fatalf("themes len = %d", len(themes))
	}

	codes, err := j.FetchStockCodes(context.Background(), 12)
	if err != nil {
		t.Fatalf("FetchStockCodes: %v", err)
	}
	if len(codes) != 2 {
		t.Fatalf("codes len = %d", len(codes))
	}

	if _, err := j.FetchStockCodes(context.Background(), 999); err == nil {
		t.Fatal("expected no stock codes error")
	}
	if _, err := j.FetchStockCodes(context.Background(), 500); err == nil {
		t.Fatal("expected HTTP status error")
	}
}

func TestNaverEsportsAndLoLEsportsFetches(t *testing.T) {
	t.Parallel()

	nextData := `{"props":{"pageProps":{"scheduleData":[{"gameId":"g1","startDate":1742366400000,"matchStatus":"RESULT","homeTeam":{"name":"T1","nameAcronym":"T1"},"awayTeam":{"name":"GEN","nameAcronym":"GEN"},"homeScore":2,"awayScore":1,"maxMatchCount":5},{"gameId":"g2","startDate":1742452800000,"matchStatus":"LIVE","homeTeam":{"name":"DK","nameAcronym":"DK"},"awayTeam":{"name":"HLE","nameAcronym":"HLE"},"homeScore":1,"awayScore":1,"maxMatchCount":0}]}}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/esports/League_of_Legends/schedule"):
			ts := r.URL.Query().Get("timestamp")
			switch ts {
			case "500":
				w.WriteHeader(http.StatusInternalServerError)
			case "broken":
				fmt.Fprint(w, `<html><body>no next data</body></html>`)
			case "empty":
				fmt.Fprint(w, `<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"scheduleData":[]}}}</script>`)
			default:
				fmt.Fprintf(w, `<script id="__NEXT_DATA__" type="application/json">%s</script>`, nextData)
			}
		case strings.Contains(r.URL.Path, "/persisted/gw/getSchedule"):
			fmt.Fprint(w, `{"data":{"schedule":{"events":[{"startTime":"2026-03-19T12:00:00Z","state":"completed","blockName":"1주차","match":{"teams":[{"name":"T1","code":"T1","result":{"gameWins":2}},{"name":"GEN","code":"GEN","result":{"gameWins":1}}],"strategy":{"type":"bestOf","count":3}}}]}}}`)
		case strings.Contains(r.URL.Path, "/persisted/gw/getLive"):
			fmt.Fprint(w, `{"data":{"schedule":{"events":[{"startTime":"2026-03-19T13:00:00Z","state":"inProgress","blockName":"1주차","match":{"teams":[{"name":"DK","code":"DK","result":{"gameWins":1}},{"name":"HLE","code":"HLE","result":{"gameWins":1}}],"strategy":{"type":"bestOf","count":5}}}]}}}`)
		case strings.Contains(r.URL.Path, "/persisted/gw/getStandings"):
			fmt.Fprint(w, `{"data":{"standings":[{"stages":[{"sections":[{"rankings":[{"ordinal":1,"teams":[{"name":"T1","code":"T1","record":{"wins":10,"losses":2}}]}]}]}]}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	n := NewNaverEsports(nil)
	n.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	n.client.Unwrap().Transport = rewriteTransport{base: srv.URL}

	matches, err := n.FetchSchedule(context.Background(), "2026-03")
	if err != nil {
		t.Fatalf("Naver FetchSchedule: %v", err)
	}
	if len(matches) != 2 || matches[0].Status != MatchFinished || matches[1].Status != MatchLive {
		t.Fatalf("unexpected naver matches: %+v", matches)
	}
	if matches[1].BestOf != 3 {
		t.Fatalf("expected default bestOf=3, got %d", matches[1].BestOf)
	}

	empty, err := n.FetchSchedule(context.Background(), "empty")
	if err != nil {
		t.Fatalf("Naver empty schedule: %v", err)
	}
	if empty != nil {
		t.Fatalf("expected nil empty schedule, got %+v", empty)
	}
	if _, err := n.FetchSchedule(context.Background(), "broken"); err == nil {
		t.Fatal("expected broken next data error")
	}
	if _, err := n.FetchSchedule(context.Background(), "500"); err == nil {
		t.Fatal("expected naver esports status error")
	}

	l := NewLoLEsports(nil)
	l.client = NewBreakerHTTPClient(srv.Client(), "test", nil)
	l.client.Unwrap().Transport = rewriteTransport{base: srv.URL}

	schedule, err := l.FetchSchedule(context.Background(), "987")
	if err != nil {
		t.Fatalf("LoL FetchSchedule: %v", err)
	}
	if len(schedule) != 1 || schedule[0].Status != MatchFinished {
		t.Fatalf("unexpected schedule: %+v", schedule)
	}

	live, err := l.FetchLive(context.Background())
	if err != nil {
		t.Fatalf("LoL FetchLive: %v", err)
	}
	if len(live) != 1 || live[0].Status != MatchLive {
		t.Fatalf("unexpected live: %+v", live)
	}

	standings, err := l.FetchStandings(context.Background(), "123")
	if err != nil {
		t.Fatalf("LoL FetchStandings: %v", err)
	}
	if len(standings) != 1 || standings[0].TeamCode != "T1" {
		t.Fatalf("unexpected standings: %+v", standings)
	}

	if _, err := l.doGet(context.Background(), srv.URL+"/missing"); err == nil {
		t.Fatal("expected doGet status error")
	}
}

func TestGeminiExtractAndErrorBranches(t *testing.T) {
	t.Parallel()

	if got := extractTextResponse(nil); got != "" {
		t.Fatalf("extractTextResponse(nil) = %q", got)
	}

	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{Content: &genai.Content{Parts: []*genai.Part{{Text: "요약"}, {Text: " 결과"}}}},
		},
	}
	if got := extractTextResponse(resp); got != "요약 결과" {
		t.Fatalf("extractTextResponse = %q", got)
	}

	g := &GeminiClient{logger: slog.Default()}
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic from nil gemini client")
		}
	}()
	_, _ = g.SummarizeYouTube(context.Background(), "https://youtu.be/test")
}

func TestURLSummaryResult_IsRetrievalSuccess(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		result  URLSummaryResult
		success bool
		failure bool
	}{
		{
			name:    "success status",
			result:  URLSummaryResult{HasRetrievalStatus: true, RetrievalStatus: genai.URLRetrievalStatusSuccess},
			success: true,
			failure: false,
		},
		{
			name:    "error status",
			result:  URLSummaryResult{HasRetrievalStatus: true, RetrievalStatus: genai.URLRetrievalStatusError},
			success: false,
			failure: true,
		},
		{
			name:    "paywall status",
			result:  URLSummaryResult{HasRetrievalStatus: true, RetrievalStatus: genai.URLRetrievalStatusPaywall},
			success: false,
			failure: true,
		},
		{
			name:    "unsafe status",
			result:  URLSummaryResult{HasRetrievalStatus: true, RetrievalStatus: genai.URLRetrievalStatusUnsafe},
			success: false,
			failure: true,
		},
		{
			name:    "no status metadata",
			result:  URLSummaryResult{HasRetrievalStatus: false},
			success: false,
			failure: true,
		},
		{
			name:    "unspecified status",
			result:  URLSummaryResult{HasRetrievalStatus: true, RetrievalStatus: genai.URLRetrievalStatusUnspecified},
			success: false,
			failure: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.result.IsRetrievalSuccess(); got != tt.success {
				t.Errorf("IsRetrievalSuccess() = %v, want %v", got, tt.success)
			}
			if got := tt.result.IsRetrievalFailure(); got != tt.failure {
				t.Errorf("IsRetrievalFailure() = %v, want %v", got, tt.failure)
			}
		})
	}
}
