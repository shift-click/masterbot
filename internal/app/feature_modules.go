package app

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/shift-click/masterbot/internal/admin"
	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/command"
	"github.com/shift-click/masterbot/internal/commandmeta"
	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/intent"
	"github.com/shift-click/masterbot/internal/metrics"
	"github.com/shift-click/masterbot/internal/scraper"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/store"
	"github.com/shift-click/masterbot/internal/transport"
)

type featureRuntime struct {
	name          string
	handlers      []bot.Handler
	fallbacks     []bot.FallbackHandler
	attachAdapter func(transport.RuntimeAdapter)
	smokeProbes   []admin.CommandSmokeProbe
}

type featureModule interface {
	Name() string
	Order() int
	Descriptors() []commandmeta.Descriptor
	Build(*featureBuildContext) (featureRuntime, error)
}

type featureBuildContext struct {
	cfg              config.Config
	logger           *slog.Logger
	lifecycle        *Lifecycle
	recorder         metrics.Recorder
	closers          *[]func() error
	accessController *bot.AccessController
	accessManager    *bot.AccessManager
	router           *bot.Router
	shared           *featureSharedState
}

type featureSharedState struct {
	coin              *coinModule
	stock             *stockModule
	geminiClient      *providers.GeminiClient
	summaryExecutor   *command.SummaryExecutor
	coupangPriceStore store.PriceStore
}

type basicFeatureModule struct {
	name        string
	order       int
	descriptors []commandmeta.Descriptor
	build       func(*featureBuildContext) (featureRuntime, error)
}

func (m basicFeatureModule) Name() string { return m.name }
func (m basicFeatureModule) Order() int   { return m.order }
func (m basicFeatureModule) Descriptors() []commandmeta.Descriptor {
	return append([]commandmeta.Descriptor(nil), m.descriptors...)
}
func (m basicFeatureModule) Build(ctx *featureBuildContext) (featureRuntime, error) {
	return m.build(ctx)
}

