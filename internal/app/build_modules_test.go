package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/intent"
	"github.com/shift-click/masterbot/internal/metrics"
)

func TestAddMetricsRecorderLifecycle(t *testing.T) {
	t.Parallel()

	lifecycle := &Lifecycle{}
	addMetricsRecorderLifecycle(lifecycle, nil)
	if len(lifecycle.components) != 0 {
		t.Fatalf("unexpected nil-recorder components: %d", len(lifecycle.components))
	}

	store, err := metrics.NewSQLiteStore(filepath.Join(t.TempDir(), "metrics.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	recorder := metrics.NewAsyncRecorder(
		store,
		"secret",
		nil,
		time.Millisecond,
		time.Millisecond,
		metrics.RetentionPolicy{
			Raw:    time.Hour,
			Hourly: 24 * time.Hour,
			Daily:  7 * 24 * time.Hour,
			Error:  7 * 24 * time.Hour,
		},
		NewLogger("error"),
	)
	addMetricsRecorderLifecycle(lifecycle, recorder)
	if len(lifecycle.components) != 1 {
		t.Fatalf("component count = %d, want 1", len(lifecycle.components))
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- lifecycle.components[0].run(ctx)
	}()
	recorder.Record(context.Background(), metrics.Event{
		EventName: metrics.EventReplySent,
		RequestID: "req-1",
	})
	time.Sleep(5 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("metrics lifecycle run: %v", err)
	}
}

func TestSetupAccessRuntimeEnabled(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Access.RuntimeDBPath = filepath.Join(t.TempDir(), "access.db")
	cfg.Access.BootstrapAdminRoomChatID = "room-admin"
	cfg.Access.BootstrapAdminUserID = "user-admin"
	closers := make([]func() error, 0)

	manager, err := setupAccessRuntime(
		cfg,
		NewLogger("error"),
		bot.NewAccessController(intent.DefaultCatalog(), cfg.Access),
		&closers,
	)
	if err != nil {
		t.Fatalf("setupAccessRuntime: %v", err)
	}
	if manager == nil {
		t.Fatal("expected access manager")
	}
	if len(closers) != 1 {
		t.Fatalf("closer count = %d, want 1", len(closers))
	}
}

func TestSetupGeminiClientEnabled(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Gemini.APIKey = "test-key"
	cfg.Gemini.Model = "gemini-2.0-flash"

	client, err := setupGeminiClient(cfg, NewLogger("error"))
	if err != nil {
		t.Fatalf("setupGeminiClient: %v", err)
	}
	if client == nil {
		t.Fatal("expected gemini client")
	}
}

func TestSetupCoinAndCoupangModulesRegisterLifecycle(t *testing.T) {
	t.Parallel()

	logger := NewLogger("error")
	lifecycle := &Lifecycle{}

	coin := setupCoinModule(logger, lifecycle)
	if coin.handler == nil || coin.dunamuForex == nil {
		t.Fatalf("unexpected coin module: %+v", coin)
	}
	if len(lifecycle.components) != 7 {
		t.Fatalf("coin lifecycle component count = %d, want 7", len(lifecycle.components))
	}

	cfg := config.Default()
	cfg.Coupang.DBPath = filepath.Join(t.TempDir(), "coupang.db")
	closers := make([]func() error, 0)
	coupangModule, err := setupCoupangModule(cfg, logger, lifecycle, metrics.NoopRecorder{}, &closers)
	if err != nil {
		t.Fatalf("setupCoupangModule: %v", err)
	}
	if coupangModule.handler == nil || coupangModule.priceStore == nil {
		t.Fatalf("unexpected coupang module: %+v", coupangModule)
	}
	if len(closers) != 1 {
		t.Fatalf("coupang closer count = %d, want 1", len(closers))
	}
	if len(lifecycle.components) != 9 {
		t.Fatalf("total lifecycle component count = %d, want 9", len(lifecycle.components))
	}

	for i, component := range lifecycle.components {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := component.run(ctx); err != nil {
			t.Fatalf("component[%d]=%s run: %v", i, component.name, err)
		}
	}
}
