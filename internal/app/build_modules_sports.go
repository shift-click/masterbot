package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/shift-click/masterbot/internal/command"
	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/scraper"
	"github.com/shift-click/masterbot/internal/scraper/providers"
)

const (
	compactDateLayout = "20060102"
	dashedDateLayout  = "2006-01-02"

	footballNextSearchDays = 14
	baseballNextSearchDays = 7
)

type sportsRuntime struct {
	espn        *providers.ESPN
	livescore   *providers.Livescore
	apiFootball *providers.APIFootball
	oddsAPI     *providers.OddsAPI
	lolEsports  *providers.LoLEsports

	footballCache *scraper.FootballCache
	esportsCache  *scraper.EsportsCache
	kstLoc        *time.Location
}

func newSportsRuntime(cfg config.Config, logger *slog.Logger) sportsRuntime {
	return sportsRuntime{
		espn:          providers.NewESPN(logger),
		livescore:     providers.NewLivescore(logger),
		apiFootball:   providers.NewAPIFootball(cfg.Sports.APIFootballKey, logger),
		oddsAPI:       providers.NewOddsAPI(cfg.Sports.OddsAPIKey, logger),
		lolEsports:    providers.NewLoLEsports(logger),
		footballCache: scraper.NewFootballCache(cfg.Sports.OddsCacheTTL),
		esportsCache:  scraper.NewEsportsCache(),
		kstLoc:        koreaLocation(),
	}
}

func configureSportsModule(cfg config.Config, logger *slog.Logger, lifecycle *Lifecycle) sportsModule {
	runtime := newSportsRuntime(cfg, logger)

	footballPoller := scraper.NewFootballPoller(scraper.SportsPollerConfig{
		LiveInterval:     cfg.Sports.LivePollInterval,
		MatchDayInterval: cfg.Sports.MatchDayInterval,
		IdleDayInterval:  cfg.Sports.IdleDayInterval,
		PreMatchLeadTime: cfg.Sports.PreMatchLeadTime,
		EventFetchDelay:  cfg.Sports.EventFetchDelay,
	}, runtime.footballCache, runtime.footballPollFn(logger), providers.FootballLeagues(), logger)

	esportsPoller := scraper.NewEsportsPoller(scraper.SportsPollerConfig{
		LiveInterval:     cfg.Sports.LivePollInterval,
		MatchDayInterval: cfg.Sports.MatchDayInterval,
		IdleDayInterval:  cfg.Sports.IdleDayInterval,
		PreMatchLeadTime: cfg.Sports.PreMatchLeadTime,
	}, runtime.esportsCache, runtime.esportsPollFn(), providers.EsportsLeagues(), logger)

	lifecycle.Add("football-poller", func(ctx context.Context) error {
		footballPoller.Start(ctx)
		return nil
	})
	lifecycle.Add("esports-poller", func(ctx context.Context) error {
		esportsPoller.Start(ctx)
		return nil
	})

	return sportsModule{
		footballHandler: command.NewFootballHandler(runtime.footballCache, logger),
		esportsHandler:  command.NewEsportsHandler(runtime.esportsCache, logger),
	}
}

func (r sportsRuntime) footballPollFn(logger *slog.Logger) func(context.Context, providers.FootballLeague, string) error {
	return func(ctx context.Context, league providers.FootballLeague, date string) error {
		now := time.Now().In(r.kstLoc)
		kstToday := now.Format(compactDateLayout)

		allMatches := r.collectFootballMatches(ctx, league, now, logger)
		todayMatches := dedupeFootballMatchesByDate(allMatches, r.kstLoc, kstToday)
		r.footballCache.SetMatches(league.ID, date, todayMatches)

		if len(todayMatches) == 0 {
			r.cacheNextFootballMatches(ctx, league, now, kstToday, logger)
		}

		r.footballCache.DetectScoreChanges(todayMatches)
		r.enrichFootballEvents(ctx, league, now, kstToday, todayMatches)
		r.updateFootballOdds(ctx, league, todayMatches)
		return nil
	}
}

