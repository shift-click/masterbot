package scraper

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdaptiveIntervalPlannerCurrent(t *testing.T) {
	t.Parallel()

	config := SportsPollerConfig{
		LiveInterval:     time.Second,
		MatchDayInterval: 2 * time.Second,
		IdleDayInterval:  3 * time.Second,
	}

	livePlanner := adaptiveIntervalPlanner[string]{
		config:  config,
		leagues: []string{"live", "idle"},
		hasLive: func(league, _ string) bool { return league == "live" },
		classifyMatchDay: func(string, string) matchDayDecision {
			return matchDayDecision{}
		},
	}
	if got := livePlanner.current("20260320"); got != config.LiveInterval {
		t.Fatalf("live planner interval = %v, want %v", got, config.LiveInterval)
	}

	matchPlanner := adaptiveIntervalPlanner[string]{
		config:  config,
		leagues: []string{"match"},
		hasLive: func(string, string) bool { return false },
		classifyMatchDay: func(string, string) matchDayDecision {
			return matchDayDecision{matchDay: true}
		},
	}
	if got := matchPlanner.current("20260320"); got != config.MatchDayInterval {
		t.Fatalf("match planner interval = %v, want %v", got, config.MatchDayInterval)
	}

	idlePlanner := adaptiveIntervalPlanner[string]{
		config:  config,
		leagues: []string{"idle"},
		hasLive: func(string, string) bool { return false },
		classifyMatchDay: func(string, string) matchDayDecision {
			return matchDayDecision{}
		},
	}
	if got := idlePlanner.current("20260320"); got != config.IdleDayInterval {
		t.Fatalf("idle planner interval = %v, want %v", got, config.IdleDayInterval)
	}
}

func TestAdaptivePollerRunnerRun(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	runner := adaptivePollerRunner[string]{
		logger:                slog.Default(),
		leagues:               []string{"ok", "err"},
		currentInterval:       func(string) time.Duration { return 5 * time.Millisecond },
		leagueID:              func(league string) string { return league },
		startMessage:          "test runner started",
		startAttrs:            []any{"leagues", 2},
		stopMessage:           "test runner stopped",
		initialFailureMessage: "initial failure",
		tickFailureMessage:    "tick failure",
		poll: func(_ context.Context, league, _ string) error {
			calls.Add(1)
			if league == "err" {
				return errors.New("boom")
			}
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 14*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		runner.run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runner did not stop after context cancellation")
	}

	if got := calls.Load(); got < int32(len(runner.leagues)) {
		t.Fatalf("poll call count = %d, want at least %d", got, len(runner.leagues))
	}
}
