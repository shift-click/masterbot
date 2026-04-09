package app

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/command"
	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/coupang"
	"github.com/shift-click/masterbot/internal/intent"
	"github.com/shift-click/masterbot/internal/metrics"
	"github.com/shift-click/masterbot/internal/transport"
)

type fakeAdapter struct {
	replyRequests []transport.ReplyRequest
}

func (f *fakeAdapter) Start(context.Context, func(context.Context, transport.Message) error) error {
	return nil
}
func (f *fakeAdapter) Close() error { return nil }
func (f *fakeAdapter) Reply(_ context.Context, req transport.ReplyRequest) error {
	f.replyRequests = append(f.replyRequests, req)
	return nil
}

type fakeCoupangPriceStore struct {
	count    int
	products []coupang.CoupangProductRecord
}

type appRecorder struct {
	events []metrics.Event
}

func (r *appRecorder) Record(_ context.Context, event metrics.Event) {
	r.events = append(r.events, event)
}

func (f *fakeCoupangPriceStore) UpsertProduct(context.Context, coupang.CoupangProductRecord) error {
	return nil
}
func (f *fakeCoupangPriceStore) UpdateProductMetadata(context.Context, coupang.CoupangProductRecord) error {
	return nil
}
func (f *fakeCoupangPriceStore) GetProduct(context.Context, string) (*coupang.CoupangProductRecord, error) {
	return nil, nil
}
func (f *fakeCoupangPriceStore) GetProductByBaseProductID(context.Context, string) (*coupang.CoupangProductRecord, error) {
	return nil, nil
}
func (f *fakeCoupangPriceStore) GetSourceMapping(context.Context, string) (*coupang.CoupangSourceMapping, error) {
	return nil, nil
}
func (f *fakeCoupangPriceStore) UpsertSourceMapping(context.Context, coupang.CoupangSourceMapping) error {
	return nil
}
func (f *fakeCoupangPriceStore) MarkSourceMappingState(context.Context, string, coupang.CoupangSourceMappingState, string) error {
	return nil
}
func (f *fakeCoupangPriceStore) TouchProduct(context.Context, string, time.Duration) error {
	return nil
}
func (f *fakeCoupangPriceStore) ListWatchedProducts(context.Context, time.Duration) ([]coupang.CoupangProductRecord, error) {
	return append([]coupang.CoupangProductRecord(nil), f.products...), nil
}
func (f *fakeCoupangPriceStore) EvictStaleProducts(context.Context, time.Duration) (int, error) {
	return 0, nil
}
func (f *fakeCoupangPriceStore) DeleteProducts(context.Context, []string) (int, error) { return 0, nil }
func (f *fakeCoupangPriceStore) CountTrackedProducts(context.Context) (int, error) {
	return f.count, nil
}
func (f *fakeCoupangPriceStore) InsertPrice(context.Context, string, int, bool) error   { return nil }
func (f *fakeCoupangPriceStore) InsertSeedPrices(context.Context, string, []int) error  { return nil }
func (f *fakeCoupangPriceStore) ReplaceSeedPrices(context.Context, string, []int) error { return nil }
func (f *fakeCoupangPriceStore) HasSeedPrices(context.Context, string) (bool, error) {
	return false, nil
}
func (f *fakeCoupangPriceStore) MarkChartBackfillAt(context.Context, string, time.Time) error {
	return nil
}
func (f *fakeCoupangPriceStore) GetPriceHistory(context.Context, string, time.Time) ([]coupang.PricePoint, error) {
	return nil, nil
}
func (f *fakeCoupangPriceStore) GetPriceStats(context.Context, string) (*coupang.PriceStats, error) {
	return nil, nil
}
func (f *fakeCoupangPriceStore) UpdateSnapshot(context.Context, coupang.CoupangSnapshot) error {
	return nil
}
func (f *fakeCoupangPriceStore) MarkRefreshState(context.Context, string, time.Time, bool) error {
	return nil
}
func (f *fakeCoupangPriceStore) SetProductTier(context.Context, string, coupang.CoupangRefreshTier) error {
	return nil
}
func (f *fakeCoupangPriceStore) Close() error { return nil }

