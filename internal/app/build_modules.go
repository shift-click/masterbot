package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/shift-click/masterbot/internal/alerting"
	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/command"
	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/coupang"
	"github.com/shift-click/masterbot/internal/metrics"
	"github.com/shift-click/masterbot/internal/scraper"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/store"
)

func addMetricsRecorderLifecycle(lifecycle *Lifecycle, recorder *metrics.AsyncRecorder) {
	if recorder == nil {
		return
	}
	lifecycle.Add("metrics-recorder", func(ctx context.Context) error {
		recorder.Start(ctx)
		<-ctx.Done()
		recorder.Wait()
		return nil
	})
}

func configureRouterMiddlewares(router *bot.Router, logger *slog.Logger, recorder metrics.Recorder, lifecycle *Lifecycle) {
	inlineNotifier := alerting.NewInlineNotifier(alerting.InlineNotifierConfig{
		TelegramBotToken: os.Getenv("JUCOBOT_ALERT_TELEGRAM_BOT_TOKEN"),
		TelegramChatID:   os.Getenv("JUCOBOT_ALERT_TELEGRAM_CHAT_ID"),
		TelegramAPIBase:  os.Getenv("JUCOBOT_ALERT_TELEGRAM_API_BASE"),
		AppName:          os.Getenv("JUCOBOT_ALERT_APP_NAME"),
	}, logger)
	rateLimiter := bot.NewRateLimitMiddleware(3, time.Second, recorder)
	lifecycle.Add("rate-limiter-cleanup", func(ctx context.Context) error {
		rateLimiter.StartCleanup(ctx)
		<-ctx.Done()
		return nil
	})
	router.Use(
		bot.NewErrorMiddleware(logger),
		rateLimiter,
		bot.NewLoggingMiddlewareWithNotifier(logger, recorder, inlineNotifier),
	)
}

func setupAccessRuntime(
	cfg config.Config,
	logger *slog.Logger,
	accessController *bot.AccessController,
	closers *[]func() error,
) (*bot.AccessManager, error) {
	if !cfg.Access.RuntimeEnabled() {
		return nil, nil
	}

	aclStore, err := store.NewSQLiteACLStore(cfg.Access.RuntimeDBPath)
	if err != nil {
		return nil, fmt.Errorf("initialize access control store: %w", err)
	}
	*closers = append(*closers, aclStore.Close)

	accessManager := bot.NewAccessManager(accessController, aclStore, cfg.Access, logger)
	if err := accessManager.Bootstrap(context.Background()); err != nil {
		return nil, fmt.Errorf("bootstrap runtime access control: %w", err)
	}
	return accessManager, nil
}

func setupGeminiClient(cfg config.Config, logger *slog.Logger) (*providers.GeminiClient, error) {
	if !cfg.Gemini.Enabled() {
		return nil, nil
	}
	geminiClient, err := providers.NewGeminiClient(context.Background(), cfg.Gemini.APIKey, cfg.Gemini.Model, logger)
	if err != nil {
		return nil, fmt.Errorf("initialize gemini client: %w", err)
	}
	return geminiClient, nil
}

type stockModule struct {
	handler    *command.StockHandler
	naverStock *providers.NaverStock
}

func setupStockModule(cfg config.Config, logger *slog.Logger, lifecycle *Lifecycle) stockModule {
	naverStock := providers.NewNaverStock(logger)

	hotlist := scraper.NewHotList(func(ctx context.Context, code string) (json.RawMessage, error) {
		quote, err := naverStock.FetchQuote(ctx, code)
		if err != nil {
			return nil, err
		}
		return json.Marshal(quote)
	}, scraper.HotListConfig{
		PollInterval:    cfg.Stock.PollInterval,
		IdleTimeout:     cfg.Stock.IdleTimeout,
		OffHourInterval: cfg.Stock.OffHourInterval,
		MarketOpen:      cfg.Stock.MarketOpen,
		MarketClose:     cfg.Stock.MarketClose,
	}, logger)
	hotlist.SetWorldFetcher(func(ctx context.Context, code string) (json.RawMessage, error) {
		quote, err := naverStock.FetchWorldQuote(ctx, code)
		if err != nil {
			return nil, err
		}
		return json.Marshal(quote)
	})
	lifecycle.Add("stock-hotlist", func(ctx context.Context) error {
		hotlist.Start(ctx)
		return nil
	})

	judalScraper := providers.NewJudalScraper(logger)
	themeIndex := scraper.NewThemeIndex(naverStock, judalScraper, logger)
	lifecycle.Add("theme-index", func(ctx context.Context) error {
		themeIndex.Start(ctx)
		return nil
	})
	return stockModule{
		handler:    command.NewStockHandler(naverStock, hotlist, themeIndex, logger),
		naverStock: naverStock,
	}
}

