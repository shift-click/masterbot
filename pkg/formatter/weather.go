package formatter

import (
	"fmt"
	"math"
	"strings"
)

// --- CAI (Comprehensive Air-quality Index) ---

// DustGrade represents a Korean air quality grade.
type DustGrade struct {
	Label string // "좋음", "보통", "나쁨", "매우나쁨"
	Icon  string // "🟢", "🟡", "🟠", "🔴"
	Index int    // numeric CAI index
}

// PM10Grade returns the grade for PM10 concentration.
func PM10Grade(ugm3 float64) DustGrade {
	v := int(math.Round(ugm3))
	switch {
	case v <= 30:
		return DustGrade{"좋음", "🟢", linearIndex(v, 0, 30, 0, 50)}
	case v <= 80:
		return DustGrade{"보통", "🟡", linearIndex(v, 31, 80, 51, 100)}
	case v <= 150:
		return DustGrade{"나쁨", "🟠", linearIndex(v, 81, 150, 101, 250)}
	default:
		return DustGrade{"매우나쁨", "🔴", linearIndex(v, 151, 600, 251, 500)}
	}
}

// PM25Grade returns the grade for PM2.5 concentration.
func PM25Grade(ugm3 float64) DustGrade {
	v := int(math.Round(ugm3))
	switch {
	case v <= 15:
		return DustGrade{"좋음", "🟢", linearIndex(v, 0, 15, 0, 50)}
	case v <= 35:
		return DustGrade{"보통", "🟡", linearIndex(v, 16, 35, 51, 100)}
	case v <= 75:
		return DustGrade{"나쁨", "🟠", linearIndex(v, 36, 75, 101, 250)}
	default:
		return DustGrade{"매우나쁨", "🔴", linearIndex(v, 76, 500, 251, 500)}
	}
}

// CAI returns the Comprehensive Air-quality Index (max of PM10 and PM2.5 indices).
func CAI(pm10, pm25 float64) DustGrade {
	g10 := PM10Grade(pm10)
	g25 := PM25Grade(pm25)
	if g25.Index >= g10.Index {
		return g25
	}
	return g10
}

// CombinedDustLabel returns the worse of PM10 and PM2.5 grades as a simple label.
func CombinedDustLabel(pm10, pm25 float64) string {
	g10 := PM10Grade(pm10)
	g25 := PM25Grade(pm25)
	if g25.Index >= g10.Index {
		return g25.Label
	}
	return g10.Label
}

func linearIndex(value, cLow, cHigh, iLow, iHigh int) int {
	if cHigh == cLow {
		return iLow
	}
	idx := float64(iHigh-iLow)/float64(cHigh-cLow)*float64(value-cLow) + float64(iLow)
	return int(math.Round(idx))
}

// --- Weather Data Types ---

// NationalCityWeather holds data for one city in the national overview.
type NationalCityWeather struct {
	Name        string
	CurrentTemp float64
	Icon        string
	MinTemp     float64
	MaxTemp     float64
	DustLabel   string // "좋음", "보통", etc.
}

// RegionalWeatherData holds detailed weather data for a specific region.
type RegionalWeatherData struct {
	RegionName     string
	CurrentTemp    float64
	ApparentTemp   float64
	Humidity       int
	WeatherIcon    string
	WeatherText    string
	YesterdayDiff  float64 // positive = warmer than yesterday
	HasYesterday   bool
	MorningPrecip  int
	AfternoonPrecip int
	DustPM10Label  string
	DustPM25Label  string
	// tomorrow forecast
	TomorrowMinTemp  float64
	TomorrowMaxTemp  float64
	TomorrowIcon     string
	TomorrowPrecip   int
	TomorrowDustLabel string
	Stale            bool
}

// AirQualityDisplay holds data for the air quality detail view.
type AirQualityDisplay struct {
	RegionName string
	CAIIndex   int
	CAILabel   string
	CAIIcon    string
	PM10       float64
	PM25       float64
	OzonePPM   float64
	Stale      bool
}

// --- Formatting Functions ---

