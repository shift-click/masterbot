package scraper

import (
	"fmt"
	"sync"
	"time"

	"github.com/shift-click/masterbot/internal/scraper/providers"
)

// WeatherCache is an in-memory cache for weather and air quality data.
type WeatherCache struct {
	mu sync.RWMutex

	forecasts  map[string]*cachedForecast
	airQuality map[string]*cachedAirQuality
	yesterday  map[string]*cachedYesterday

	forecastTTL  time.Duration
	yesterdayTTL time.Duration
}

type cachedForecast struct {
	data      *providers.ForecastData
	fetchedAt time.Time
}

type cachedAirQuality struct {
	data      *providers.AirQualityData
	fetchedAt time.Time
}

type cachedYesterday struct {
	temp      float64
	fetchedAt time.Time
}

// CacheResult wraps cached data with a stale flag.
type CacheResult[T any] struct {
	Data  T
	Stale bool
}

func NewWeatherCache(forecastTTL, yesterdayTTL time.Duration) *WeatherCache {
	return &WeatherCache{
		forecasts:    make(map[string]*cachedForecast),
		airQuality:   make(map[string]*cachedAirQuality),
		yesterday:    make(map[string]*cachedYesterday),
		forecastTTL:  forecastTTL,
		yesterdayTTL: yesterdayTTL,
	}
}

func locationKey(lat, lon float64) string {
	return fmt.Sprintf("%.4f,%.4f", lat, lon)
}

// GetForecast returns cached forecast data. Returns stale data if TTL expired.
func (c *WeatherCache) GetForecast(lat, lon float64) (*CacheResult[*providers.ForecastData], bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.forecasts[locationKey(lat, lon)]
	if !ok {
		return nil, false
	}
	stale := time.Since(entry.fetchedAt) > c.forecastTTL
	return &CacheResult[*providers.ForecastData]{Data: entry.data, Stale: stale}, true
}

// SetForecast stores forecast data in cache.
func (c *WeatherCache) SetForecast(lat, lon float64, data *providers.ForecastData) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.forecasts[locationKey(lat, lon)] = &cachedForecast{
		data:      data,
		fetchedAt: time.Now(),
	}
}

// GetAirQuality returns cached air quality data.
func (c *WeatherCache) GetAirQuality(lat, lon float64) (*CacheResult[*providers.AirQualityData], bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.airQuality[locationKey(lat, lon)]
	if !ok {
		return nil, false
	}
	stale := time.Since(entry.fetchedAt) > c.forecastTTL
	return &CacheResult[*providers.AirQualityData]{Data: entry.data, Stale: stale}, true
}

// SetAirQuality stores air quality data in cache.
func (c *WeatherCache) SetAirQuality(lat, lon float64, data *providers.AirQualityData) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.airQuality[locationKey(lat, lon)] = &cachedAirQuality{
		data:      data,
		fetchedAt: time.Now(),
	}
}

// GetYesterday returns cached yesterday temperature.
func (c *WeatherCache) GetYesterday(lat, lon float64) (*CacheResult[float64], bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.yesterday[locationKey(lat, lon)]
	if !ok {
		return nil, false
	}
	stale := time.Since(entry.fetchedAt) > c.yesterdayTTL
	return &CacheResult[float64]{Data: entry.temp, Stale: stale}, true
}

// SetYesterday stores yesterday temperature in cache.
func (c *WeatherCache) SetYesterday(lat, lon float64, temp float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.yesterday[locationKey(lat, lon)] = &cachedYesterday{
		temp:      temp,
		fetchedAt: time.Now(),
	}
}
