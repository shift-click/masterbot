package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

const EnvPrefix = "JUCOBOT_"

type Config struct {
	Bot      BotConfig
	Iris     IrisConfig
	HTTPTest HTTPTestConfig
	Cache    CacheConfig
	Store    StoreConfig
	Stock    StockConfig
	Coupang  CoupangConfig
	Lotto    LottoConfig
	Sports   SportsConfig
	Weather  WeatherConfig
	AutoQuery AutoQueryConfig
	Access   AccessConfig
	Admin    AdminConfig
	Gemini   GeminiConfig
	Chart    ChartConfig
}

type rawConfig struct {
	Bot      rawBotConfig      `koanf:"bot"`
	Iris     rawIrisConfig     `koanf:"iris"`
	HTTPTest rawHTTPTestConfig `koanf:"http_test"`
	Cache    rawCacheConfig    `koanf:"cache"`
	Store    rawStoreConfig    `koanf:"store"`
	Stock    rawStockConfig    `koanf:"stock"`
	Coupang  rawCoupangConfig  `koanf:"coupang"`
	Lotto    rawLottoConfig    `koanf:"lotto"`
	Sports   rawSportsConfig   `koanf:"sports"`
	Weather  rawWeatherConfig  `koanf:"weather"`
	AutoQuery rawAutoQueryConfig `koanf:"auto_query"`
	Access   rawAccessConfig   `koanf:"access"`
	Admin    rawAdminConfig    `koanf:"admin"`
	Gemini   rawGeminiConfig   `koanf:"gemini"`
	Chart    rawChartConfig    `koanf:"chart"`
}

func Default() Config {
	return Config{
		Bot:      defaultBotConfig(),
		Iris:     defaultIrisConfig(),
		HTTPTest: defaultHTTPTestConfig(),
		Cache:    defaultCacheConfig(),
		Store:    defaultStoreConfig(),
		Stock:    defaultStockConfig(),
		Coupang:  defaultCoupangConfig(),
		Lotto:    defaultLottoConfig(),
		Sports:   defaultSportsConfig(),
		Weather:  defaultWeatherConfig(),
		AutoQuery: defaultAutoQueryConfig(),
		Access:   defaultAccessConfig(),
		Admin:    defaultAdminConfig(),
		Gemini:   defaultGeminiConfig(),
		Chart:    defaultChartConfig(),
	}
}

func Load(path string) (Config, error) {
	k := koanf.New(".")
	raw := defaultRawConfig()

	if _, err := os.Stat(path); err == nil {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return Config{}, fmt.Errorf("load config file: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("stat config file: %w", err)
	}

	if err := k.Load(env.Provider(EnvPrefix, ".", mapEnvKey), nil); err != nil {
		return Config{}, fmt.Errorf("load env config: %w", err)
	}

	if err := k.Unmarshal("", &raw); err != nil {
		return Config{}, fmt.Errorf("unmarshal config: %w", err)
	}

	cfg, err := raw.materialize()
	if err != nil {
		return Config{}, err
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	var problems []string

	c.Bot.validate(&problems)
	c.Iris.validate(&problems)
	c.HTTPTest.validate(&problems)
	c.Cache.validate(&problems)
	c.Store.validate(&problems)
	c.Stock.validate(&problems)
	c.Coupang.validate(&problems)
	c.Lotto.validate(&problems)
	c.Sports.validate(&problems)
	c.Weather.validate(&problems)
	c.AutoQuery.validate(&problems)
	c.Access.validate(&problems)
	c.Admin.validate(&problems)
	c.Gemini.validate(&problems)
	c.Chart.validate(&problems)

	if c.Iris.Enabled && c.HTTPTest.Enabled {
		problems = append(problems, "iris and http_test transports are mutually exclusive; enable only one")
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid config: %s", strings.Join(problems, "; "))
	}

	return nil
}

func mapEnvKey(key string) string {
	trimmed := strings.TrimPrefix(key, EnvPrefix)
	parts := strings.Split(strings.ToLower(trimmed), "_")
	if len(parts) < 2 {
		return strings.ToLower(trimmed)
	}

	return parts[0] + "." + strings.Join(parts[1:], "_")
}

func defaultRawConfig() rawConfig {
	return rawConfig{
		Bot:      defaultRawBotConfig(),
		Iris:     defaultRawIrisConfig(),
		HTTPTest: defaultRawHTTPTestConfig(),
		Cache:    defaultRawCacheConfig(),
		Store:    defaultRawStoreConfig(),
		Stock:    defaultRawStockConfig(),
		Coupang:  defaultRawCoupangConfig(),
		Lotto:    defaultRawLottoConfig(),
		Sports:   defaultRawSportsConfig(),
		Weather:  defaultRawWeatherConfig(),
		AutoQuery: defaultRawAutoQueryConfig(),
		Access:   defaultRawAccessConfig(),
		Admin:    defaultRawAdminConfig(),
		Gemini:   defaultRawGeminiConfig(),
		Chart:    defaultRawChartConfig(),
	}
}

func (r rawConfig) materialize() (Config, error) {
	botCfg, err := r.Bot.materialize()
	if err != nil {
		return Config{}, err
	}
	irisCfg, err := r.Iris.materialize()
	if err != nil {
		return Config{}, err
	}
	httpTestCfg, err := r.HTTPTest.materialize()
	if err != nil {
		return Config{}, err
	}
	cacheCfg, err := r.Cache.materialize()
	if err != nil {
		return Config{}, err
	}
	storeCfg, err := r.Store.materialize()
	if err != nil {
		return Config{}, err
	}
	stockCfg, err := r.Stock.materialize()
	if err != nil {
		return Config{}, err
	}
	coupangCfg, err := r.Coupang.materialize()
	if err != nil {
		return Config{}, err
	}
	lottoCfg, err := r.Lotto.materialize()
	if err != nil {
		return Config{}, err
	}
	sportsCfg, err := r.Sports.materialize()
	if err != nil {
		return Config{}, err
	}
	weatherCfg, err := r.Weather.materialize()
	if err != nil {
		return Config{}, err
	}
	autoQueryCfg, err := r.AutoQuery.materialize()
	if err != nil {
		return Config{}, err
	}
	accessCfg, err := r.Access.materialize()
	if err != nil {
		return Config{}, err
	}
	adminCfg, err := r.Admin.materialize()
	if err != nil {
		return Config{}, err
	}
	geminiCfg, err := r.Gemini.materialize()
	if err != nil {
		return Config{}, err
	}
	chartCfg, err := r.Chart.materialize()
	if err != nil {
		return Config{}, err
	}

	return Config{
		Bot:      botCfg,
		Iris:     irisCfg,
		HTTPTest: httpTestCfg,
		Cache:    cacheCfg,
		Store:    storeCfg,
		Stock:    stockCfg,
		Coupang:  coupangCfg,
		Lotto:    lottoCfg,
		Sports:   sportsCfg,
		Weather:  weatherCfg,
		AutoQuery: autoQueryCfg,
		Access:   accessCfg,
		Admin:    adminCfg,
		Gemini:   geminiCfg,
		Chart:    chartCfg,
	}, nil
}
