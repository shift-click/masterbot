package app

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/config"
)

func TestInitTransportAdapterReturnsNilWhenDisabled(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Iris.Enabled = false

	adapter, err := initTransportAdapter(cfg, NewLogger("error"))
	if err != nil {
		t.Fatalf("initTransportAdapter: %v", err)
	}
	if adapter != nil {
		t.Fatal("expected nil adapter when kakao transport is disabled")
	}
}

func TestInitTransportAdapterReturnsIrisClientWhenEnabled(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Iris.Enabled = true

	adapter, err := initTransportAdapter(cfg, NewLogger("error"))
	if err != nil {
		t.Fatalf("initTransportAdapter: %v", err)
	}
	if adapter == nil {
		t.Fatal("expected non-nil adapter when kakao transport is enabled")
	}
}

func TestInitStoreReturnsMemoryStoreWhenConfigured(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Store.Driver = "memory"

	store, err := initStore(cfg)
	if err != nil {
		t.Fatalf("initStore: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestInitStoreRejectsUnsupportedDriver(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Store.Driver = "supabase"

	store, err := initStore(cfg)
	if err == nil {
		t.Fatal("expected unsupported driver error")
	}
	if store != nil {
		t.Fatal("expected nil store for unsupported driver")
	}
}

func TestCloseAllHonorsTimeout(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := closeAll(ctx, []func() error{
		func() error {
			<-ctx.Done()
			return nil
		},
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("closeAll() error = %v, want deadline exceeded", err)
	}
}

func TestAppRunClosesResourcesOnContextCancel(t *testing.T) {
	t.Parallel()

	var closed atomic.Bool

	app := &App{
		cfg:       config.Default(),
		logger:    NewLogger("error"),
		lifecycle: &Lifecycle{},
		closers: []func() error{
			func() error {
				closed.Store(true)
				return nil
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if !closed.Load() {
		t.Fatal("expected closer to run on shutdown")
	}
}

func TestAppRunWaitsForLifecycleBeforeClosingResources(t *testing.T) {
	t.Parallel()

	var lifecycleStopped atomic.Bool
	var closerSawStopped atomic.Bool

	app := &App{
		cfg:    config.Default(),
		logger: NewLogger("error"),
		lifecycle: &Lifecycle{
			components: []component{{
				name: "blocking",
				run: func(ctx context.Context) error {
					<-ctx.Done()
					time.Sleep(20 * time.Millisecond)
					lifecycleStopped.Store(true)
					return nil
				},
			}},
		},
		closers: []func() error{
			func() error {
				closerSawStopped.Store(lifecycleStopped.Load())
				return nil
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if !closerSawStopped.Load() {
		t.Fatal("expected closer to run after lifecycle completion")
	}
}

func TestBuildCreatesAppWithoutTransportAdapterWhenDisabled(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Iris.Enabled = false
	cfg.Coupang.DBPath = filepath.Join(t.TempDir(), "coupang-disabled.db")
	cfg.Access.RuntimeDBPath = filepath.Join(t.TempDir(), "access-disabled.db")
	cfg.Access.BootstrapAdminRoomChatID = "100"
	cfg.Access.BootstrapAdminUserID = "200"

	app, err := Build(cfg, NewLogger("error"))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = closeAll(ctx, app.closers)
	})
	if app.adapter != nil {
		t.Fatal("expected nil adapter when transport is disabled")
	}
}

func TestBuildCreatesAppWithTransportAdapterWhenEnabled(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Iris.Enabled = true
	cfg.Coupang.DBPath = filepath.Join(t.TempDir(), "coupang-enabled.db")
	cfg.Access.RuntimeDBPath = filepath.Join(t.TempDir(), "access-enabled.db")
	cfg.Access.BootstrapAdminRoomChatID = "100"
	cfg.Access.BootstrapAdminUserID = "200"

	app, err := Build(cfg, NewLogger("error"))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = closeAll(ctx, app.closers)
	})
	if app.adapter == nil {
		t.Fatal("expected non-nil adapter when transport is enabled")
	}
}