type coinModule struct {
	handler      *command.CoinHandler
	dunamuForex  *providers.DunamuForex
	coinResolver *providers.CoinResolver
}

func setupCoinModule(logger *slog.Logger, lifecycle *Lifecycle) coinModule {
	coinCache := scraper.NewCoinCache(logger)
	coinAliases := providers.NewCoinAliases()
	binanceSymbols := coinAliases.BinanceSymbols()
	upbitSymbols := coinAliases.UpbitSymbols()
	marketCapSymbols := coinAliases.MarketCapSymbols()

	binanceWS := providers.NewBinanceWS(binanceSymbols, coinCache.OnBinanceUpdate, logger)
	upbitWS := providers.NewUpbitWS(upbitSymbols, coinCache.OnUpbitUpdate, logger)
	lifecycle.Add("binance-ws", func(ctx context.Context) error {
		binanceWS.Start(ctx)
		return nil
	})
	lifecycle.Add("upbit-ws", func(ctx context.Context) error {
		upbitWS.Start(ctx)
		return nil
	})

	dunamuForex := providers.NewDunamuForex(logger)
	lifecycle.Add("dunamu-forex", func(ctx context.Context) error {
		dunamuForex.StartPolling(ctx, 5*time.Minute)
		return nil
	})
	lifecycle.Add("forex-sync", func(ctx context.Context) error {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				rate := dunamuForex.Rate()
				if rate.Rate > 0 {
					coinCache.UpdateForexRate(rate.Rate)
				}
			}
		}
	})

	coinGecko := providers.NewCoinGecko(logger)
	lifecycle.Add("coingecko-poll", func(ctx context.Context) error {
		coinGecko.StartPolling(ctx, 10*time.Minute, 1*time.Hour)
		return nil
	})
	lifecycle.Add("coingecko-marketcap-sync", func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(15 * time.Second):
		}

		syncMarketCaps := func() {
			for _, sym := range marketCapSymbols {
				if mc, ok := coinGecko.MarketCap(sym); ok {
					coinCache.UpdateMarketCap(sym, mc)
				}
			}
		}
		syncMarketCaps()

		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				syncMarketCaps()
			}
		}
	})

	dexScreener := providers.NewDexScreener(logger)
	dexHotList := scraper.NewCoinHotList(dexScreener, scraper.DefaultCoinHotListConfig(), logger)
	lifecycle.Add("dex-hotlist", func(ctx context.Context) error {
		dexHotList.Start(ctx)
		return nil
	})

	coinResolver := providers.NewCoinResolver(coinAliases, coinGecko, dexScreener, logger)
	return coinModule{
		handler:      command.NewCoinHandler(coinResolver, coinCache, coinGecko, dexScreener, dexHotList, logger),
		dunamuForex:  dunamuForex,
		coinResolver: coinResolver,
	}
}

type coupangModule struct {
	handler    *command.CoupangHandler
	priceStore store.PriceStore
}

