package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"
)

// OpenMeteo fetches weather and air quality data from the Open-Meteo API.
type OpenMeteo struct {
	client *BreakerHTTPClient
	logger *slog.Logger
}

func NewOpenMeteo(logger *slog.Logger) *OpenMeteo {
	if logger == nil {
		logger = slog.Default()
	}
	return &OpenMeteo{
		client: DefaultBreakerClient(10 * time.Second, "openmeteo", logger),
		logger: logger.With("component", "openmeteo"),
	}
}

// ForecastData holds weather data for a single location.
type ForecastData struct {
	CurrentTemp      float64
	ApparentTemp     float64
	Humidity         int
	WeatherCode      int
	DailyMaxTemp     []float64 // index 0=today, 1=tomorrow
	DailyMinTemp     []float64
	DailyWeatherCode []int
	DailyPrecipProb  []int // daily max precipitation probability
	// hourly precipitation probability for today (24 values)
	HourlyPrecipProb []int
}

// AirQualityData holds air quality data for a single location.
type AirQualityData struct {
	PM10  float64 // µg/m³
	PM25  float64 // µg/m³
	Ozone float64 // µg/m³
}

// FetchForecast fetches current weather and daily forecast for a single location.
func (o *OpenMeteo) FetchForecast(ctx context.Context, lat, lon float64) (*ForecastData, error) {
	results, err := o.FetchBatchForecast(ctx, []float64{lat}, []float64{lon})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("openmeteo: empty response")
	}
	return results[0], nil
}

// FetchBatchForecast fetches weather data for multiple locations in a single API call.
func (o *OpenMeteo) FetchBatchForecast(ctx context.Context, lats, lons []float64) ([]*ForecastData, error) {
	if len(lats) == 0 || len(lats) != len(lons) {
		return nil, fmt.Errorf("openmeteo: invalid coordinates")
	}

	latStrs := make([]string, len(lats))
	lonStrs := make([]string, len(lons))
	for i := range lats {
		latStrs[i] = fmt.Sprintf("%.4f", lats[i])
		lonStrs[i] = fmt.Sprintf("%.4f", lons[i])
	}

	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%s&longitude=%s"+
			"&current=temperature_2m,apparent_temperature,relative_humidity_2m,weather_code"+
			"&daily=temperature_2m_max,temperature_2m_min,weather_code,precipitation_probability_max"+
			"&hourly=precipitation_probability"+
			"&timezone=Asia%%2FSeoul&forecast_days=2",
		strings.Join(latStrs, ","),
		strings.Join(lonStrs, ","),
	)

	body, err := o.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("openmeteo forecast: %w", err)
	}

	if len(lats) == 1 {
		var single omForecastResponse
		if err := json.Unmarshal(body, &single); err != nil {
			return nil, fmt.Errorf("openmeteo forecast parse: %w", err)
		}
		return []*ForecastData{parseForecastResponse(&single)}, nil
	}

	var batch []omForecastResponse
	if err := json.Unmarshal(body, &batch); err != nil {
		return nil, fmt.Errorf("openmeteo forecast batch parse: %w", err)
	}

	results := make([]*ForecastData, len(batch))
	for i := range batch {
		results[i] = parseForecastResponse(&batch[i])
	}
	return results, nil
}

// FetchAirQuality fetches PM10, PM2.5, and ozone for a single location.
func (o *OpenMeteo) FetchAirQuality(ctx context.Context, lat, lon float64) (*AirQualityData, error) {
	url := fmt.Sprintf(
		"https://air-quality-api.open-meteo.com/v1/air-quality?latitude=%.4f&longitude=%.4f"+
			"&hourly=pm10,pm2_5,ozone&timezone=Asia%%2FSeoul&forecast_days=1",
		lat, lon,
	)

	body, err := o.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("openmeteo air quality: %w", err)
	}

	var resp omAirQualityResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("openmeteo air quality parse: %w", err)
	}

	return pickNearestAirQuality(&resp), nil
}

