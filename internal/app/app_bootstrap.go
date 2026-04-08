package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shift-click/masterbot/internal/admin"
	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/command"
	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/intent"
	"github.com/shift-click/masterbot/internal/metrics"
	"github.com/shift-click/masterbot/internal/store"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/internal/transport/httptest"
	"github.com/shift-click/masterbot/internal/transport/iris"
)

type bootstrapAssembly struct {
	lifecycle        *Lifecycle
	closers          []func() error
	metricsStore     *metrics.SQLiteStore
	metricsRecorder  *metrics.AsyncRecorder
	accessController *bot.AccessController
	accessManager    *bot.AccessManager
	router           *bot.Router
	featureModules   []featureModule
}

type featureAssembly struct {
	runtimes    []featureRuntime
	priceStore  store.PriceStore
	smokeProbes []admin.CommandSmokeProbe
}

func Build(cfg config.Config, logger *slog.Logger) (*App, error) {
	if logger == nil {
		logger = NewLogger(cfg.Bot.LogLevel)
	}

	if cfg.HTTPTest.Enabled {
		cfg.Access.DefaultPolicy = config.AccessPolicyAllow
	}

	bootstrap, err := assembleBootstrapRuntime(cfg, logger)
	if err != nil {
		return nil, err
	}

	features, err := assembleFeatureModules(cfg, logger, &bootstrap)
	if err != nil {
		return nil, err
	}

	if err := registerFeatureRuntimes(bootstrap.router, features.runtimes); err != nil {
		return nil, fmt.Errorf("register feature runtimes: %w", err)
	}
	if err := bootstrap.router.ValidateAccess(); err != nil {
		return nil, fmt.Errorf("validate access: %w", err)
	}

	adapter, err := initTransportAdapter(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("initialize transport adapter: %w", err)
	}
	attachTransportMetrics(adapter, bootstrap.metricsRecorder)
	attachAdapters(adapter, features.runtimes, &bootstrap.closers)

	if err := wireAdminRuntime(cfg, logger, bootstrap.lifecycle, bootstrap.metricsStore, features.priceStore, bootstrap.router, features.smokeProbes); err != nil {
		return nil, err
	}

	return &App{
		cfg:       cfg,
		logger:    logger,
		router:    bootstrap.router,
		adapter:   adapter,
		lifecycle: bootstrap.lifecycle,
		closers:   bootstrap.closers,
		recorder:  bootstrap.metricsRecorder,
		composite: newReplyCompositeTracker(),
	}, nil
}

func assembleBootstrapRuntime(cfg config.Config, logger *slog.Logger) (bootstrapAssembly, error) {
	assembly := bootstrapAssembly{
		lifecycle: &Lifecycle{},
		closers:   make([]func() error, 0),
	}
	assembly.featureModules = defaultFeatureModules()

	metricsStore, metricsRecorder, err := initMetrics(cfg, logger)
	if err != nil {
		return bootstrapAssembly{}, fmt.Errorf("initialize metrics: %w", err)
	}
	assembly.metricsStore = metricsStore
	assembly.metricsRecorder = metricsRecorder
	appendMetricsStoreCloser(&assembly.closers, metricsStore)
	addMetricsRecorderLifecycle(assembly.lifecycle, metricsRecorder)

	catalog, err := buildFeatureCatalog(assembly.featureModules)
	if err != nil {
		return bootstrapAssembly{}, fmt.Errorf("build feature catalog: %w", err)
	}
	assembly.accessController = bot.NewAccessController(catalog, cfg.Access)
	registry := bot.NewRegistry(catalog)
	assembly.router = bot.NewRouter(cfg.Bot.CommandPrefix, assembly.accessController, registry, logger)
	assembly.router.SetMetricsRecorder(metricsRecorder)
	configureRouterMiddlewares(assembly.router, logger, metricsRecorder, assembly.lifecycle)

	accessManager, err := setupAccessRuntime(cfg, logger, assembly.accessController, &assembly.closers)
	if err != nil {
		return bootstrapAssembly{}, err
	}
	assembly.accessManager = accessManager
	return assembly, nil
}