func defaultFeatureModules() []featureModule {
	modules := []featureModule{
		basicFeatureModule{
			name:        "help-and-ai",
			order:       10,
			descriptors: []commandmeta.Descriptor{commandmeta.Must("help"), commandmeta.Must("ai")},
			build: func(ctx *featureBuildContext) (featureRuntime, error) {
				return featureRuntime{
					name: "help-and-ai",
					handlers: []bot.Handler{
						command.NewHelpHandler(ctx.router.VisibleEntries, ctx.router.Catalog().Resolve),
						command.NewAIHandler(),
					},
					smokeProbes: []admin.CommandSmokeProbe{
						{
							ID:          "help",
							Message:     "도움",
							ExpectTexts: []string{"사용 가능한 명령어"},
							ExpectType:  string(transport.ReplyTypeText),
						},
					},
				}, nil
			},
		},
		basicFeatureModule{
			name:        "gold",
			order:       20,
			descriptors: []commandmeta.Descriptor{commandmeta.Must("gold")},
			build: func(ctx *featureBuildContext) (featureRuntime, error) {
				return featureRuntime{
					name: "gold",
					handlers: []bot.Handler{
						command.NewGoldHandler(providers.NewNaverGold(ctx.logger), ctx.logger),
					},
				}, nil
			},
		},
		basicFeatureModule{
			name:        "market",
			order:       30,
			descriptors: []commandmeta.Descriptor{commandmeta.Must("coin"), commandmeta.Must("finance"), commandmeta.Must("forex-convert")},
			build: func(ctx *featureBuildContext) (featureRuntime, error) {
				module := setupCoinModule(ctx.logger, ctx.lifecycle)
				ctx.shared.coin = &module
				return featureRuntime{
					name: "market",
					handlers: []bot.Handler{
						command.NewForexHandler(module.dunamuForex),
						module.handler,
					},
					fallbacks: []bot.FallbackHandler{
						command.NewForexConvertHandler(module.dunamuForex),
					},
				}, nil
			},
		},
		basicFeatureModule{
			name:        "stock",
			order:       40,
			descriptors: []commandmeta.Descriptor{commandmeta.Must("stock")},
			build: func(ctx *featureBuildContext) (featureRuntime, error) {
				module := setupStockModule(ctx.cfg, ctx.logger, ctx.lifecycle)
				ctx.shared.stock = &module
				return featureRuntime{
					name:     "stock",
					handlers: []bot.Handler{module.handler},
				}, nil
			},
		},
		basicFeatureModule{
			name:        "index",
			order:       45,
			descriptors: []commandmeta.Descriptor{commandmeta.Must("index")},
			build: func(ctx *featureBuildContext) (featureRuntime, error) {
				if ctx.shared.stock == nil {
					return featureRuntime{}, fmt.Errorf("index module requires stock module")
				}
				return featureRuntime{
					name:     "index",
					handlers: []bot.Handler{command.NewIndexHandler(ctx.shared.stock.naverStock, ctx.logger)},
				}, nil
			},
		},
		basicFeatureModule{
			name:        "chart",
			order:       50,
			descriptors: []commandmeta.Descriptor{commandmeta.Must("chart")},
			build: func(ctx *featureBuildContext) (featureRuntime, error) {
				if ctx.shared.coin == nil || ctx.shared.stock == nil {
					return featureRuntime{}, fmt.Errorf("chart module requires coin and stock modules")
				}
				handler := command.NewChartHandler(command.ChartHandlerDeps{
					CoinResolver:  ctx.shared.coin.coinResolver,
					StockResolver: ctx.shared.stock.naverStock,
					BinanceOHLC:   providers.NewBinanceOHLC(ctx.logger),
					UpbitOHLC:     providers.NewUpbitOHLC(ctx.logger),
					StockOHLC:     providers.NewNaverStockOHLC(ctx.logger),
					DEXOHLC:       providers.NewGeckoTerminalOHLC(ctx.logger),
					RendererURL:   ctx.cfg.Chart.RendererURL,
					Logger:        ctx.logger,
				})
				return featureRuntime{
					name:     "chart",
					handlers: []bot.Handler{handler},
				}, nil
			},
		},
		basicFeatureModule{
			name:        "coupang",
			order:       60,
			descriptors: []commandmeta.Descriptor{commandmeta.Must("coupang")},
			build: func(ctx *featureBuildContext) (featureRuntime, error) {
				module, err := setupCoupangModule(ctx.cfg, ctx.logger, ctx.lifecycle, ctx.recorder, ctx.closers)
				if err != nil {
					return featureRuntime{}, err
				}
				ctx.shared.coupangPriceStore = module.priceStore
				return featureRuntime{
					name:     "coupang",
					handlers: []bot.Handler{module.handler},
				}, nil
			},
		},
		basicFeatureModule{
			name:        "sports",
			order:       70,
			descriptors: []commandmeta.Descriptor{commandmeta.Must("football"), commandmeta.Must("esports"), commandmeta.Must("baseball")},
			build: func(ctx *featureBuildContext) (featureRuntime, error) {
				sports := setupSportsModule(ctx.cfg, ctx.logger, ctx.lifecycle)
				baseball := setupBaseballModule(ctx.cfg, ctx.logger, ctx.lifecycle)
				return featureRuntime{
					name: "sports",
					handlers: []bot.Handler{
						sports.footballHandler,
						sports.esportsHandler,
						baseball,
					},
				}, nil
			},
		},
		basicFeatureModule{
			name:        "info",
			order:       80,
			descriptors: []commandmeta.Descriptor{commandmeta.Must("weather"), commandmeta.Must("trending"), commandmeta.Must("news")},
			build: func(ctx *featureBuildContext) (featureRuntime, error) {
				openMeteo := providers.NewOpenMeteo(ctx.logger)
				weatherCache := scraper.NewWeatherCache(ctx.cfg.Weather.CacheTTL, ctx.cfg.Weather.YesterdayCacheTTL)
				return featureRuntime{
					name: "info",
					handlers: []bot.Handler{
						command.NewWeatherCommand(openMeteo, weatherCache, ctx.logger),
						command.NewTrendingHandler(providers.NewGoogleTrends(ctx.logger)),
						command.NewNewsHandlerReal(providers.NewGoogleNews(ctx.logger)),
					},
				}, nil
			},
		},
		basicFeatureModule{
			name:        "admin",
			order:       90,
			descriptors: []commandmeta.Descriptor{commandmeta.Must("admin")},
			build: func(ctx *featureBuildContext) (featureRuntime, error) {
				return featureRuntime{
					name: "admin",
					handlers: []bot.Handler{
						buildAdminHandler(ctx.accessController, ctx.accessManager, nil, ctx.router.IntentIDs, ctx.logger),
					},
				}, nil
			},
		},
		basicFeatureModule{
			name:        "summary",
			order:       100,
			descriptors: []commandmeta.Descriptor{commandmeta.Must("youtube"), commandmeta.Must("url-summary")},
			build: func(ctx *featureBuildContext) (featureRuntime, error) {
				if ctx.shared.geminiClient == nil {
					client, err := setupGeminiClient(ctx.cfg, ctx.logger)
					if err != nil {
						return featureRuntime{}, err
					}
					ctx.shared.geminiClient = client
				}
				if ctx.shared.summaryExecutor == nil {
					exec := command.NewSummaryExecutor(ctx.logger, 3, 3*time.Minute)
					ctx.shared.summaryExecutor = exec
					ctx.lifecycle.Add("summary-executor", func(inner context.Context) error {
						return exec.Run(inner)
					})
				}
				youtube := command.NewYouTubeHandler(ctx.shared.geminiClient, ctx.logger)
				urlSummary := command.NewURLSummaryHandler(ctx.shared.geminiClient, ctx.logger)
				youtube.SetExecutor(ctx.shared.summaryExecutor)
				urlSummary.SetExecutor(ctx.shared.summaryExecutor)
				return featureRuntime{
					name:     "summary",
					handlers: []bot.Handler{youtube, urlSummary},
					attachAdapter: func(adapter transport.RuntimeAdapter) {
						youtube.SetAdapter(adapter)
						urlSummary.SetAdapter(adapter)
					},
				}, nil
			},
		},
		basicFeatureModule{
			name:        "utility-fallbacks",
			order:       110,
			descriptors: []commandmeta.Descriptor{commandmeta.Must("calc")},
			build: func(ctx *featureBuildContext) (featureRuntime, error) {
				return featureRuntime{
					name:      "utility-fallbacks",
					fallbacks: []bot.FallbackHandler{command.NewCalcHandler()},
					smokeProbes: []admin.CommandSmokeProbe{
						{
							ID:          "calc",
							Message:     "100*2",
							ExpectTexts: []string{"200"},
							ExpectType:  string(transport.ReplyTypeText),
						},
					},
				}, nil
			},
		},
	}

	slices.SortFunc(modules, func(a, b featureModule) int {
		return a.Order() - b.Order()
	})
	return modules
}

