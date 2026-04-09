package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/scraper"
	"github.com/shift-click/masterbot/internal/scraper/providers"
)

func TestConfigureSportsAndBaseballModules(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	logger := slog.Default()

	sportsLC := &Lifecycle{}
	sports := configureSportsModule(cfg, logger, sportsLC)
	if sports.footballHandler == nil || sports.esportsHandler == nil {
		t.Fatalf("sports handlers should not be nil: %+v", sports)
	}
	if len(sportsLC.components) != 2 {
		t.Fatalf("sports lifecycle components = %d, want 2", len(sportsLC.components))
	}

	baseballLC := &Lifecycle{}
	baseball := configureBaseballModule(cfg, logger, baseballLC)
	if baseball == nil {
		t.Fatal("baseball handler should not be nil")
	}
	if len(baseballLC.components) != 1 {
		t.Fatalf("baseball lifecycle components = %d, want 1", len(baseballLC.components))
	}
}

func TestSportsRuntimeFootballPollFlow(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/scoreboard"):
			tomorrow := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
			fmt.Fprintf(w, `{"events":[{"id":"espn-next","competitions":[{"competitors":[{"homeAway":"home","team":{"displayName":"Home FC"},"score":"0"},{"homeAway":"away","team":{"displayName":"Away FC"},"score":"0"}],"status":{"type":{"state":"pre","detail":"-"}},"startDate":"%s"}]}]}`, tomorrow)
		case strings.Contains(r.URL.Path, "/v4/sports/soccer_epl/odds"):
			fmt.Fprint(w, `[{"home_team":"Home FC","away_team":"Away FC","commence_time":"2026-03-19T12:00:00Z","bookmakers":[{"markets":[{"key":"h2h","outcomes":[{"name":"Home FC","price":1.9},{"name":"Draw","price":3.3},{"name":"Away FC","price":4.2}]}]}]}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	logger := slog.Default()
	espn := providers.NewESPN(logger)
	setUnexportedField(t, espn, "client", &http.Client{Timeout: 5 * time.Second, Transport: appRewriteTransport{base: srv.URL}})

	odds := providers.NewOddsAPI("key", logger)
	setUnexportedField(t, odds, "client", &http.Client{Timeout: 5 * time.Second, Transport: appRewriteTransport{base: srv.URL}})

	runtime := sportsRuntime{
		espn:          espn,
		oddsAPI:       odds,
		footballCache: scraper.NewFootballCache(time.Hour),
		kstLoc:        koreaLocation(),
	}

	league := providers.FootballLeague{
		ID:              "epl",
		ESPNSlug:        "eng.1",
		OddsAPISportKey: "soccer_epl",
	}
	date := time.Now().In(runtime.kstLoc).Format(compactDateLayout)

	poll := runtime.footballPollFn(logger)
	if err := poll(context.Background(), league, date); err != nil {
		t.Fatalf("football poll error: %v", err)
	}

	today, ok := runtime.footballCache.GetMatches(league.ID, date)
	if !ok {
		t.Fatal("expected today matches cache entry")
	}
	if len(today) != 0 {
		t.Fatalf("expected no today matches to trigger next-match caching, got %+v", today)
	}

	nextMatches, _, ok := runtime.footballCache.GetNextMatches(league.ID)
	if !ok || len(nextMatches) == 0 {
		t.Fatalf("expected next matches in cache, got ok=%v len=%d", ok, len(nextMatches))
	}

	home, draw, away, ok := runtime.footballCache.GetOdds(nextMatches[0].ID)
	if !ok || home <= 0 || draw <= 0 || away <= 0 {
		t.Fatalf("expected odds for next match, got ok=%v odds=(%v,%v,%v)", ok, home, draw, away)
	}
}

func TestSportsRuntimeAndBaseballHelpers(t *testing.T) {
	t.Parallel()

	cache := scraper.NewFootballCache(time.Hour)
	runtime := sportsRuntime{footballCache: cache, kstLoc: koreaLocation()}

	matchID := "m-finished"
	cache.SetEvents(matchID, []providers.MatchEvent{{Type: providers.EventGoal, Player: "A"}})
	if !skipFootballEventFetch(cache, providers.FootballMatch{ID: matchID, Status: providers.MatchFinished}) {
		t.Fatal("finished match with cached events should be skipped")
	}
	if skipFootballEventFetch(cache, providers.FootballMatch{ID: "m-scheduled", Status: providers.MatchScheduled}) != true {
		t.Fatal("scheduled match should be skipped")
	}

	runtime.enrichFootballEvents(context.Background(), providers.FootballLeague{}, time.Now(), "", []providers.FootballMatch{
		{ID: "m-events", Status: providers.MatchLive, Events: []providers.MatchEvent{{Type: providers.EventGoal, Player: "P"}}},
		{ID: "m-scheduled", Status: providers.MatchScheduled},
	})
	if events, ok := cache.GetEvents("m-events"); !ok || len(events) != 1 {
		t.Fatalf("expected cached inline events, got ok=%v events=%+v", ok, events)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/api/v1/schedule") {
			gameType := r.URL.Query().Get("gameType")
			if gameType == "R" {
				fmt.Fprint(w, `{"dates":[{"games":[]}]}`)
				return
			}
			fmt.Fprint(w, `{"dates":[{"games":[{"gamePk":101,"gameDate":"","status":{"statusCode":"S"},"teams":{"away":{"team":{"name":"Away"},"score":0},"home":{"team":{"name":"Home"},"score":0}},"linescore":{"currentInning":0,"isTopInning":true}}]}]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	mlb := providers.NewMLB(slog.Default())
	setUnexportedField(t, mlb, "client", &http.Client{Timeout: 5 * time.Second, Transport: appRewriteTransport{base: srv.URL}})

	baseball := baseballRuntime{
		mlb:    mlb,
		cache:  scraper.NewBaseballCache(),
		kstLoc: koreaLocation(),
	}

	now := time.Now().In(baseball.kstLoc)
	matches, err := baseball.fetchMLBMatches(context.Background(), now.Format(dashedDateLayout), now)
	if err != nil {
		t.Fatalf("fetchMLBMatches error: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected mlb matches")
	}

	fetched, err := baseball.fetchMatches(context.Background(), providers.BaseballLeague{ID: "mlb"}, now.Format(dashedDateLayout), now)
	if err != nil {
		t.Fatalf("fetchMatches mlb error: %v", err)
	}
	if len(fetched) == 0 {
		t.Fatal("expected mlb fetchMatches result")
	}

	baseball.cacheNextMatches(context.Background(), providers.BaseballLeague{ID: "mlb"}, now)
	if next, _, ok := baseball.cache.GetNextMatches("mlb"); !ok || len(next) == 0 {
		t.Fatalf("expected next baseball matches, got ok=%v len=%d", ok, len(next))
	}

	poll := baseball.pollFn()
	if err := poll(context.Background(), providers.BaseballLeague{ID: "unknown"}, now.Format(compactDateLayout)); err != nil {
		t.Fatalf("unknown league poll should not fail, got %v", err)
	}
}

func TestSportsRuntimeFetchFootballEventsAndEsportsPoll(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/scoreboard/soccer/match-1"):
			fmt.Fprint(w, `{"T1":[{"Nm":"Home FC"}],"T2":[{"Nm":"Away FC"}],"Incs-s":{"g1":[{"Min":12,"Sc":[1,0],"Incs":[{"Min":12,"IT":36,"Pn":"","Sc":[1,0]}]}]}}`)
		case strings.Contains(r.URL.Path, "/fixtures/events"):
			fmt.Fprint(w, `{"response":[{"time":{"elapsed":12},"team":{"name":"Home FC"},"player":{"name":"AF Player"},"assist":{"name":"AF Assist"},"type":"Goal","detail":"Normal Goal"}]}`)
		case strings.Contains(r.URL.Path, "/fixtures"):
			fmt.Fprint(w, `{"response":[{"fixture":{"id":777},"teams":{"home":{"name":"Home FC"},"away":{"name":"Away FC"}}}]}`)
		case strings.Contains(r.URL.Path, "/persisted/gw/getSchedule"):
			fmt.Fprint(w, `{"data":{"schedule":{"events":[{"startTime":"2026-03-19T12:00:00Z","state":"completed","blockName":"1주차","match":{"teams":[{"name":"T1","code":"T1","result":{"gameWins":2}},{"name":"GEN","code":"GEN","result":{"gameWins":1}}],"strategy":{"type":"bestOf","count":3}}}]}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	logger := slog.Default()
	livescore := providers.NewLivescore(logger)
	setUnexportedField(t, livescore, "client", &http.Client{Timeout: 5 * time.Second, Transport: appRewriteTransport{base: srv.URL}})

	apiFootball := providers.NewAPIFootball("key", logger)
	setUnexportedField(t, apiFootball, "client", &http.Client{Timeout: 5 * time.Second, Transport: appRewriteTransport{base: srv.URL}})

	lol := providers.NewLoLEsports(logger)
	setUnexportedField(t, lol, "client", &http.Client{Timeout: 5 * time.Second, Transport: appRewriteTransport{base: srv.URL}})

	runtime := sportsRuntime{
		livescore:     livescore,
		apiFootball:   apiFootball,
		lolEsports:    lol,
		footballCache: scraper.NewFootballCache(time.Hour),
		esportsCache:  scraper.NewEsportsCache(),
		kstLoc:        koreaLocation(),
	}

	events := runtime.fetchFootballEvents(
		context.Background(),
		providers.FootballLeague{APIFootballID: 39},
		time.Now(),
		time.Now().In(runtime.kstLoc).Format(compactDateLayout),
		providers.FootballMatch{ID: "match-1", HomeTeam: "Home FC", AwayTeam: "Away FC"},
	)
	if len(events) == 0 || events[0].Player == "" {
		t.Fatalf("expected api-football enriched events, got %+v", events)
	}

	poll := runtime.esportsPollFn()
	date := time.Now().In(runtime.kstLoc).Format(compactDateLayout)
	if err := poll(context.Background(), providers.EsportsLeague{ID: "lck", LeagueID: "98767991310872058"}, date); err != nil {
		t.Fatalf("esports poll error: %v", err)
	}

	matches, ok := runtime.esportsCache.GetMatches("lck", date)
	if !ok {
		t.Fatal("expected esports cache entry")
	}
	if len(matches) > 0 && matches[0].LeagueID != "lck" {
		t.Fatalf("unexpected esports league id: %+v", matches[0])
	}
}

type appRewriteTransport struct {
	base string
}

func (t appRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, _ := url.Parse(t.base)
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	return http.DefaultTransport.RoundTrip(req)
}

func setUnexportedField(t *testing.T, target any, field string, value any) {
	t.Helper()
	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		t.Fatalf("target must be non-nil pointer: %T", target)
	}
	elem := v.Elem()
	f := elem.FieldByName(field)
	if !f.IsValid() {
		t.Fatalf("field %q not found in %T", field, target)
	}

	// Auto-wrap *http.Client → *BreakerHTTPClient when the field expects it.
	if httpClient, ok := value.(*http.Client); ok {
		if f.Type() == reflect.TypeOf((*providers.BreakerHTTPClient)(nil)) {
			value = providers.NewBreakerHTTPClient(httpClient, "test", nil)
		}
	}

	val := reflect.ValueOf(value)
	if !val.Type().AssignableTo(f.Type()) {
		if val.Type().ConvertibleTo(f.Type()) {
			val = val.Convert(f.Type())
		} else {
			t.Fatalf("cannot assign %s to %s for field %q", val.Type(), f.Type(), field)
		}
	}

	ptr := unsafe.Pointer(f.UnsafeAddr())
	reflect.NewAt(f.Type(), ptr).Elem().Set(val)
}
