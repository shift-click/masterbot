package command

import (
	"context"
	"log/slog"
	"strings"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/pkg/formatter"
)

type weatherMode int

const (
	weatherNationalToday weatherMode = iota
	weatherNationalTomorrow
	weatherRegionalForecast
	weatherRegionalAirQuality

	weatherUnavailableText    = "날씨 정보를 가져올 수 없습니다."
	unknownRegionText         = "알 수 없는 지역입니다."
	airQualityUnavailableText = "대기질 정보를 가져올 수 없습니다."
)

// WeatherHandler handles weather and air quality queries.
type WeatherHandler struct {
	descriptorSupport
	provider *providers.OpenMeteo
	cache    *scraper.WeatherCache
	logger   *slog.Logger
}

func NewWeatherCommand(provider *providers.OpenMeteo, cache *scraper.WeatherCache, logger *slog.Logger) *WeatherHandler {
	return &WeatherHandler{
		descriptorSupport: newDescriptorSupport("weather"),
		provider:          provider,
		cache:             cache,
		logger:            logger,
	}
}

func (h *WeatherHandler) Execute(ctx context.Context, cmd bot.CommandContext) error {
	input := strings.TrimSpace(strings.Join(cmd.Args, " "))
	mode, regionName := parseWeatherQuery(input)

	switch mode {
	case weatherNationalToday:
		return h.handleNationalToday(ctx, cmd)
	case weatherNationalTomorrow:
		return h.handleNationalTomorrow(ctx, cmd)
	case weatherRegionalForecast:
		return h.handleRegionalForecast(ctx, cmd, regionName)
	case weatherRegionalAirQuality:
		return h.handleRegionalAirQuality(ctx, cmd, regionName)
	default:
		return h.handleNationalToday(ctx, cmd)
	}
}

func (h *WeatherHandler) MatchBareQuery(_ context.Context, content string) ([]string, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, false
	}

	mode, regionName := parseWeatherQuery(content)
	switch mode {
	case weatherNationalToday:
		return nil, true
	case weatherNationalTomorrow:
		return []string{"내일"}, true
	case weatherRegionalForecast:
		// Only match bare queries for known locations to avoid
		// intercepting unrelated inputs like stock names.
		if _, ok := LookupLocation(regionName); !ok {
			return nil, false
		}
		return []string{regionName}, true
	case weatherRegionalAirQuality:
		if _, ok := LookupLocation(regionName); !ok {
			return nil, false
		}
		return []string{regionName, "미세먼지"}, true
	}
	return nil, false
}

func parseWeatherQuery(input string) (weatherMode, string) {
	input = strings.TrimSpace(input)

	// "[지역] 미세먼지" pattern
	if strings.HasSuffix(input, "미세먼지") {
		region := strings.TrimSpace(strings.TrimSuffix(input, "미세먼지"))
		if region != "" {
			if _, ok := LookupLocation(region); ok {
				return weatherRegionalAirQuality, region
			}
		}
		return weatherRegionalAirQuality, region
	}

	// Remove "날씨" suffix to get the prefix
	prefix := input
	if strings.HasSuffix(input, "날씨") {
		prefix = strings.TrimSpace(strings.TrimSuffix(input, "날씨"))
	}

	// empty or just "날씨"
	if prefix == "" {
		return weatherNationalToday, ""
	}

	// "내일" or "내일 날씨"
	if prefix == "내일" {
		return weatherNationalTomorrow, ""
	}

	// "[region] 날씨" or just "[region]" with slash command
	if _, ok := LookupLocation(prefix); ok {
		return weatherRegionalForecast, prefix
	}

	// fallback: treat as region
	return weatherRegionalForecast, prefix
}

func (h *WeatherHandler) handleNationalToday(ctx context.Context, cmd bot.CommandContext) error {
	cities := NationalCities()
	lats := make([]float64, len(cities))
	lons := make([]float64, len(cities))
	for i, c := range cities {
		lats[i] = c.Lat
		lons[i] = c.Lon
	}

	forecasts, err := h.fetchBatchForecasts(ctx, cities)
	if err != nil {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: weatherUnavailableText,
		})
	}

	// Fetch air quality for each city
	airData := h.fetchBatchAirQuality(ctx, cities)

	display := make([]formatter.NationalCityWeather, len(cities))
	for i, c := range cities {
		fd := forecasts[i]
		icon, _ := providers.WeatherDescription(fd.WeatherCode)
		var minTemp, maxTemp float64
		if len(fd.DailyMinTemp) > 0 {
			minTemp = fd.DailyMinTemp[0]
		}
		if len(fd.DailyMaxTemp) > 0 {
			maxTemp = fd.DailyMaxTemp[0]
		}

		dustLabel := "보통"
		if airData[i] != nil {
			dustLabel = formatter.CombinedDustLabel(airData[i].PM10, airData[i].PM25)
		}

		display[i] = formatter.NationalCityWeather{
			Name:        c.ShortName,
			CurrentTemp: fd.CurrentTemp,
			Icon:        icon,
			MinTemp:     minTemp,
			MaxTemp:     maxTemp,
			DustLabel:   dustLabel,
		}
	}

	text := formatter.FormatNationalWeather("🌤️ 전국 날씨", display)
	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
	})
}