func TestReplyDataByType(t *testing.T) {
	t.Parallel()

	text := replyData(bot.Reply{Type: transport.ReplyTypeText, Text: "hello"})
	if text.(string) != "hello" {
		t.Fatalf("text reply = %v", text)
	}

	image := replyData(bot.Reply{Type: transport.ReplyTypeImage, ImageBase64: "img"})
	if image.(string) != "img" {
		t.Fatalf("image reply = %v", image)
	}

	images := replyData(bot.Reply{Type: transport.ReplyTypeImageMultiple, Images: []string{"a", "b"}})
	if len(images.([]string)) != 2 {
		t.Fatalf("images reply = %v", images)
	}
}

func TestClassifyReplyError(t *testing.T) {
	t.Parallel()

	if got := classifyReplyError(nil); got != "" {
		t.Fatalf("nil classify = %q", got)
	}
	if got := classifyReplyError(context.DeadlineExceeded); got != "deadline_exceeded" {
		t.Fatalf("deadline classify = %q", got)
	}
	if got := classifyReplyError(context.Canceled); got != "canceled" {
		t.Fatalf("canceled classify = %q", got)
	}
	if got := classifyReplyError(errors.New("x")); got != "reply_error" {
		t.Fatalf("default classify = %q", got)
	}
}

func TestRatio(t *testing.T) {
	t.Parallel()

	if got := ratio(1, 0); got != 0 {
		t.Fatalf("ratio divide-by-zero = %v", got)
	}
	if got := ratio(2, 4); got != 0.5 {
		t.Fatalf("ratio(2,4) = %v", got)
	}
}

func TestRoomAliasMapAndAutoQueryRooms(t *testing.T) {
	t.Parallel()

	aliases := roomAliasMap([]config.AccessRoomConfig{
		{ChatID: "room-1", Alias: "운영방"},
		{ChatID: "", Alias: "무시"},
	})
	if aliases["room-1"] != "운영방" {
		t.Fatalf("aliases = %+v", aliases)
	}
	if _, ok := aliases[""]; ok {
		t.Fatalf("unexpected empty chat id alias: %+v", aliases)
	}

	catalog := intent.DefaultCatalog()
	rooms := autoQueryRoomsFromConfig([]config.AutoQueryRoomConfig{
		{ChatID: " room-2 ", Mode: "explicit-only", AllowedHandlers: []string{"coin"}, BudgetPerHour: 5, CooldownWindow: time.Minute},
		{ChatID: " ", Mode: "off"},
	}, catalog)
	if len(rooms) != 1 {
		t.Fatalf("rooms len = %d", len(rooms))
	}
	if rooms[0].ChatID != "room-2" {
		t.Fatalf("chat id = %q", rooms[0].ChatID)
	}
}