// FetchYesterdayTemperature fetches yesterday's temperature at the same hour.
func (o *OpenMeteo) FetchYesterdayTemperature(ctx context.Context, lat, lon float64) (float64, error) {
	now := time.Now().In(kstLocation())
	yesterday := now.AddDate(0, 0, -1)
	dateStr := yesterday.Format("2006-01-02")

	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f"+
			"&hourly=temperature_2m&timezone=Asia%%2FSeoul"+
			"&start_date=%s&end_date=%s",
		lat, lon, dateStr, dateStr,
	)

	body, err := o.doGet(ctx, url)
	if err != nil {
		return 0, fmt.Errorf("openmeteo yesterday: %w", err)
	}

	var resp struct {
		Hourly struct {
			Time        []string  `json:"time"`
			Temperature []float64 `json:"temperature_2m"`
		} `json:"hourly"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("openmeteo yesterday parse: %w", err)
	}

	targetHour := now.Hour()
	if targetHour < len(resp.Hourly.Temperature) {
		return resp.Hourly.Temperature[targetHour], nil
	}
	if len(resp.Hourly.Temperature) > 0 {
		return resp.Hourly.Temperature[len(resp.Hourly.Temperature)-1], nil
	}
	return 0, fmt.Errorf("openmeteo: no yesterday data")
}

func (o *OpenMeteo) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "jucobot/2.0")
	req.Header.Set("Accept", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	return io.ReadAll(resp.Body)
}

// --- JSON response structs ---

type omForecastResponse struct {
	Current struct {
		Temperature      float64 `json:"temperature_2m"`
		ApparentTemp     float64 `json:"apparent_temperature"`
		RelativeHumidity int     `json:"relative_humidity_2m"`
		WeatherCode      int     `json:"weather_code"`
	} `json:"current"`
	Daily struct {
		TempMax         []float64 `json:"temperature_2m_max"`
		TempMin         []float64 `json:"temperature_2m_min"`
		WeatherCode     []int     `json:"weather_code"`
		PrecipProbMax   []int     `json:"precipitation_probability_max"`
	} `json:"daily"`
	Hourly struct {
		Time       []string `json:"time"`
		PrecipProb []int    `json:"precipitation_probability"`
	} `json:"hourly"`
}

type omAirQualityResponse struct {
	Hourly struct {
		Time  []string  `json:"time"`
		PM10  []float64 `json:"pm10"`
		PM25  []float64 `json:"pm2_5"`
		Ozone []float64 `json:"ozone"`
	} `json:"hourly"`
}

func parseForecastResponse(resp *omForecastResponse) *ForecastData {
	fd := &ForecastData{
		CurrentTemp:      resp.Current.Temperature,
		ApparentTemp:     resp.Current.ApparentTemp,
		Humidity:         resp.Current.RelativeHumidity,
		WeatherCode:      resp.Current.WeatherCode,
		DailyMaxTemp:     resp.Daily.TempMax,
		DailyMinTemp:     resp.Daily.TempMin,
		DailyWeatherCode: resp.Daily.WeatherCode,
		DailyPrecipProb:  resp.Daily.PrecipProbMax,
	}

	// Extract today's hourly precip probs (first 24 entries)
	if len(resp.Hourly.PrecipProb) >= 24 {
		fd.HourlyPrecipProb = make([]int, 24)
		copy(fd.HourlyPrecipProb, resp.Hourly.PrecipProb[:24])
	}

	return fd
}

func pickNearestAirQuality(resp *omAirQualityResponse) *AirQualityData {
	now := time.Now().In(kstLocation())
	targetHour := now.Hour()

	idx := targetHour
	if idx >= len(resp.Hourly.PM10) {
		idx = len(resp.Hourly.PM10) - 1
	}
	if idx < 0 {
		return &AirQualityData{}
	}

	aq := &AirQualityData{}
	if idx < len(resp.Hourly.PM10) {
		aq.PM10 = resp.Hourly.PM10[idx]
	}
	if idx < len(resp.Hourly.PM25) {
		aq.PM25 = resp.Hourly.PM25[idx]
	}
	if idx < len(resp.Hourly.Ozone) {
		aq.Ozone = resp.Hourly.Ozone[idx]
	}
	return aq
}

func kstLocation() *time.Location {
	loc, _ := time.LoadLocation("Asia/Seoul")
	if loc == nil {
		loc = time.FixedZone("KST", 9*60*60)
	}
	return loc
}

// --- WMO Weather Code Mapping ---

// WeatherDescription returns an emoji icon and Korean text for a WMO weather code.
func WeatherDescription(code int) (icon string, text string) {
	switch code {
	case 0:
		return "☀️", "맑음"
	case 1:
		return "🌤️", "대체로 맑음"
	case 2:
		return "⛅", "구름 조금"
	case 3:
		return "☁️", "흐림"
	case 45, 48:
		return "🌫️", "안개"
	case 51:
		return "🌦️", "약한 이슬비"
	case 53:
		return "🌦️", "이슬비"
	case 55:
		return "🌧️", "강한 이슬비"
	case 56, 57:
		return "🌧️", "얼어붙는 이슬비"
	case 61:
		return "🌧️", "약한 비"
	case 63:
		return "🌧️", "비"
	case 65:
		return "🌧️", "강한 비"
	case 66, 67:
		return "🌧️", "얼어붙는 비"
	case 71:
		return "❄️", "약한 눈"
	case 73:
		return "❄️", "눈"
	case 75:
		return "❄️", "강한 눈"
	case 77:
		return "❄️", "싸락눈"
	case 80:
		return "🌧️", "약한 소나기"
	case 81:
		return "🌧️", "소나기"
	case 82:
		return "🌧️", "강한 소나기"
	case 85:
		return "❄️", "약한 눈소나기"
	case 86:
		return "❄️", "강한 눈소나기"
	case 95:
		return "⛈️", "뇌우"
	case 96, 99:
		return "⛈️", "우박 동반 뇌우"
	default:
		return "🌤️", "맑음"
	}
}

// OzoneUgToMPPM converts ozone from µg/m³ to ppm.
func OzoneUgToMPPM(ugm3 float64) float64 {
	// At standard conditions: 1 ppm O3 ≈ 1963 µg/m³ (molar mass 48, at 25°C 1atm)
	// Simplified: 1 µg/m³ ≈ 0.000509 ppm
	return math.Round(ugm3*0.000509*1000) / 1000
}

// MorningAfternoonPrecipProb calculates morning (6-12) and afternoon (12-18) max precip probability.
func MorningAfternoonPrecipProb(hourlyProb []int) (morning, afternoon int) {
	if len(hourlyProb) < 18 {
		return 0, 0
	}
	for i := 6; i < 12; i++ {
		if hourlyProb[i] > morning {
			morning = hourlyProb[i]
		}
	}
	for i := 12; i < 18; i++ {
		if hourlyProb[i] > afternoon {
			afternoon = hourlyProb[i]
		}
	}
	return morning, afternoon
}