func (h *WeatherHandler) handleNationalTomorrow(ctx context.Context, cmd bot.CommandContext) error {
	cities := NationalCities()
	forecasts, err := h.fetchBatchForecasts(ctx, cities)
	if err != nil {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: weatherUnavailableText,
		})
	}

	display := make([]formatter.NationalTomorrowCity, len(cities))
	for i, c := range cities {
		fd := forecasts[i]
		var minTemp, maxTemp float64
		var weatherCode, precipProb int
		if len(fd.DailyMinTemp) > 1 {
			minTemp = fd.DailyMinTemp[1]
		}
		if len(fd.DailyMaxTemp) > 1 {
			maxTemp = fd.DailyMaxTemp[1]
		}
		if len(fd.DailyWeatherCode) > 1 {
			weatherCode = fd.DailyWeatherCode[1]
		}
		if len(fd.DailyPrecipProb) > 1 {
			precipProb = fd.DailyPrecipProb[1]
		}
		icon, _ := providers.WeatherDescription(weatherCode)

		display[i] = formatter.NationalTomorrowCity{
			Name:       c.ShortName,
			Icon:       icon,
			MinTemp:    minTemp,
			MaxTemp:    maxTemp,
			PrecipProb: precipProb,
		}
	}

	text := formatter.FormatNationalTomorrowWeather(display)
	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
	})
}

func (h *WeatherHandler) handleRegionalForecast(ctx context.Context, cmd bot.CommandContext, regionName string) error {
	loc, ok := LookupLocation(regionName)
	if !ok {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: unknownRegionText,
		})
	}

	fd, stale, err := h.fetchForecast(ctx, loc.Lat, loc.Lon)
	if err != nil {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: weatherUnavailableText,
		})
	}

	aq, _ := h.fetchAirQuality(ctx, loc.Lat, loc.Lon)
	yesterdayTemp, hasYesterday := h.fetchYesterday(ctx, loc.Lat, loc.Lon)

	icon, weatherText := providers.WeatherDescription(fd.WeatherCode)
	_ = icon

	morning, afternoon := providers.MorningAfternoonPrecipProb(fd.HourlyPrecipProb)

	pm10Label, pm25Label := "보통", "보통"
	if aq != nil {
		pm10Label = formatter.PM10Grade(aq.PM10).Label
		pm25Label = formatter.PM25Grade(aq.PM25).Label
	}

	var tomorrowMin, tomorrowMax float64
	var tomorrowCode, tomorrowPrecip int
	if len(fd.DailyMinTemp) > 1 {
		tomorrowMin = fd.DailyMinTemp[1]
	}
	if len(fd.DailyMaxTemp) > 1 {
		tomorrowMax = fd.DailyMaxTemp[1]
	}
	if len(fd.DailyWeatherCode) > 1 {
		tomorrowCode = fd.DailyWeatherCode[1]
	}
	if len(fd.DailyPrecipProb) > 1 {
		tomorrowPrecip = fd.DailyPrecipProb[1]
	}
	tomorrowIcon, _ := providers.WeatherDescription(tomorrowCode)

	displayName := loc.Name
	if loc.Name == loc.ShortName {
		displayName = loc.ShortName
	}

	d := formatter.RegionalWeatherData{
		RegionName:      displayName,
		CurrentTemp:     fd.CurrentTemp,
		ApparentTemp:    fd.ApparentTemp,
		Humidity:        fd.Humidity,
		WeatherIcon:     icon,
		WeatherText:     weatherText,
		YesterdayDiff:   fd.CurrentTemp - yesterdayTemp,
		HasYesterday:    hasYesterday,
		MorningPrecip:   morning,
		AfternoonPrecip: afternoon,
		DustPM10Label:   pm10Label,
		DustPM25Label:   pm25Label,
		TomorrowMinTemp: tomorrowMin,
		TomorrowMaxTemp: tomorrowMax,
		TomorrowIcon:    tomorrowIcon,
		TomorrowPrecip:  tomorrowPrecip,
		Stale:           stale,
	}

	text := formatter.FormatRegionalWeather(d)
	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
	})
}

func (h *WeatherHandler) handleRegionalAirQuality(ctx context.Context, cmd bot.CommandContext, regionName string) error {
	loc, ok := LookupLocation(regionName)
	if !ok {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: unknownRegionText,
		})
	}

	aq, stale, err := h.fetchAirQualityWithStale(ctx, loc.Lat, loc.Lon)
	if err != nil {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: airQualityUnavailableText,
		})
	}

	cai := formatter.CAI(aq.PM10, aq.PM25)

	displayName := loc.Name
	if loc.City == "서울" && loc.ShortName != "서울" {
		displayName = "서울시 " + loc.ShortName
	}

	d := formatter.AirQualityDisplay{
		RegionName: displayName,
		CAIIndex:   cai.Index,
		CAILabel:   cai.Label,
		CAIIcon:    cai.Icon,
		PM10:       aq.PM10,
		PM25:       aq.PM25,
		OzonePPM:   providers.OzoneUgToMPPM(aq.Ozone),
		Stale:      stale,
	}

	text := formatter.FormatAirQuality(d)
	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
	})
}