func TestWaitLifecycleAndCloseAll(t *testing.T) {
	t.Parallel()

	run := &lifecycleRun{
		errCh:  make(chan error, 1),
		doneCh: make(chan struct{}),
	}
	run.errCh <- errors.New("boom")
	close(run.doneCh)
	close(run.errCh)
	if err := waitLifecycle(context.Background(), func() {}, run); err == nil {
		t.Fatal("expected lifecycle error")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := closeAll(ctx, []func() error{
		func() error { return errors.New("first") },
		func() error { return nil },
	}); err == nil {
		t.Fatal("expected closeAll first error")
	}
}

func TestAttachAdapterAndRecordReplyMetric(t *testing.T) {
	t.Parallel()

	youtube := commandYouTubeHandlerForTest()
	adapter := &fakeAdapter{}
	closers := make([]func() error, 0)

	urlSummary := command.NewURLSummaryHandler(nil, slog.Default())
	attachAdapter(adapter, youtube, urlSummary, &closers)
	if len(closers) != 1 {
		t.Fatalf("closers len = %d", len(closers))
	}

	store, err := metrics.NewSQLiteStore(filepath.Join(t.TempDir(), "metrics.db"))
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	recorder := metrics.NewAsyncRecorder(
		store,
		"secret",
		nil,
		10*time.Millisecond,
		50*time.Millisecond,
		metrics.RetentionPolicy{Raw: time.Hour, Hourly: time.Hour, Daily: time.Hour, Error: time.Hour},
		nil,
	)
	ctx, cancel := context.WithCancel(context.Background())
	recorder.Start(ctx)
	defer func() {
		cancel()
		recorder.Wait()
	}()

	app := &App{recorder: recorder}
	msg := transport.Message{Room: "운영방", Raw: transport.RawChatLog{ID: "r1", ChatID: "room-1", UserID: "u1"}}
	reply := bot.Reply{Type: transport.ReplyTypeText, Text: "ok"}
	app.recordReplyMetric(context.Background(), msg, reply, time.Now().Add(-10*time.Millisecond), nil)
	app.recordReplyMetric(context.Background(), msg, reply, time.Now().Add(-10*time.Millisecond), context.DeadlineExceeded)
}

func TestInitMetricsAndFeatureLoaderNilStore(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Admin.MetricsEnabled = false
	store, recorder, err := initMetrics(cfg, NewLogger("error"))
	if err != nil {
		t.Fatalf("initMetrics: %v", err)
	}
	if store != nil || recorder != nil {
		t.Fatalf("expected nil metrics components, got store=%v recorder=%v", store, recorder)
	}

	loader := buildFeatureStatsLoader(nil, cfg)
	stats, err := loader(context.Background(), time.Now(), time.Now(), "")
	if err != nil {
		t.Fatalf("feature stats loader: %v", err)
	}
	if stats.TrackedProducts != 0 || stats.StaleRatio != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestAppendMetricsStoreCloserAndBuildAdminHandlerNil(t *testing.T) {
	t.Parallel()

	closers := make([]func() error, 0)
	appendMetricsStoreCloser(&closers, nil)
	if len(closers) != 0 {
		t.Fatalf("closers should remain empty: %d", len(closers))
	}

	adminHandler := buildAdminHandler(nil, nil, nil, nil, NewLogger("error"))
	if adminHandler != nil {
		t.Fatal("expected nil admin handler when access manager is nil")
	}
}

func TestWireAdminRuntimeIsolatedFromCoreAssembly(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	logger := NewLogger("error")
	store, err := metrics.NewSQLiteStore(filepath.Join(t.TempDir(), "admin-metrics.db"))
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	disabledLifecycle := &Lifecycle{}
	if err := wireAdminRuntime(cfg, logger, disabledLifecycle, store, nil, nil, nil); err != nil {
		t.Fatalf("wireAdminRuntime disabled: %v", err)
	}
	if got := len(disabledLifecycle.components); got != 0 {
		t.Fatalf("disabled admin lifecycle components = %d, want 0", got)
	}

	cfg.Admin.Enabled = true
	cfg.Admin.MetricsEnabled = true
	cfg.Admin.AllowedEmails = []string{"ops@example.com"}

	enabledLifecycle := &Lifecycle{}
	if err := wireAdminRuntime(cfg, logger, enabledLifecycle, store, nil, nil, nil); err != nil {
		t.Fatalf("wireAdminRuntime enabled: %v", err)
	}
	if got := len(enabledLifecycle.components); got != 1 {
		t.Fatalf("enabled admin lifecycle components = %d, want 1", got)
	}
	if enabledLifecycle.components[0].name != "admin-server" {
		t.Fatalf("admin lifecycle component = %q", enabledLifecycle.components[0].name)
	}
}

func TestAssembleBootstrapRuntimeAndFeatureModules(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Iris.Enabled = false
	cfg.Coupang.DBPath = filepath.Join(t.TempDir(), "coupang.db")
	cfg.Lotto.DBPath = filepath.Join(t.TempDir(), "lotto.db")
	cfg.Access.RuntimeDBPath = filepath.Join(t.TempDir(), "access.db")
	cfg.Access.BootstrapAdminRoomChatID = "100"
	cfg.Access.BootstrapAdminUserID = "200"

	bootstrap, err := assembleBootstrapRuntime(cfg, NewLogger("error"))
	if err != nil {
		t.Fatalf("assembleBootstrapRuntime: %v", err)
	}
	if bootstrap.lifecycle == nil || bootstrap.router == nil || bootstrap.accessController == nil {
		t.Fatalf("unexpected bootstrap assembly: %+v", bootstrap)
	}

	features, err := assembleFeatureModules(cfg, NewLogger("error"), &bootstrap)
	if err != nil {
		t.Fatalf("assembleFeatureModules: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = closeAll(ctx, bootstrap.closers)
	})

	runtimes := map[string]featureRuntime{}
	for _, runtime := range features.runtimes {
		runtimes[runtime.name] = runtime
	}
	for _, name := range []string{"stock", "market", "coupang", "summary"} {
		runtime, ok := runtimes[name]
		if !ok {
			t.Fatalf("missing feature runtime %q", name)
		}
		if len(runtime.handlers) == 0 && len(runtime.fallbacks) == 0 {
			t.Fatalf("feature runtime %q has no registrations", name)
		}
	}
	if runtimes["summary"].attachAdapter == nil {
		t.Fatal("expected summary runtime to wire adapter attachment")
	}
	if features.priceStore == nil {
		t.Fatal("expected coupang price store to be initialized")
	}
}

func TestBuildWithNilLoggerInitializesApplication(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Iris.Enabled = false
	cfg.Admin.Enabled = false
	cfg.Admin.MetricsEnabled = false
	cfg.Coupang.DBPath = filepath.Join(t.TempDir(), "coupang.db")
	cfg.Lotto.DBPath = filepath.Join(t.TempDir(), "lotto.db")
	cfg.Access.RuntimeDBPath = filepath.Join(t.TempDir(), "access.db")
	cfg.Access.BootstrapAdminRoomChatID = "100"
	cfg.Access.BootstrapAdminUserID = "200"

	app, err := Build(cfg, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = closeAll(ctx, app.closers)
	})

	if app.logger == nil || app.router == nil || app.lifecycle == nil {
		t.Fatalf("unexpected app wiring: %+v", app)
	}
	if app.adapter != nil {
		t.Fatalf("expected transport adapter to remain disabled, got %T", app.adapter)
	}
	if app.composite == nil {
		t.Fatal("expected composite tracker to be initialized")
	}
}

func TestSetupHelpersDisabledPaths(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Access.RuntimeDBPath = ""
	cfg.Gemini.APIKey = ""
	cfg.Gemini.Model = ""
	closers := make([]func() error, 0)

	manager, err := setupAccessRuntime(cfg, NewLogger("error"), bot.NewAccessController(intent.DefaultCatalog(), cfg.Access), &closers)
	if err != nil {
		t.Fatalf("setupAccessRuntime disabled: %v", err)
	}
	if manager != nil {
		t.Fatalf("expected nil access manager, got %T", manager)
	}
	if len(closers) != 0 {
		t.Fatalf("unexpected closers: %d", len(closers))
	}

	geminiClient, err := setupGeminiClient(cfg, NewLogger("error"))
	if err != nil {
		t.Fatalf("setupGeminiClient disabled: %v", err)
	}
	if geminiClient != nil {
		t.Fatalf("expected nil gemini client, got %T", geminiClient)
	}
}

func TestKoreaLocationReturnsSeoulLocation(t *testing.T) {
	t.Parallel()

	loc := koreaLocation()
	if loc == nil {
		t.Fatal("expected korea location to be resolved")
	}
	if loc.String() != "Asia/Seoul" && loc.String() != "KST" {
		t.Fatalf("unexpected korea location: %s", loc)
	}
}

func TestRecordReplyMetricKeepsTransportBoundaryFields(t *testing.T) {
	t.Parallel()

	recorder := &appRecorder{}
	app := &App{recorder: recorder}
	msg := transport.Message{
		Room: "운영방",
		Raw:  transport.RawChatLog{ID: "req-reply", ChatID: "room-9", UserID: "user-9"},
	}
	reply := bot.Reply{
		Type: transport.ReplyTypeText,
		Text: "ok",
		Metadata: map[string]any{
			"request_correlation_id": "reply:req-reply",
			"reply_part":             "text",
		},
	}

	app.recordReplyMetric(context.Background(), msg, reply, time.Now().Add(-5*time.Millisecond), nil)
	if len(recorder.events) != 1 {
		t.Fatalf("event count = %d, want 1", len(recorder.events))
	}
	event := recorder.events[0]
	if event.EventName != metrics.EventReplySent {
		t.Fatalf("event name = %s", event.EventName)
	}
	if event.FeatureKey != "reply" || event.Attribution != "transport_reply" {
		t.Fatalf("unexpected reply boundary fields: %+v", event)
	}
	if event.RequestID != "req-reply" || event.RawRoomID != "room-9" || event.RawUserID != "user-9" {
		t.Fatalf("unexpected request routing fields: %+v", event)
	}
}

func TestInitMetricsAndFeatureStatsLoaderWithStore(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Admin.MetricsEnabled = true
	cfg.Admin.MetricsDBPath = filepath.Join(t.TempDir(), "metrics.db")

	store, recorder, err := initMetrics(cfg, NewLogger("error"))
	if err != nil {
		t.Fatalf("initMetrics: %v", err)
	}
	if store == nil || recorder == nil {
		t.Fatalf("expected metrics components, got store=%v recorder=%v", store, recorder)
	}
	t.Cleanup(func() { _ = store.Close() })

	priceStore := &fakeCoupangPriceStore{
		count: 2,
		products: []coupang.CoupangProductRecord{
			{Snapshot: coupang.CoupangSnapshot{LastSeenAt: time.Now().Add(-2 * cfg.Coupang.Freshness)}},
			{Snapshot: coupang.CoupangSnapshot{LastSeenAt: time.Now()}},
		},
	}
	loader := buildFeatureStatsLoader(priceStore, cfg)
	stats, err := loader(context.Background(), time.Now(), time.Now(), "")
	if err != nil {
		t.Fatalf("feature stats loader: %v", err)
	}
	if stats.TrackedProducts != 2 || stats.StaleProducts != 1 || stats.StaleRatio != 0.5 {
		t.Fatalf("unexpected feature stats: %+v", stats)
	}
}

type addrOnlyAdapter struct {
	transport.RuntimeAdapter
	addr string
}

func (a addrOnlyAdapter) Addr() string { return a.addr }

func TestTransportAddrAndAutoQueryManagerSetup(t *testing.T) {
	t.Parallel()

	app := &App{adapter: addrOnlyAdapter{addr: "127.0.0.1:9999"}}
	if got := app.TransportAddr(); got != "127.0.0.1:9999" {
		t.Fatalf("TransportAddr() = %q", got)
	}
	app.adapter = nil
	if got := app.TransportAddr(); got != "" {
		t.Fatalf("TransportAddr() without addr provider = %q", got)
	}

	cfg := config.Default()
	cfg.Store.Driver = "memory"
	cfg.AutoQuery.DefaultPolicy.Mode = "allow"
	cfg.AutoQuery.Rooms = []config.AutoQueryRoomConfig{
		{ChatID: "room-1", Mode: "allow", AllowedHandlers: []string{"weather"}},
	}
	stateStore, err := initStore(cfg)
	if err != nil {
		t.Fatalf("initStore: %v", err)
	}
	manager, err := initAutoQueryManager(cfg, intent.DefaultCatalog(), stateStore)
	if err != nil {
		t.Fatalf("initAutoQueryManager: %v", err)
	}
	if manager == nil {
		t.Fatal("expected auto query manager")
	}
}

func commandYouTubeHandlerForTest() *command.YouTubeHandler {
	return command.NewYouTubeHandler(nil, nil)
}
