package scraper

import (
	"context"
	"log/slog"
	"time"

	"github.com/shift-click/masterbot/internal/scraper/providers"
)

// SportsPollerConfig holds polling configuration.
type SportsPollerConfig struct {
	LiveInterval     time.Duration // polling during live matches (default 30s)
	MatchDayInterval time.Duration // polling on match days, pre-game (default 1h)
	IdleDayInterval  time.Duration // polling on days without matches (default 6h)
	PreMatchLeadTime time.Duration // switch to live polling this long before match start (default 30m)
	EventFetchDelay  time.Duration // delay before fetching events after score change (default 90s)
}

// FootballPollFunc is called by the poller to refresh football data for a league.
type FootballPollFunc func(ctx context.Context, league providers.FootballLeague, date string) error

// BaseballPollFunc is called by the poller to refresh baseball data for a league.
type BaseballPollFunc func(ctx context.Context, league providers.BaseballLeague, date string) error

// EsportsPollFunc is called by the poller to refresh esports data for a league.
type EsportsPollFunc func(ctx context.Context, league providers.EsportsLeague, date string) error

type matchDayDecision struct {
	live     bool
	matchDay bool
}

type adaptivePollerRunner[T any] struct {
	logger                *slog.Logger
	leagues               []T
	poll                  func(context.Context, T, string) error
	currentInterval       func(string) time.Duration
	leagueID              func(T) string
	startMessage          string
	startAttrs            []any
	stopMessage           string
	initialFailureMessage string
	tickFailureMessage    string
}

type adaptiveIntervalPlanner[T any] struct {
	config           SportsPollerConfig
	leagues          []T
	hasLive          func(T, string) bool
	classifyMatchDay func(T, string) matchDayDecision
}

// FootballPoller manages adaptive polling for football leagues.
type FootballPoller struct {
	config  SportsPollerConfig
	cache   *FootballCache
	pollFn  FootballPollFunc
	leagues []providers.FootballLeague
	logger  *slog.Logger
}

// NewFootballPoller creates a new adaptive football poller.
func NewFootballPoller(config SportsPollerConfig, cache *FootballCache, pollFn FootballPollFunc, leagues []providers.FootballLeague, logger *slog.Logger) *FootballPoller {
	if logger == nil {
		logger = slog.Default()
	}
	return &FootballPoller{
		config:  config,
		cache:   cache,
		pollFn:  pollFn,
		leagues: leagues,
		logger:  logger.With("component", "football_poller"),
	}
}

// Start begins adaptive polling. Blocks until ctx is cancelled.
func (p *FootballPoller) Start(ctx context.Context) {
	adaptivePollerRunner[providers.FootballLeague]{
		logger:          p.logger,
		leagues:         p.leagues,
		poll:            p.pollFn,
		currentInterval: p.currentInterval,
		leagueID:        func(league providers.FootballLeague) string { return league.ID },
		startMessage:    "football poller started",
		startAttrs: []any{
			"leagues", len(p.leagues),
			"live_interval", p.config.LiveInterval,
			"match_day_interval", p.config.MatchDayInterval,
			"idle_day_interval", p.config.IdleDayInterval,
		},
		stopMessage:           "football poller stopped",
		initialFailureMessage: "initial football poll failed",
		tickFailureMessage:    "football poll failed",
	}.run(ctx)
}

func (p *FootballPoller) currentInterval(today string) time.Duration {
	return adaptiveIntervalPlanner[providers.FootballLeague]{
		config:  p.config,
		leagues: p.leagues,
		hasLive: func(league providers.FootballLeague, date string) bool {
			return p.cache.HasLiveMatches(league.ID, date)
		},
		classifyMatchDay: p.matchDayDecision,
	}.current(today)
}

func (p *FootballPoller) matchDayDecision(league providers.FootballLeague, today string) matchDayDecision {
	matches, ok := p.cache.GetMatches(league.ID, today)
	if !ok || len(matches) == 0 {
		return matchDayDecision{}
	}
	now := time.Now()
	for _, match := range matches {
		if match.Status == providers.MatchScheduled && match.StartTime.Sub(now) <= p.config.PreMatchLeadTime {
			return matchDayDecision{live: true, matchDay: true}
		}
	}
	return matchDayDecision{matchDay: true}
}

// EsportsPoller manages adaptive polling for esports leagues.
type EsportsPoller struct {
	config  SportsPollerConfig
	cache   *EsportsCache
	pollFn  EsportsPollFunc
	leagues []providers.EsportsLeague
	logger  *slog.Logger
}

// NewEsportsPoller creates a new adaptive esports poller.
func NewEsportsPoller(config SportsPollerConfig, cache *EsportsCache, pollFn EsportsPollFunc, leagues []providers.EsportsLeague, logger *slog.Logger) *EsportsPoller {
	if logger == nil {
		logger = slog.Default()
	}
	return &EsportsPoller{
		config:  config,
		cache:   cache,
		pollFn:  pollFn,
		leagues: leagues,
		logger:  logger.With("component", "esports_poller"),
	}
}