func buildFeatureCatalog(modules []featureModule) (*intent.Catalog, error) {
	entries := make([]intent.Entry, 0, 24)
	for _, module := range modules {
		for _, descriptor := range module.Descriptors() {
			entries = append(entries, intent.Entry{
				ID:              descriptor.ID,
				Name:            descriptor.Name,
				Description:     descriptor.Description,
				SlashAliases:    append([]string(nil), descriptor.SlashAliases...),
				ExplicitAliases: append([]string(nil), descriptor.ExplicitAliases...),
				NormalizeKeys:   append([]string(nil), descriptor.NormalizeKeys...),
				FallbackScope:   descriptor.FallbackScope,
				AllowAutoQuery:  descriptor.AllowAutoQuery,
				ACLExempt:       descriptor.ACLExempt,
				Example:         descriptor.Example,
				Category:        descriptor.Category,
				HelpVisible:     descriptor.HelpVisible,
			})
		}
	}
	return intent.NewCatalog(entries)
}

func buildFeatureRuntimes(ctx *featureBuildContext, modules []featureModule) ([]featureRuntime, []admin.CommandSmokeProbe, error) {
	runtimes := make([]featureRuntime, 0, len(modules))
	probes := make([]admin.CommandSmokeProbe, 0, 8)
	for _, module := range modules {
		runtime, err := module.Build(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("build feature module %q: %w", module.Name(), err)
		}
		runtimes = append(runtimes, runtime)
		probes = append(probes, runtime.smokeProbes...)
	}
	return runtimes, probes, nil
}