func (r sportsRuntime) collectFootballMatches(ctx context.Context, league providers.FootballLeague, now time.Time, logger *slog.Logger) []providers.FootballMatch {
	kstToday := now.Format(compactDateLayout)
	utcNow := now.UTC()
	utcToday := utcNow.Format(compactDateLayout)
	utcYesterday := utcNow.AddDate(0, 0, -1).Format(compactDateLayout)

	fetchDates := []string{utcToday}
	if utcToday != utcYesterday {
		fetchDates = append(fetchDates, utcYesterday)
	}

	var allMatches []providers.FootballMatch
	for _, date := range fetchDates {
		matches, err := r.fetchFootballMatches(ctx, league, date, date == utcToday)
		if err != nil {
			logger.Debug("football fetch failed", "league", league.ID, "date", date, "error", err)
			continue
		}
		allMatches = append(allMatches, matches...)
	}

	if league.ESPNSlug == "" && kstToday != utcToday {
		if lsMatches, err := r.livescore.FetchMatches(ctx, kstToday, league.LivescoreName); err == nil {
			allMatches = append(allMatches, lsMatches...)
		}
	}
	return allMatches
}

func (r sportsRuntime) fetchFootballMatches(ctx context.Context, league providers.FootballLeague, date string, allowFallback bool) ([]providers.FootballMatch, error) {
	if league.ESPNSlug != "" {
		matches, err := r.espn.FetchScoreboard(ctx, league.ESPNSlug, date)
		if err == nil {
			return matches, nil
		}
		if !allowFallback {
			return nil, err
		}
	}
	return r.livescore.FetchMatches(ctx, date, league.LivescoreName)
}

func dedupeFootballMatchesByDate(matches []providers.FootballMatch, loc *time.Location, targetDate string) []providers.FootballMatch {
	seen := make(map[string]bool, len(matches))
	filtered := make([]providers.FootballMatch, 0, len(matches))
	for _, match := range matches {
		if seen[match.ID] {
			continue
		}
		seen[match.ID] = true
		if match.StartTime.In(loc).Format(compactDateLayout) == targetDate {
			filtered = append(filtered, match)
		}
	}
	return filtered
}

func (r sportsRuntime) cacheNextFootballMatches(ctx context.Context, league providers.FootballLeague, now time.Time, kstToday string, logger *slog.Logger) {
	nowUTC := now.UTC()
	next := newNextFootballMatchesState()

	for dayOffset := 0; dayOffset <= footballNextSearchDays; dayOffset++ {
		futureUTC := nowUTC.AddDate(0, 0, dayOffset)
		futureDate := futureUTC.Format(compactDateLayout)
		futureMatches := r.fetchFutureFootballMatches(ctx, league, futureDate)
		next = r.collectNextFootballMatches(next, futureMatches, kstToday)
		if shouldStopNextFootballSearch(next.nextDateKST, futureDate) {
			break
		}
	}
	r.saveNextFootballMatches(league.ID, next, logger)
}

func (r sportsRuntime) fetchFutureFootballMatches(ctx context.Context, league providers.FootballLeague, date string) []providers.FootballMatch {
	if league.ESPNSlug != "" {
		matches, _ := r.espn.FetchScoreboard(ctx, league.ESPNSlug, date)
		return matches
	}
	matches, _ := r.livescore.FetchMatches(ctx, date, league.LivescoreName)
	return matches
}

type nextFootballMatchesState struct {
	nextDateKST string
	filtered    []providers.FootballMatch
	seen        map[string]bool
}

func newNextFootballMatchesState() nextFootballMatchesState {
	return nextFootballMatchesState{
		filtered: make([]providers.FootballMatch, 0),
		seen:     make(map[string]bool),
	}
}

func (r sportsRuntime) collectNextFootballMatches(
	current nextFootballMatchesState,
	futureMatches []providers.FootballMatch,
	kstToday string,
) nextFootballMatchesState {
	for _, match := range futureMatches {
		if current.seen[match.ID] {
			continue
		}
		matchDateKST := match.StartTime.In(r.kstLoc).Format(compactDateLayout)
		if matchDateKST <= kstToday {
			continue
		}
		if current.nextDateKST == "" {
			current.nextDateKST = matchDateKST
		}
		if matchDateKST != current.nextDateKST {
			continue
		}
		current.filtered = append(current.filtered, match)
		current.seen[match.ID] = true
	}
	return current
}

func shouldStopNextFootballSearch(nextDateKST, futureDate string) bool {
	return nextDateKST != "" && futureDate > nextDateKST
}

func (r sportsRuntime) saveNextFootballMatches(leagueID string, next nextFootballMatchesState, logger *slog.Logger) {
	if len(next.filtered) == 0 {
		return
	}
	nextDay, _ := time.ParseInLocation(compactDateLayout, next.nextDateKST, r.kstLoc)
	logger.Debug("next matches found", "league", leagueID, "date", next.nextDateKST, "count", len(next.filtered))
	r.footballCache.SetNextMatches(leagueID, next.filtered, nextDay)
}