// --- Data fetching helpers with caching ---

func (h *WeatherHandler) fetchForecast(ctx context.Context, lat, lon float64) (*providers.ForecastData, bool, error) {
	if cached, ok := h.cache.GetForecast(lat, lon); ok && !cached.Stale {
		return cached.Data, false, nil
	}

	fd, err := h.provider.FetchForecast(ctx, lat, lon)
	if err != nil {
		// return stale data if available
		if cached, ok := h.cache.GetForecast(lat, lon); ok {
			return cached.Data, true, nil
		}
		return nil, false, err
	}

	h.cache.SetForecast(lat, lon, fd)
	return fd, false, nil
}

func (h *WeatherHandler) fetchAirQuality(ctx context.Context, lat, lon float64) (*providers.AirQualityData, error) {
	aq, _, err := h.fetchAirQualityWithStale(ctx, lat, lon)
	return aq, err
}

func (h *WeatherHandler) fetchAirQualityWithStale(ctx context.Context, lat, lon float64) (*providers.AirQualityData, bool, error) {
	if cached, ok := h.cache.GetAirQuality(lat, lon); ok && !cached.Stale {
		return cached.Data, false, nil
	}

	aq, err := h.provider.FetchAirQuality(ctx, lat, lon)
	if err != nil {
		if cached, ok := h.cache.GetAirQuality(lat, lon); ok {
			return cached.Data, true, nil
		}
		return nil, false, err
	}

	h.cache.SetAirQuality(lat, lon, aq)
	return aq, false, nil
}

func (h *WeatherHandler) fetchYesterday(ctx context.Context, lat, lon float64) (float64, bool) {
	if cached, ok := h.cache.GetYesterday(lat, lon); ok && !cached.Stale {
		return cached.Data, true
	}

	temp, err := h.provider.FetchYesterdayTemperature(ctx, lat, lon)
	if err != nil {
		if cached, ok := h.cache.GetYesterday(lat, lon); ok {
			return cached.Data, true
		}
		return 0, false
	}

	h.cache.SetYesterday(lat, lon, temp)
	return temp, true
}

func (h *WeatherHandler) fetchBatchForecasts(ctx context.Context, cities []Location) ([]*providers.ForecastData, error) {
	results, allCached := h.loadCachedForecasts(cities)
	if allCached {
		return results, nil
	}

	lats, lons := cityCoordinates(cities)
	fetched, err := h.provider.FetchBatchForecast(ctx, lats, lons)
	if err != nil {
		merged, ok := h.mergeStaleForecasts(cities, results)
		if ok {
			return merged, nil
		}
		return nil, err
	}

	h.applyFetchedForecasts(cities, results, fetched)
	return results, nil
}

func (h *WeatherHandler) loadCachedForecasts(cities []Location) ([]*providers.ForecastData, bool) {
	results := make([]*providers.ForecastData, len(cities))
	allCached := true
	for i, city := range cities {
		cached, ok := h.cache.GetForecast(city.Lat, city.Lon)
		if ok && !cached.Stale {
			results[i] = cached.Data
			continue
		}
		allCached = false
	}
	return results, allCached
}

func cityCoordinates(cities []Location) ([]float64, []float64) {
	lats := make([]float64, len(cities))
	lons := make([]float64, len(cities))
	for i, city := range cities {
		lats[i] = city.Lat
		lons[i] = city.Lon
	}
	return lats, lons
}

func (h *WeatherHandler) mergeStaleForecasts(cities []Location, results []*providers.ForecastData) ([]*providers.ForecastData, bool) {
	hasAny := false
	for i := range results {
		if results[i] != nil {
			hasAny = true
			continue
		}
		cached, ok := h.cache.GetForecast(cities[i].Lat, cities[i].Lon)
		if !ok {
			continue
		}
		results[i] = cached.Data
		hasAny = true
	}
	return results, hasAny
}

func (h *WeatherHandler) applyFetchedForecasts(cities []Location, results []*providers.ForecastData, fetched []*providers.ForecastData) {
	for i, forecast := range fetched {
		if forecast == nil {
			continue
		}
		h.cache.SetForecast(cities[i].Lat, cities[i].Lon, forecast)
		results[i] = forecast
	}
}

func (h *WeatherHandler) fetchBatchAirQuality(ctx context.Context, cities []Location) []*providers.AirQualityData {
	results := make([]*providers.AirQualityData, len(cities))
	for i, c := range cities {
		aq, err := h.fetchAirQuality(ctx, c.Lat, c.Lon)
		if err != nil {
			h.logger.Debug("air quality fetch failed", "city", c.ShortName, "error", err)
			continue
		}
		results[i] = aq
	}
	return results
}