func setupCoupangModule(
	cfg config.Config,
	logger *slog.Logger,
	lifecycle *Lifecycle,
	recorder metrics.Recorder,
	closers *[]func() error,
) (coupangModule, error) {
	priceStore, err := store.NewSQLitePriceStore(cfg.Coupang.DBPath)
	if err != nil {
		return coupangModule{}, fmt.Errorf("initialize coupang price store: %w", err)
	}
	*closers = append(*closers, priceStore.Close)

	coupangScraper := providers.NewCoupangScraper(logger, cfg.Coupang.ScraperProxyURL)
	fallcentResolver := providers.NewFallcentResolver(logger, cfg.Coupang.CandidateFanout)
	naverTitleResolver := providers.NewNaverTitleResolver(logger)
	coupangTracker := coupang.NewCoupangTracker(priceStore, coupangScraper, fallcentResolver, naverTitleResolver, coupang.CoupangTrackerConfig{
		CollectInterval:           cfg.Coupang.CollectInterval,
		IdleTimeout:               cfg.Coupang.IdleTimeout,
		MaxProducts:               cfg.Coupang.MaxProducts,
		HotInterval:               cfg.Coupang.HotInterval,
		WarmInterval:              cfg.Coupang.WarmInterval,
		ColdInterval:              cfg.Coupang.ColdInterval,
		Freshness:                 cfg.Coupang.Freshness,
		StaleThreshold:            cfg.Coupang.StaleThreshold,
		MinRefreshInterval:        cfg.Coupang.MinRefreshInterval,
		RefreshBudgetPerHour:      cfg.Coupang.RefreshBudgetPerHour,
		RegistrationBudgetPerHour: cfg.Coupang.RegistrationBudgetPerHour,
		ResolutionBudgetPerHour:   cfg.Coupang.ResolutionBudgetPerHour,
		TierWindow:                cfg.Coupang.TierWindow,
		HotThreshold:              cfg.Coupang.HotThreshold,
		WarmThreshold:             cfg.Coupang.WarmThreshold,
		CandidateFanout:           cfg.Coupang.CandidateFanout,
		MappingRecheckBackoff:     cfg.Coupang.MappingRecheckBackoff,
		AllowAuxiliaryFallback:    cfg.Coupang.AllowAuxiliaryFallback,
		RegistrationLatencyBudget: cfg.Coupang.RegistrationLatencyBudget,
		ReadRefreshTimeout:        cfg.Coupang.ReadRefreshTimeout,
		LookupCoalescingEnabled:   cfg.Coupang.LookupCoalescingEnabled,
		RegistrationJoinWait:      cfg.Coupang.RegistrationJoinWait,
		ReadRefreshJoinWait:       cfg.Coupang.ReadRefreshJoinWait,
		ChartBackfillInterval:     cfg.Coupang.ChartBackfillInterval,
	}, logger)
	coupangTracker.SetMetricsRecorder(recorder)

	lifecycle.Add("coupang-async", func(ctx context.Context) error {
		coupangTracker.InitAsyncGroup(ctx)
		<-ctx.Done()
		if err := coupangTracker.WaitAsync(); err != nil {
			logger.Warn("coupang async work completed with errors", "error", err)
		}
		return nil
	})

	watchlist := scraper.NewCoupangWatchlist(coupangTracker, scraper.CoupangWatchlistConfig{
		CollectInterval: cfg.Coupang.CollectInterval,
		IdleTimeout:     cfg.Coupang.IdleTimeout,
		EvictInterval:   time.Hour,
		MaxProducts:     cfg.Coupang.MaxProducts,
	}, logger)
	lifecycle.Add("coupang-watchlist", func(ctx context.Context) error {
		watchlist.Start(ctx)
		return nil
	})

	return coupangModule{
		handler:    command.NewCoupangHandler(coupangTracker, logger),
		priceStore: priceStore,
	}, nil
}

type sportsModule struct {
	footballHandler *command.FootballHandler
	esportsHandler  *command.EsportsHandler
}

func setupSportsModule(cfg config.Config, logger *slog.Logger, lifecycle *Lifecycle) sportsModule {
	return configureSportsModule(cfg, logger, lifecycle)
}

func setupBaseballModule(cfg config.Config, logger *slog.Logger, lifecycle *Lifecycle) *command.BaseballHandler {
	return configureBaseballModule(cfg, logger, lifecycle)
}

func koreaLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil || loc == nil {
		return time.FixedZone("KST", 9*60*60)
	}
	return loc
}