// Start begins adaptive polling. Blocks until ctx is cancelled.
func (p *EsportsPoller) Start(ctx context.Context) {
	adaptivePollerRunner[providers.EsportsLeague]{
		logger:                p.logger,
		leagues:               p.leagues,
		poll:                  p.pollFn,
		currentInterval:       p.currentInterval,
		leagueID:              func(league providers.EsportsLeague) string { return league.ID },
		startMessage:          "esports poller started",
		startAttrs:            []any{"leagues", len(p.leagues)},
		stopMessage:           "esports poller stopped",
		initialFailureMessage: "initial esports poll failed",
		tickFailureMessage:    "esports poll failed",
	}.run(ctx)
}

func (p *EsportsPoller) currentInterval(today string) time.Duration {
	return adaptiveIntervalPlanner[providers.EsportsLeague]{
		config:  p.config,
		leagues: p.leagues,
		hasLive: func(league providers.EsportsLeague, date string) bool {
			return p.cache.HasLiveMatches(league.ID, date)
		},
		classifyMatchDay: p.matchDayDecision,
	}.current(today)
}

func (p *EsportsPoller) matchDayDecision(league providers.EsportsLeague, today string) matchDayDecision {
	matches, ok := p.cache.GetMatches(league.ID, today)
	if !ok || len(matches) == 0 {
		return matchDayDecision{}
	}
	return matchDayDecision{matchDay: true}
}

// BaseballPoller manages adaptive polling for baseball leagues.
type BaseballPoller struct {
	config  SportsPollerConfig
	cache   *BaseballCache
	pollFn  BaseballPollFunc
	leagues []providers.BaseballLeague
	logger  *slog.Logger
}

// NewBaseballPoller creates a new adaptive baseball poller.
func NewBaseballPoller(config SportsPollerConfig, cache *BaseballCache, pollFn BaseballPollFunc, leagues []providers.BaseballLeague, logger *slog.Logger) *BaseballPoller {
	if logger == nil {
		logger = slog.Default()
	}
	return &BaseballPoller{
		config:  config,
		cache:   cache,
		pollFn:  pollFn,
		leagues: leagues,
		logger:  logger.With("component", "baseball_poller"),
	}
}

// Start begins adaptive polling. Blocks until ctx is cancelled.
func (p *BaseballPoller) Start(ctx context.Context) {
	adaptivePollerRunner[providers.BaseballLeague]{
		logger:                p.logger,
		leagues:               p.leagues,
		poll:                  p.pollFn,
		currentInterval:       p.currentInterval,
		leagueID:              func(league providers.BaseballLeague) string { return league.ID },
		startMessage:          "baseball poller started",
		startAttrs:            []any{"leagues", len(p.leagues)},
		stopMessage:           "baseball poller stopped",
		initialFailureMessage: "initial baseball poll failed",
		tickFailureMessage:    "baseball poll failed",
	}.run(ctx)
}

func (p *BaseballPoller) currentInterval(today string) time.Duration {
	return adaptiveIntervalPlanner[providers.BaseballLeague]{
		config:  p.config,
		leagues: p.leagues,
		hasLive: func(league providers.BaseballLeague, date string) bool {
			return p.cache.HasLiveMatches(league.ID, date)
		},
		classifyMatchDay: p.matchDayDecision,
	}.current(today)
}

func (p *BaseballPoller) matchDayDecision(league providers.BaseballLeague, today string) matchDayDecision {
	matches, ok := p.cache.GetMatches(league.ID, today)
	if !ok || len(matches) == 0 {
		return matchDayDecision{}
	}
	now := time.Now()
	for _, match := range matches {
		if match.Status == providers.BaseballScheduled && match.StartTime.Sub(now) <= p.config.PreMatchLeadTime {
			return matchDayDecision{live: true, matchDay: true}
		}
	}
	return matchDayDecision{matchDay: true}
}

func (r adaptivePollerRunner[T]) run(ctx context.Context) {
	r.logger.Info(r.startMessage, r.startAttrs...)

	today := todayDateStr()
	r.pollOnce(ctx, today, r.initialFailureMessage, slog.LevelWarn)

	ticker := time.NewTicker(r.currentInterval(today))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info(r.stopMessage)
			return
		case <-ticker.C:
			today = todayDateStr()
			r.pollOnce(ctx, today, r.tickFailureMessage, slog.LevelDebug)
			ticker.Reset(r.currentInterval(today))
		}
	}
}

func (r adaptivePollerRunner[T]) pollOnce(ctx context.Context, today, message string, level slog.Level) {
	for _, league := range r.leagues {
		if err := r.poll(ctx, league, today); err != nil {
			r.logger.Log(ctx, level, message, "league", r.leagueID(league), "error", err)
		}
	}
}

func (p adaptiveIntervalPlanner[T]) current(today string) time.Duration {
	for _, league := range p.leagues {
		if p.hasLive(league, today) {
			return p.config.LiveInterval
		}
	}
	for _, league := range p.leagues {
		decision := p.classifyMatchDay(league, today)
		if decision.live {
			return p.config.LiveInterval
		}
		if decision.matchDay {
			return p.config.MatchDayInterval
		}
	}
	return p.config.IdleDayInterval
}

func todayDateStr() string {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		loc = time.FixedZone("KST", 9*60*60)
	}
	return time.Now().In(loc).Format("20060102")
}
