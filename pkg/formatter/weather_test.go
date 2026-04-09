package formatter

import (
	"strings"
	"testing"
)

func TestPM10Grade(t *testing.T) {
	tests := []struct {
		ugm3  float64
		label string
	}{
		{0, "좋음"},
		{15, "좋음"},
		{30, "좋음"},
		{31, "보통"},
		{50, "보통"},
		{80, "보통"},
		{81, "나쁨"},
		{150, "나쁨"},
		{151, "매우나쁨"},
		{300, "매우나쁨"},
	}
	for _, tt := range tests {
		g := PM10Grade(tt.ugm3)
		if g.Label != tt.label {
			t.Errorf("PM10Grade(%.0f).Label=%q, want %q", tt.ugm3, g.Label, tt.label)
		}
	}
}

func TestPM25Grade(t *testing.T) {
	tests := []struct {
		ugm3  float64
		label string
	}{
		{0, "좋음"},
		{10, "좋음"},
		{15, "좋음"},
		{16, "보통"},
		{25, "보통"},
		{35, "보통"},
		{36, "나쁨"},
		{75, "나쁨"},
		{76, "매우나쁨"},
	}
	for _, tt := range tests {
		g := PM25Grade(tt.ugm3)
		if g.Label != tt.label {
			t.Errorf("PM25Grade(%.0f).Label=%q, want %q", tt.ugm3, g.Label, tt.label)
		}
	}
}

func TestCAI(t *testing.T) {
	// PM10=27 → 좋음(idx~45), PM2.5=13 → 좋음(idx~43) → CAI=45
	g := CAI(27, 13)
	if g.Label != "좋음" {
		t.Errorf("CAI(27,13).Label=%q, want 좋음", g.Label)
	}

	// PM10=27 → 좋음(~45), PM2.5=20 → 보통(~63) → CAI picks PM2.5
	g2 := CAI(27, 20)
	if g2.Label != "보통" {
		t.Errorf("CAI(27,20).Label=%q, want 보통", g2.Label)
	}
}

func TestCombinedDustLabel(t *testing.T) {
	label := CombinedDustLabel(27, 13)
	if label != "좋음" {
		t.Errorf("CombinedDustLabel(27,13)=%q, want 좋음", label)
	}
	label2 := CombinedDustLabel(50, 20)
	if label2 != "보통" {
		t.Errorf("CombinedDustLabel(50,20)=%q, want 보통", label2)
	}
}

func TestFormatNationalWeather(t *testing.T) {
	cities := []NationalCityWeather{
		{Name: "서울", CurrentTemp: 3.5, Icon: "☀️", MinTemp: 1, MaxTemp: 11, DustLabel: "보통"},
		{Name: "부산", CurrentTemp: 8.0, Icon: "☁️", MinTemp: 6, MaxTemp: 15, DustLabel: "좋음"},
	}
	result := FormatNationalWeather("🌤️ 전국 날씨", cities)
	if !strings.Contains(result, "서울: 3.5°") {
		t.Error("missing 서울 temperature")
	}
	if !strings.Contains(result, "미세먼지 보통") {
		t.Error("missing dust grade")
	}
	if !strings.Contains(result, "부산: 8.0°") {
		t.Error("missing 부산 temperature")
	}
}

func TestFormatRegionalWeather(t *testing.T) {
	d := RegionalWeatherData{
		RegionName:      "서울특별시 강남구",
		CurrentTemp:     3.5,
		ApparentTemp:    2.4,
		Humidity:        52,
		WeatherIcon:     "☀️",
		WeatherText:     "맑음",
		YesterdayDiff:   -2.4,
		HasYesterday:    true,
		MorningPrecip:   0,
		AfternoonPrecip: 20,
		DustPM10Label:   "좋음",
		DustPM25Label:   "좋음",
		TomorrowMinTemp: 2,
		TomorrowMaxTemp: 14,
		TomorrowIcon:    "☀️",
		TomorrowPrecip:  0,
	}
	result := FormatRegionalWeather(d)
	if !strings.Contains(result, "강남구 날씨") {
		t.Error("missing region name")
	}
	if !strings.Contains(result, "3.5°") {
		t.Error("missing temperature")
	}
	if !strings.Contains(result, "어제보다 2.4° 낮아요") {
		t.Error("missing yesterday comparison")
	}
	if !strings.Contains(result, "오전0% 오후20%") {
		t.Error("missing precip prob")
	}
}

func TestFormatAirQuality(t *testing.T) {
	d := AirQualityDisplay{
		RegionName: "서울시 강남구",
		CAIIndex:   59,
		CAILabel:   "보통",
		CAIIcon:    "🟡",
		PM10:       27,
		PM25:       13,
		OzonePPM:   0.03,
	}
	result := FormatAirQuality(d)
	if !strings.Contains(result, "통합대기지수: 59") {
		t.Error("missing CAI index")
	}
	if !strings.Contains(result, "PM10") {
		t.Error("missing PM10")
	}
}