// FormatNationalWeather formats the 10-city national weather overview.
func FormatNationalWeather(title string, cities []NationalCityWeather) string {
	var b strings.Builder
	b.WriteString(title)
	b.WriteByte('\n')
	for i, c := range cities {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(fmt.Sprintf("%s: %.1f° %s (%.0f° / %.0f°)",
			c.Name, c.CurrentTemp, c.Icon, c.MinTemp, c.MaxTemp))
		b.WriteString(fmt.Sprintf("\n(😷 미세먼지 %s)", c.DustLabel))
	}
	return b.String()
}

// FormatNationalTomorrowWeather formats the national tomorrow forecast.
func FormatNationalTomorrowWeather(cities []NationalTomorrowCity) string {
	var b strings.Builder
	b.WriteString("📌 내일 전국 날씨\n")
	for i, c := range cities {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(fmt.Sprintf("%s: %s (%.0f° / %.0f°) ☔%d%%",
			c.Name, c.Icon, c.MinTemp, c.MaxTemp, c.PrecipProb))
	}
	return b.String()
}

// NationalTomorrowCity holds data for one city in the tomorrow forecast.
type NationalTomorrowCity struct {
	Name       string
	Icon       string
	MinTemp    float64
	MaxTemp    float64
	PrecipProb int
}

// FormatRegionalWeather formats detailed weather for a single region.
func FormatRegionalWeather(d RegionalWeatherData) string {
	var b strings.Builder

	if d.Stale {
		b.WriteString("[⚠️ 캐시된 데이터]\n")
	}

	b.WriteString(fmt.Sprintf("🌤️ %s 날씨\n", d.RegionName))
	b.WriteByte('\n')
	b.WriteString(fmt.Sprintf("🌡️ 온도: %.1f° (체감: %.1f°)\n", d.CurrentTemp, d.ApparentTemp))
	b.WriteString(fmt.Sprintf("💧 습도: %d%%\n", d.Humidity))

	weatherLine := fmt.Sprintf("☁️ 날씨: %s", d.WeatherText)
	if d.HasYesterday {
		diff := d.YesterdayDiff
		if diff > 0 {
			weatherLine += fmt.Sprintf(" (어제보다 %.1f° 높아요)", diff)
		} else if diff < 0 {
			weatherLine += fmt.Sprintf(" (어제보다 %.1f° 낮아요)", math.Abs(diff))
		} else {
			weatherLine += " (어제와 같아요)"
		}
	}
	b.WriteString(weatherLine)
	b.WriteByte('\n')

	b.WriteString(fmt.Sprintf("☔ 강수: 오전%d%% 오후%d%%\n", d.MorningPrecip, d.AfternoonPrecip))
	b.WriteString(fmt.Sprintf("😷 미세먼지: %s / 초미세먼지: %s", d.DustPM10Label, d.DustPM25Label))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("📌 내일: %.0f° / %.0f°, %s\n", d.TomorrowMinTemp, d.TomorrowMaxTemp, d.TomorrowIcon))
	b.WriteString(fmt.Sprintf("☔ 강수: %d%%", d.TomorrowPrecip))
	if d.TomorrowDustLabel != "" {
		b.WriteString(fmt.Sprintf("\n😷 미세먼지: %s", d.TomorrowDustLabel))
	}

	return b.String()
}

// FormatAirQuality formats detailed air quality for a single region.
func FormatAirQuality(d AirQualityDisplay) string {
	var b strings.Builder

	if d.Stale {
		b.WriteString("[⚠️ 캐시된 데이터]\n")
	}

	b.WriteString(fmt.Sprintf("🌬️ %s 실시간 대기환경\n\n", d.RegionName))
	b.WriteString(fmt.Sprintf("상태: %s %s (통합대기지수: %d)\n", d.CAIIcon, d.CAILabel, d.CAIIndex))
	b.WriteString(fmt.Sprintf("미세먼지(PM10): %.0f ㎍/㎥\n", d.PM10))
	b.WriteString(fmt.Sprintf("초미세먼지(PM2.5): %.0f ㎍/㎥\n", d.PM25))
	b.WriteString(fmt.Sprintf("오존(O3): %.2f ppm", d.OzonePPM))

	return b.String()
}