func attachAdapters(adapter transport.RuntimeAdapter, runtimes []featureRuntime, closers *[]func() error) {
	if adapter == nil {
		return
	}
	*closers = append(*closers, adapter.Close)
	for _, runtime := range runtimes {
		if runtime.attachAdapter != nil {
			runtime.attachAdapter(adapter)
		}
	}
}

type commandSmokeRunner struct {
	roomChatID string
	router     *bot.Router
}

const adminSmokePrincipal = "admin-smoke"

func (r commandSmokeRunner) Run(ctx context.Context, probes []admin.CommandSmokeProbe) ([]admin.CommandSmokeResult, error) {
	results := make([]admin.CommandSmokeResult, 0, len(probes))
	for _, probe := range probes {
		result := admin.CommandSmokeResult{ID: probe.ID, Message: probe.Message}
		var replies []bot.Reply
		err := r.router.Dispatch(ctx, transport.Message{
			Msg: probe.Message,
			Raw: transport.RawChatLog{
				ID:      "smoke-" + probe.ID,
				ChatID:  r.roomChatID,
				UserID:  adminSmokePrincipal,
				Message: probe.Message,
			},
			Room:   adminSmokePrincipal,
			Sender: adminSmokePrincipal,
		}, func(_ context.Context, reply bot.Reply) error {
			replies = append(replies, reply)
			return nil
		})
		if err != nil {
			result.OK = false
			result.Error = err.Error()
			results = append(results, result)
			continue
		}
		result.OK = commandSmokeMatchesExpectation(replies, probe)
		result.ReplyCount = len(replies)
		if len(replies) > 0 {
			result.Replies = repliesToStrings(replies)
		}
		if !result.OK {
			result.Error = "probe expectation mismatch"
		}
		results = append(results, result)
	}
	return results, nil
}

func resolveSmokeRoomChatID(cfg config.Config) string {
	if room := cfg.Admin.SmokeRoomChatID; room != "" {
		return room
	}
	if room := cfg.Access.BootstrapAdminRoomChatID; room != "" {
		return room
	}
	if len(cfg.Access.Rooms) > 0 {
		return cfg.Access.Rooms[0].ChatID
	}
	return ""
}

func commandSmokeMatchesExpectation(replies []bot.Reply, probe admin.CommandSmokeProbe) bool {
	if len(replies) == 0 {
		return false
	}
	matchedType := probe.ExpectType == "" || string(replies[0].Type) == probe.ExpectType
	if !matchedType {
		return false
	}
	text := repliesToStrings(replies)
	for _, expected := range probe.ExpectTexts {
		found := false
		for _, replyText := range text {
			if replyText == "" {
				continue
			}
			if containsText(replyText, expected) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func containsText(haystack, needle string) bool {
	return needle == "" || strings.Contains(haystack, needle)
}

func repliesToStrings(replies []bot.Reply) []string {
	out := make([]string, 0, len(replies))
	for _, reply := range replies {
		switch reply.Type {
		case transport.ReplyTypeText:
			out = append(out, reply.Text)
		case transport.ReplyTypeImage:
			out = append(out, "<image>")
		case transport.ReplyTypeImageMultiple:
			out = append(out, "<image_multiple>")
		default:
			out = append(out, "")
		}
	}
	return out
}