func (r sportsRuntime) enrichFootballEvents(ctx context.Context, league providers.FootballLeague, now time.Time, kstDate string, matches []providers.FootballMatch) {
	for _, match := range matches {
		if skipFootballEventFetch(r.footballCache, match) {
			continue
		}
		if len(match.Events) > 0 {
			r.footballCache.SetEvents(match.ID, match.Events)
			continue
		}

		events := r.fetchFootballEvents(ctx, league, now, kstDate, match)
		if len(events) > 0 {
			r.footballCache.SetEvents(match.ID, events)
		}
	}
}

func skipFootballEventFetch(cache *scraper.FootballCache, match providers.FootballMatch) bool {
	if _, ok := cache.GetEvents(match.ID); ok && match.Status == providers.MatchFinished {
		return true
	}
	return match.Status != providers.MatchFinished && match.Status != providers.MatchLive
}

func (r sportsRuntime) fetchFootballEvents(ctx context.Context, league providers.FootballLeague, now time.Time, kstDate string, match providers.FootballMatch) []providers.MatchEvent {
	var events []providers.MatchEvent
	if lsEvents, err := r.livescore.FetchMatchEvents(ctx, match.ID); err == nil && len(lsEvents) > 0 {
		events = lsEvents
	}

	if !hasIncompleteGoalScorer(events) {
		return events
	}
	if r.apiFootball.DailyBudgetRemaining() <= 0 {
		return events
	}

	afEvents, err := r.apiFootball.FetchEventsForMatch(
		ctx,
		kstDate,
		league.APIFootballID,
		now.Year(),
		match.HomeTeam,
		match.AwayTeam,
	)
	if err != nil || len(afEvents) == 0 {
		return events
	}
	return afEvents
}

func hasIncompleteGoalScorer(events []providers.MatchEvent) bool {
	for _, event := range events {
		if event.Player != "" {
			continue
		}
		switch event.Type {
		case providers.EventGoal, providers.EventPenalty, providers.EventOwnGoal:
			return true
		}
	}
	return false
}

func (r sportsRuntime) updateFootballOdds(ctx context.Context, league providers.FootballLeague, todayMatches []providers.FootballMatch) {
	target, ok := firstMissingOddsMatch(r.footballCache, league.ID, todayMatches)
	if !ok || league.OddsAPISportKey == "" {
		return
	}

	odds, err := r.oddsAPI.FetchOdds(ctx, league.OddsAPISportKey)
	if err != nil {
		return
	}
	for _, odd := range odds {
		if providers.TeamNameMatch(odd.HomeTeam, target.HomeTeam) && providers.TeamNameMatch(odd.AwayTeam, target.AwayTeam) {
			r.footballCache.SetOdds(target.ID, odd.OddsHome, odd.OddsDraw, odd.OddsAway)
			return
		}
	}
}

func firstMissingOddsMatch(cache *scraper.FootballCache, leagueID string, todayMatches []providers.FootballMatch) (providers.FootballMatch, bool) {
	allMatches := append([]providers.FootballMatch(nil), todayMatches...)
	if nextMatches, _, ok := cache.GetNextMatches(leagueID); ok {
		allMatches = append(allMatches, nextMatches...)
	}

	for _, match := range allMatches {
		if match.Status != providers.MatchScheduled {
			continue
		}
		if match.OddsHome > 0 {
			continue
		}
		if _, _, _, ok := cache.GetOdds(match.ID); ok {
			continue
		}
		return match, true
	}
	return providers.FootballMatch{}, false
}

func (r sportsRuntime) esportsPollFn() func(context.Context, providers.EsportsLeague, string) error {
	return func(ctx context.Context, league providers.EsportsLeague, date string) error {
		allMatches, err := r.lolEsports.FetchSchedule(ctx, league.LeagueID)
		if err != nil {
			return err
		}

		targetDate := normalizeDashedDate(date)
		filtered := make([]providers.EsportsMatch, 0, len(allMatches))
		for _, match := range allMatches {
			matchDate := match.StartTime.In(r.kstLoc).Format(dashedDateLayout)
			if matchDate != targetDate && matchDate != date {
				continue
			}
			match.LeagueID = league.ID
			filtered = append(filtered, match)
		}
		r.esportsCache.SetMatches(league.ID, date, filtered)
		return nil
	}
}

func normalizeDashedDate(date string) string {
	if len(date) != 8 {
		return date
	}
	return date[:4] + "-" + date[4:6] + "-" + date[6:8]
}

