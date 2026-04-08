package command

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper"
	"github.com/shift-click/masterbot/internal/scraper/providers"
)

func TestWeatherHandlerExecuteNationalTodayFromCache(t *testing.T) {
	h := newWeatherHandlerWithWarmCache()

	reply := runWeather(t, h, []string{})
	if reply.Text == "" {
		t.Fatal("expected non-empty reply")
	}
	if !strings.Contains(reply.Text, "전국 날씨") {
		t.Fatalf("reply = %q", reply.Text)
	}
}

func TestWeatherHandlerExecuteNationalTomorrowFromCache(t *testing.T) {
	h := newWeatherHandlerWithWarmCache()

	reply := runWeather(t, h, []string{"내일"})
	if reply.Text == "" {
		t.Fatal("expected non-empty reply")
	}
	if !strings.Contains(reply.Text, "내일") {
		t.Fatalf("reply = %q", reply.Text)
	}
}

func TestWeatherHandlerExecuteRegionalForecastFromCache(t *testing.T) {
	h := newWeatherHandlerWithWarmCache()

	reply := runWeather(t, h, []string{"서울"})
	if reply.Text == "" {
		t.Fatal("expected non-empty reply")
	}
	if !strings.Contains(reply.Text, "서울") {
		t.Fatalf("reply = %q", reply.Text)
	}
}

func TestWeatherHandlerExecuteRegionalAirQualityFromCache(t *testing.T) {
	h := newWeatherHandlerWithWarmCache()

	reply := runWeather(t, h, []string{"서울", "미세먼지"})
	if reply.Text == "" {
		t.Fatal("expected non-empty reply")
	}
	if !strings.Contains(reply.Text, "서울") {
		t.Fatalf("reply = %q", reply.Text)
	}
}

func TestWeatherHandlerExecuteUnknownRegion(t *testing.T) {
	h := newWeatherHandlerWithWarmCache()

	reply := runWeather(t, h, []string{"없는지역"})
	if !strings.Contains(reply.Text, unknownRegionText) {
		t.Fatalf("reply = %q", reply.Text)
	}
}

func TestParseWeatherQuery(t *testing.T) {
	tests := []struct {
		input      string
		wantMode   weatherMode
		wantRegion string
	}{
		{input: "", wantMode: weatherNationalToday},
		{input: "날씨", wantMode: weatherNationalToday},
		{input: "내일", wantMode: weatherNationalTomorrow},
		{input: "내일 날씨", wantMode: weatherNationalTomorrow},
		{input: "서울", wantMode: weatherRegionalForecast, wantRegion: "서울"},
		{input: "서울 날씨", wantMode: weatherRegionalForecast, wantRegion: "서울"},
		{input: "서울 미세먼지", wantMode: weatherRegionalAirQuality, wantRegion: "서울"},
	}

	for _, tc := range tests {
		mode, region := parseWeatherQuery(tc.input)
		if mode != tc.wantMode || region != tc.wantRegion {
			t.Fatalf("parseWeatherQuery(%q) = (%v,%q), want (%v,%q)", tc.input, mode, region, tc.wantMode, tc.wantRegion)
		}
	}
}

func newWeatherHandlerWithWarmCache() *WeatherHandler {
	cache := scraper.NewWeatherCache(time.Hour, time.Hour)
	forecast := &providers.ForecastData{
		CurrentTemp:      22.5,
		ApparentTemp:     23.0,
		Humidity:         48,
		WeatherCode:      1,
		DailyMaxTemp:     []float64{25.0, 27.0},
		DailyMinTemp:     []float64{16.0, 18.0},
		DailyWeatherCode: []int{1, 3},
		DailyPrecipProb:  []int{10, 30},
		HourlyPrecipProb: make([]int, 24),
	}
	air := &providers.AirQualityData{PM10: 20, PM25: 10, Ozone: 60}

	for _, city := range NationalCities() {
		cache.SetForecast(city.Lat, city.Lon, forecast)
		cache.SetAirQuality(city.Lat, city.Lon, air)
		cache.SetYesterday(city.Lat, city.Lon, 21.0)
	}

	return NewWeatherCommand(nil, cache, slog.Default())
}

func runWeather(t *testing.T, h *WeatherHandler, args []string) bot.Reply {
	t.Helper()

	var reply bot.Reply
	err := h.Execute(context.Background(), bot.CommandContext{
		Command: "날씨",
		Args:    args,
		Reply: func(_ context.Context, r bot.Reply) error {
			reply = r
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	return reply
}