func assembleFeatureModules(cfg config.Config, logger *slog.Logger, bootstrap *bootstrapAssembly) (featureAssembly, error) {
	ctx := &featureBuildContext{
		cfg:              cfg,
		logger:           logger,
		lifecycle:        bootstrap.lifecycle,
		recorder:         bootstrap.metricsRecorder,
		closers:          &bootstrap.closers,
		accessController: bootstrap.accessController,
		accessManager:    bootstrap.accessManager,
		router:           bootstrap.router,
		shared:           &featureSharedState{},
	}
	runtimes, smokeProbes, err := buildFeatureRuntimes(ctx, bootstrap.featureModules)
	if err != nil {
		return featureAssembly{}, err
	}
	return featureAssembly{
		runtimes:    runtimes,
		priceStore:  ctx.shared.coupangPriceStore,
		smokeProbes: smokeProbes,
	}, nil
}

func appendMetricsStoreCloser(closers *[]func() error, metricsStore *metrics.SQLiteStore) {
	if metricsStore == nil {
		return
	}
	*closers = append(*closers, metricsStore.Close)
}

func buildAdminHandler(
	accessController *bot.AccessController,
	accessManager *bot.AccessManager,
	autoQueryManager *bot.AutoQueryManager,
	intentIDs func() []string,
	logger *slog.Logger,
) bot.Handler {
	if accessManager == nil {
		return nil
	}
	return command.NewAdminHandler(accessController, accessManager, autoQueryManager, intentIDs, logger)
}

func attachAdapter(adapter transport.RuntimeAdapter, youtubeHandler *command.YouTubeHandler, urlSummaryHandler *command.URLSummaryHandler, closers *[]func() error) {
	if adapter == nil {
		return
	}
	*closers = append(*closers, adapter.Close)
	youtubeHandler.SetAdapter(adapter)
	urlSummaryHandler.SetAdapter(adapter)
}

func attachTransportMetrics(adapter transport.RuntimeAdapter, recorder metrics.Recorder) {
	if adapter == nil || recorder == nil {
		return
	}
	if configured, ok := adapter.(interface{ SetMetricsRecorder(metrics.Recorder) }); ok {
		configured.SetMetricsRecorder(recorder)
	}
}

func wireAdminRuntime(
	cfg config.Config,
	logger *slog.Logger,
	lifecycle *Lifecycle,
	metricsStore *metrics.SQLiteStore,
	priceStore store.PriceStore,
	router *bot.Router,
	smokeProbes []admin.CommandSmokeProbe,
) error {
	if !cfg.Admin.Enabled {
		return nil
	}
	var smokeRunner admin.CommandSmokeRunner
	if roomChatID := resolveSmokeRoomChatID(cfg); roomChatID != "" && router != nil && len(smokeProbes) > 0 {
		smokeRunner = commandSmokeRunner{roomChatID: roomChatID, router: router}
	}
	adminServer, err := admin.NewServer(cfg.Admin, metricsStore, buildFeatureStatsLoader(priceStore, cfg), smokeRunner, smokeProbes, logger)
	if err != nil {
		return fmt.Errorf("initialize admin server: %w", err)
	}
	lifecycle.Add("admin-server", func(ctx context.Context) error {
		return adminServer.Start(ctx)
	})
	return nil
}

func initStore(cfg config.Config) (store.Store, error) {
	switch cfg.Store.Driver {
	case "memory":
		return store.NewMemoryStore(), nil
	default:
		return nil, errors.New("unsupported store driver: " + cfg.Store.Driver)
	}
}

func autoQueryPolicyFromConfig(cfg config.AutoQueryPolicyConfig, catalog *intent.Catalog) bot.AutoQueryPolicy {
	return bot.AutoQueryPolicy{
		Mode:              bot.AutoQueryMode(strings.TrimSpace(cfg.Mode)),
		AllowedHandlers:   append([]string(nil), cfg.AllowedHandlers...),
		BudgetPerHour:     cfg.BudgetPerHour,
		CooldownWindow:    cfg.CooldownWindow,
		DegradationTarget: bot.AutoQueryMode(strings.TrimSpace(cfg.DegradationTarget)),
	}.Normalize(catalog)
}