type baseballRuntime struct {
	mlb    *providers.MLB
	kbo    *providers.KBO
	npb    *providers.NPB
	cache  *scraper.BaseballCache
	kstLoc *time.Location
}

func newBaseballRuntime(logger *slog.Logger) baseballRuntime {
	return baseballRuntime{
		mlb:    providers.NewMLB(logger),
		kbo:    providers.NewKBO(logger),
		npb:    providers.NewNPB(logger),
		cache:  scraper.NewBaseballCache(),
		kstLoc: koreaLocation(),
	}
}

func configureBaseballModule(cfg config.Config, logger *slog.Logger, lifecycle *Lifecycle) *command.BaseballHandler {
	runtime := newBaseballRuntime(logger)
	poller := scraper.NewBaseballPoller(scraper.SportsPollerConfig{
		LiveInterval:     cfg.Sports.LivePollInterval,
		MatchDayInterval: cfg.Sports.MatchDayInterval,
		IdleDayInterval:  cfg.Sports.IdleDayInterval,
		PreMatchLeadTime: cfg.Sports.PreMatchLeadTime,
	}, runtime.cache, runtime.pollFn(), providers.BaseballLeagues(), logger)

	lifecycle.Add("baseball-poller", func(ctx context.Context) error {
		poller.Start(ctx)
		return nil
	})
	return command.NewBaseballHandler(runtime.cache, logger)
}

func (r baseballRuntime) pollFn() func(context.Context, providers.BaseballLeague, string) error {
	return func(ctx context.Context, league providers.BaseballLeague, date string) error {
		now := time.Now().In(r.kstLoc)
		kstDate := normalizeDashedDate(date)

		matches, err := r.fetchMatches(ctx, league, kstDate, now)
		if err != nil {
			return err
		}
		r.cache.SetMatches(league.ID, date, matches)

		if len(matches) == 0 {
			r.cacheNextMatches(ctx, league, now)
		}
		return nil
	}
}

func (r baseballRuntime) fetchMatches(ctx context.Context, league providers.BaseballLeague, kstDate string, now time.Time) ([]providers.BaseballMatch, error) {
	switch league.ID {
	case "mlb":
		return r.fetchMLBMatches(ctx, kstDate, now)
	case "kbo":
		return r.kbo.FetchSchedule(ctx, kstDate)
	case "npb":
		return r.npb.FetchSchedule(ctx, kstDate)
	default:
		return nil, nil
	}
}

func (r baseballRuntime) fetchMLBMatches(ctx context.Context, kstDate string, now time.Time) ([]providers.BaseballMatch, error) {
	matches, err := r.mlb.FetchSchedule(ctx, kstDate, "R")
	if err == nil && len(matches) == 0 {
		matches, err = r.mlb.FetchSchedule(ctx, kstDate, "S")
	}
	if err == nil && len(matches) == 0 {
		matches, err = r.mlb.FetchSchedule(ctx, kstDate, "P")
	}
	if err != nil {
		return nil, err
	}

	todayKST := now.Format(compactDateLayout)
	filtered := make([]providers.BaseballMatch, 0, len(matches))
	for _, match := range matches {
		if match.StartTime.IsZero() || match.StartTime.In(r.kstLoc).Format(compactDateLayout) == todayKST {
			filtered = append(filtered, match)
		}
	}
	return filtered, nil
}

func (r baseballRuntime) cacheNextMatches(ctx context.Context, league providers.BaseballLeague, now time.Time) {
	for dayOffset := 1; dayOffset <= baseballNextSearchDays; dayOffset++ {
		futureDate := now.AddDate(0, 0, dayOffset)
		futureDateStr := futureDate.Format(dashedDateLayout)
		matches := r.fetchFutureMatches(ctx, league, futureDateStr)
		if len(matches) == 0 {
			continue
		}
		r.cache.SetNextMatches(league.ID, matches, futureDate)
		return
	}
}

func (r baseballRuntime) fetchFutureMatches(ctx context.Context, league providers.BaseballLeague, futureDate string) []providers.BaseballMatch {
	switch league.ID {
	case "mlb":
		matches, _ := r.mlb.FetchSchedule(ctx, futureDate, "R")
		if len(matches) == 0 {
			matches, _ = r.mlb.FetchSchedule(ctx, futureDate, "S")
		}
		return matches
	case "kbo":
		matches, _ := r.kbo.FetchSchedule(ctx, futureDate)
		return matches
	case "npb":
		matches, _ := r.npb.FetchSchedule(ctx, futureDate)
		return matches
	default:
		return nil
	}
}
