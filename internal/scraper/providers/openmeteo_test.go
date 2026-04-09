package providers

import (
	"encoding/json"
	"testing"
)

func TestParseForecastResponse(t *testing.T) {
	raw := `{
		"current": {
			"temperature_2m": 3.5,
			"apparent_temperature": 1.2,
			"relative_humidity_2m": 52,
			"weather_code": 0
		},
		"daily": {
			"temperature_2m_max": [11.0, 14.0],
			"temperature_2m_min": [1.0, 2.0],
			"weather_code": [0, 1],
			"precipitation_probability_max": [20, 0]
		},
		"hourly": {
			"time": ["2026-03-19T00:00","2026-03-19T01:00"],
			"precipitation_probability": [0,0,0,0,0,0,5,10,15,10,5,0,20,25,20,15,10,5,0,0,0,0,0,0]
		}
	}`

	var resp omForecastResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}

	fd := parseForecastResponse(&resp)
	if fd.CurrentTemp != 3.5 {
		t.Errorf("CurrentTemp=%v, want 3.5", fd.CurrentTemp)
	}
	if fd.ApparentTemp != 1.2 {
		t.Errorf("ApparentTemp=%v, want 1.2", fd.ApparentTemp)
	}
	if fd.Humidity != 52 {
		t.Errorf("Humidity=%v, want 52", fd.Humidity)
	}
	if fd.WeatherCode != 0 {
		t.Errorf("WeatherCode=%v, want 0", fd.WeatherCode)
	}
	if len(fd.DailyMaxTemp) != 2 || fd.DailyMaxTemp[0] != 11.0 {
		t.Errorf("DailyMaxTemp=%v, want [11 14]", fd.DailyMaxTemp)
	}
	if len(fd.HourlyPrecipProb) != 24 {
		t.Errorf("HourlyPrecipProb len=%d, want 24", len(fd.HourlyPrecipProb))
	}
}

func TestParseAirQualityResponse(t *testing.T) {
	raw := `{
		"hourly": {
			"time": ["2026-03-19T00:00","2026-03-19T01:00","2026-03-19T02:00","2026-03-19T03:00",
				"2026-03-19T04:00","2026-03-19T05:00","2026-03-19T06:00","2026-03-19T07:00",
				"2026-03-19T08:00","2026-03-19T09:00","2026-03-19T10:00","2026-03-19T11:00",
				"2026-03-19T12:00","2026-03-19T13:00","2026-03-19T14:00","2026-03-19T15:00",
				"2026-03-19T16:00","2026-03-19T17:00","2026-03-19T18:00","2026-03-19T19:00",
				"2026-03-19T20:00","2026-03-19T21:00","2026-03-19T22:00","2026-03-19T23:00"],
			"pm10": [27,28,30,25,22,20,27,30,35,32,28,25,22,20,18,17,16,15,14,13,12,11,10,9],
			"pm2_5": [13,14,15,12,10,9,13,15,18,16,14,12,10,9,8,7,7,6,6,5,5,4,4,3],
			"ozone": [60,58,55,52,50,48,59,65,70,72,75,78,80,82,84,85,83,80,75,70,65,62,60,58]
		}
	}`

	var resp omAirQualityResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}

	aq := pickNearestAirQuality(&resp)
	if aq.PM10 <= 0 {
		t.Error("PM10 should be > 0")
	}
	if aq.PM25 <= 0 {
		t.Error("PM25 should be > 0")
	}
	if aq.Ozone <= 0 {
		t.Error("Ozone should be > 0")
	}
}

func TestWeatherDescription(t *testing.T) {
	tests := []struct {
		code     int
		wantIcon string
		wantText string
	}{
		{0, "☀️", "맑음"},
		{2, "⛅", "구름 조금"},
		{3, "☁️", "흐림"},
		{61, "🌧️", "약한 비"},
		{73, "❄️", "눈"},
		{95, "⛈️", "뇌우"},
	}

	for _, tt := range tests {
		icon, text := WeatherDescription(tt.code)
		if icon != tt.wantIcon || text != tt.wantText {
			t.Errorf("WeatherDescription(%d) = (%q, %q), want (%q, %q)", tt.code, icon, text, tt.wantIcon, tt.wantText)
		}
	}
}

func TestOzoneConversion(t *testing.T) {
	ppm := OzoneUgToMPPM(59.0)
	if ppm < 0.029 || ppm > 0.031 {
		t.Errorf("OzoneUgToMPPM(59) = %v, want ~0.030", ppm)
	}
}

func TestMorningAfternoonPrecipProb(t *testing.T) {
	hourly := []int{0, 0, 0, 0, 0, 0, 5, 10, 15, 10, 5, 0, 20, 25, 20, 15, 10, 5, 0, 0, 0, 0, 0, 0}
	am, pm := MorningAfternoonPrecipProb(hourly)
	if am != 15 {
		t.Errorf("morning=%d, want 15", am)
	}
	if pm != 25 {
		t.Errorf("afternoon=%d, want 25", pm)
	}
}

func TestParseBatchForecastResponse(t *testing.T) {
	raw := `[
		{"current":{"temperature_2m":3.5,"apparent_temperature":1.2,"relative_humidity_2m":52,"weather_code":0},"daily":{"temperature_2m_max":[11],"temperature_2m_min":[1],"weather_code":[0],"precipitation_probability_max":[0]},"hourly":{"time":[],"precipitation_probability":[]}},
		{"current":{"temperature_2m":5.0,"apparent_temperature":3.0,"relative_humidity_2m":60,"weather_code":3},"daily":{"temperature_2m_max":[12],"temperature_2m_min":[2],"weather_code":[3],"precipitation_probability_max":[30]},"hourly":{"time":[],"precipitation_probability":[]}}
	]`

	var batch []omForecastResponse
	if err := json.Unmarshal([]byte(raw), &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch) != 2 {
		t.Fatalf("len=%d, want 2", len(batch))
	}

	fd0 := parseForecastResponse(&batch[0])
	fd1 := parseForecastResponse(&batch[1])
	if fd0.CurrentTemp != 3.5 {
		t.Errorf("batch[0] temp=%v, want 3.5", fd0.CurrentTemp)
	}
	if fd1.CurrentTemp != 5.0 {
		t.Errorf("batch[1] temp=%v, want 5.0", fd1.CurrentTemp)
	}
}