func autoQueryRoomsFromConfig(rooms []config.AutoQueryRoomConfig, catalog *intent.Catalog) []bot.AutoQueryBootstrapRoom {
	if len(rooms) == 0 {
		return nil
	}

	out := make([]bot.AutoQueryBootstrapRoom, 0, len(rooms))
	for _, room := range rooms {
		chatID := strings.TrimSpace(room.ChatID)
		if chatID == "" {
			continue
		}
		out = append(out, bot.AutoQueryBootstrapRoom{
			ChatID: chatID,
			Policy: autoQueryPolicyFromConfig(config.AutoQueryPolicyConfig{
				Mode:              room.Mode,
				AllowedHandlers:   room.AllowedHandlers,
				BudgetPerHour:     room.BudgetPerHour,
				CooldownWindow:    room.CooldownWindow,
				DegradationTarget: room.DegradationTarget,
			}, catalog),
		})
	}
	return out
}

func initMetrics(cfg config.Config, logger *slog.Logger) (*metrics.SQLiteStore, *metrics.AsyncRecorder, error) {
	if !cfg.Admin.MetricsEnabled {
		return nil, nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Admin.MetricsDBPath), 0o755); err != nil {
		return nil, nil, err
	}
	metricsStore, err := metrics.NewSQLiteStore(cfg.Admin.MetricsDBPath)
	if err != nil {
		return nil, nil, err
	}
	recorder := metrics.NewAsyncRecorder(
		metricsStore,
		cfg.Admin.PseudonymSecret,
		roomAliasMap(cfg.Access.Rooms),
		cfg.Admin.FlushInterval,
		cfg.Admin.RollupInterval,
		metrics.RetentionPolicy{
			Raw:    cfg.Admin.RawRetention,
			Hourly: cfg.Admin.HourlyRetention,
			Daily:  cfg.Admin.DailyRetention,
			Error:  cfg.Admin.ErrorRetention,
		},
		logger,
	)
	return metricsStore, recorder, nil
}

func roomAliasMap(rooms []config.AccessRoomConfig) map[string]string {
	aliases := make(map[string]string, len(rooms))
	for _, room := range rooms {
		if strings.TrimSpace(room.ChatID) == "" {
			continue
		}
		aliases[room.ChatID] = room.Alias
	}
	return aliases
}

func buildFeatureStatsLoader(priceStore store.PriceStore, cfg config.Config) admin.FeatureStatsLoader {
	return func(ctx context.Context, _ time.Time, _ time.Time, _ string) (metrics.CoupangFeatureStats, error) {
		if priceStore == nil {
			return metrics.CoupangFeatureStats{}, nil
		}
		count, err := priceStore.CountTrackedProducts(ctx)
		if err != nil {
			return metrics.CoupangFeatureStats{}, err
		}
		products, err := priceStore.ListWatchedProducts(ctx, 365*24*time.Hour)
		if err != nil {
			return metrics.CoupangFeatureStats{}, err
		}
		var stale int64
		now := time.Now()
		for _, product := range products {
			if product.Snapshot.LastSeenAt.IsZero() || now.Sub(product.Snapshot.LastSeenAt) > cfg.Coupang.Freshness {
				stale++
			}
		}
		return metrics.CoupangFeatureStats{
			TrackedProducts: int64(count),
			StaleProducts:   stale,
			StaleRatio:      ratio(stale, int64(count)),
		}, nil
	}
}

func initTransportAdapter(cfg config.Config, logger *slog.Logger) (transport.RuntimeAdapter, error) {
	if cfg.HTTPTest.Enabled {
		logger.Info("http test transport adapter enabled", "addr", cfg.HTTPTest.Addr)
		return httptest.NewServer(cfg.HTTPTest, logger), nil
	}
	if !cfg.Iris.Enabled {
		logger.Info("kakao transport adapter disabled")
		return nil, nil
	}

	instances := cfg.Iris.ResolvedInstances()
	if len(instances) == 0 {
		logger.Info("kakao transport adapter disabled (no instances)")
		return nil, nil
	}

	if len(instances) == 1 {
		logger.Info("iris transport adapter enabled", "instance", instances[0].ID)
		return iris.NewClient(instances[0], logger), nil
	}

	adapters := make(map[string]transport.RuntimeAdapter, len(instances))
	for _, inst := range instances {
		adapters[inst.ID] = iris.NewClient(inst, logger)
		logger.Info("iris transport adapter registered", "instance", inst.ID)
	}
	return transport.NewCompositeAdapter(adapters)
}
