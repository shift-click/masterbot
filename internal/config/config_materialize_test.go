package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfigHelpersAndScopeParsers(t *testing.T) {
	t.Parallel()

	if got := mapEnvKey("JUCOBOT_BOT_COMMAND_PREFIX"); got != "bot.command_prefix" {
		t.Fatalf("mapEnvKey = %q", got)
	}
	if got := mapEnvKey("JUCOBOT_SINGLE"); got != "single" {
		t.Fatalf("mapEnvKey single = %q", got)
	}

	if got := splitCommaList(" a , ,b,c "); len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("splitCommaList = %v", got)
	}
	if got := splitPipeList(" x ; ; y "); len(got) != 2 || got[0] != "x" || got[1] != "y" {
		t.Fatalf("splitPipeList = %v", got)
	}
	if got := normalizeBasePath(" admin/path/ "); got != "/admin/path" {
		t.Fatalf("normalizeBasePath = %q", got)
	}
	if got := normalizeBasePath(""); got != "/admin" {
		t.Fatalf("normalizeBasePath empty = %q", got)
	}

	scopes, err := parseAudienceScopes("partner@example.com|partner|t1;t2|r1|coin;stock")
	if err != nil {
		t.Fatalf("parseAudienceScopes valid error = %v", err)
	}
	if len(scopes) != 1 || scopes[0].Email != "partner@example.com" || len(scopes[0].Tenants) != 2 {
		t.Fatalf("unexpected parsed scopes: %+v", scopes)
	}
	if _, err := parseAudienceScopes("broken"); err == nil {
		t.Fatal("expected parseAudienceScopes error for invalid format")
	}
	if _, err := parseAudienceScopes(" |partner|t|r|f"); err == nil {
		t.Fatal("expected parseAudienceScopes error for missing email")
	}
}

func TestLoadAndValidateAdditionalPaths(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "no-such.yaml"))
	if err != nil {
		t.Fatalf("Load(nonexistent) error = %v", err)
	}
	if cfg.Bot.CommandPrefix != "/" {
		t.Fatalf("default command prefix = %q", cfg.Bot.CommandPrefix)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("bot:\n  command_prefix: \"#\"\n"), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	t.Setenv("JUCOBOT_BOT_COMMAND_PREFIX", "!")
	cfg, err = Load(path)
	if err != nil {
		t.Fatalf("Load(with env override) error = %v", err)
	}
	if cfg.Bot.CommandPrefix != "!" {
		t.Fatalf("expected env override command prefix, got %q", cfg.Bot.CommandPrefix)
	}

	badPath := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(badPath, []byte("bot: ["), 0o600); err != nil {
		t.Fatalf("write bad yaml: %v", err)
	}
	if _, err := Load(badPath); err == nil {
		t.Fatal("expected Load error for invalid yaml")
	}

	invalid := Default()
	invalid.Admin.Enabled = true
	invalid.Admin.MetricsEnabled = false
	invalid.Cache.Driver = "bad"
	invalid.AutoQuery.DefaultPolicy.Mode = "bad-mode"
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected Validate() error for invalid combined config")
	}
}

func TestRawMaterializeParseErrors(t *testing.T) {
	t.Parallel()

	if _, err := (rawBotConfig{ShutdownTimeout: "oops"}).materialize(); err == nil {
		t.Fatal("expected bot materialize parse error")
	}
	if _, err := (rawCacheConfig{DefaultTTL: "1s", StaleTTL: "oops", CleanupInterval: "1s"}).materialize(); err == nil {
		t.Fatal("expected cache materialize parse error")
	}
	if _, err := (rawStockConfig{PollInterval: "1s", IdleTimeout: "oops", OffHourInterval: "1s"}).materialize(); err == nil {
		t.Fatal("expected stock materialize parse error")
	}
	if _, err := (rawWeatherConfig{CacheTTL: "1s", YesterdayCacheTTL: "oops"}).materialize(); err == nil {
		t.Fatal("expected weather materialize parse error")
	}
	if _, err := (rawIrisConfig{RequestTimeout: "1s", ReconnectMin: "oops", ReconnectMax: "1s"}).materialize(); err == nil {
		t.Fatal("expected iris materialize parse error")
	}
	if _, err := (rawSportsConfig{
		LivePollInterval: "1s", MatchDayInterval: "1m", IdleDayInterval: "1h",
		PreMatchLeadTime: "oops", OddsCacheTTL: "1h", EventFetchDelay: "1s",
	}).materialize(); err == nil {
		t.Fatal("expected sports materialize parse error")
	}
	if _, err := (rawAutoQueryConfig{
		DefaultPolicy: rawAutoQueryPolicy{
			Mode:           autoQueryModeExplicitOnly,
			BudgetPerHour:  1,
			CooldownWindow: "1s",
		},
		Rooms: []rawAutoQueryRoom{{ChatID: "r1", CooldownWindow: "oops"}},
	}).materialize(); err == nil {
		t.Fatal("expected auto_query room cooldown parse error")
	}
	if _, err := (rawCoupangConfig{
		CollectInterval: "1m", IdleTimeout: "1h", MaxProducts: 1, HotInterval: "1h", WarmInterval: "2h",
		ColdInterval: "3h", Freshness: "1h", StaleThreshold: "2h", MinRefreshInterval: "1m",
		TierWindow: "1h", MappingRecheckBackoff: "1h", ReadRefreshTimeout: "oops",
	}).materialize(); err == nil {
		t.Fatal("expected coupang materialize parse error")
	}
	if _, err := (rawAdminConfig{
		FlushInterval: "1s", RollupInterval: "1m", RawRetention: "1h", HourlyRetention: "1h",
		DailyRetention: "1h", ErrorRetention: "oops",
	}).materialize(); err == nil {
		t.Fatal("expected admin materialize parse error")
	}
}

func TestValidationHelpersCoverage(t *testing.T) {
	t.Parallel()

	var problems []string
	AccessConfig{
		DefaultPolicy: "invalid",
		Rooms: []AccessRoomConfig{
			{ChatID: "room-1"},
			{ChatID: "room-1"},
			{ChatID: ""},
		},
	}.validate(&problems)
	if len(problems) == 0 {
		t.Fatal("expected access validation problems")
	}
	if got := compactStrings([]string{" a ", "a", "b", "", "b"}); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("compactStrings = %v", got)
	}
	if !(AccessConfig{RuntimeDBPath: "x"}).RuntimeEnabled() {
		t.Fatal("expected RuntimeEnabled=true for non-empty db path")
	}

	problems = nil
	CoupangConfig{
		CollectInterval: 0, IdleTimeout: -1, MaxProducts: 0,
		HotInterval: 0, WarmInterval: 0, ColdInterval: 0,
		Freshness: 1 * time.Hour, StaleThreshold: 30 * time.Minute,
		MinRefreshInterval: 0, RefreshBudgetPerHour: 0, RegistrationBudgetPerHour: 0, ResolutionBudgetPerHour: 0,
		TierWindow: 0, HotThreshold: 0, WarmThreshold: 0, CandidateFanout: 0, MappingRecheckBackoff: 0,
		ReadRefreshTimeout: 0,
	}.validate(&problems)
	if len(problems) == 0 {
		t.Fatal("expected coupang validation problems")
	}
	if !strings.Contains(strings.Join(problems, " "), "registration_join_wait") {
		t.Fatalf("expected registration join wait problem, got %v", problems)
	}
	if !strings.Contains(strings.Join(problems, " "), "read_refresh_join_wait") {
		t.Fatalf("expected read refresh join wait problem, got %v", problems)
	}

	problems = nil
	AdminConfig{
		Enabled:           true,
		MetricsEnabled:    true,
		AuthEmailHeader:   "",
		AllowedEmails:     nil,
		AudienceScopes:    []AdminAudienceScope{{Email: "dup@example.com", Role: "bad"}, {Email: "dup@example.com", Role: "operator"}},
		TrustedProxyCIDRs: nil,
	}.validate(&problems)
	if len(problems) == 0 {
		t.Fatal("expected admin validation problems")
	}
	if !strings.Contains(strings.Join(problems, " "), "duplicate email") {
		t.Fatalf("expected duplicate email problem, got %v", problems)
	}
}

func TestValidateCoupangPositiveHelpers(t *testing.T) {
	t.Parallel()

	var problems []string
	validateCoupangPositiveValues(CoupangConfig{
		CollectInterval:           -time.Second,
		IdleTimeout:               -time.Second,
		MaxProducts:               0,
		HotInterval:               0,
		WarmInterval:              time.Minute,
		ColdInterval:              time.Minute,
		Freshness:                 -time.Second,
		MinRefreshInterval:        -time.Second,
		RefreshBudgetPerHour:      0,
		RegistrationBudgetPerHour: 0,
		ResolutionBudgetPerHour:   0,
		TierWindow:                -time.Second,
		WarmThreshold:             0,
		CandidateFanout:           0,
		MappingRecheckBackoff:     -time.Second,
		ReadRefreshTimeout:        -time.Second,
		RegistrationJoinWait:      -time.Second,
		ReadRefreshJoinWait:       -time.Second,
	}, &problems)

	joined := strings.Join(problems, " | ")
	for _, want := range []string{
		"coupang.collect_interval must be greater than zero",
		"coupang.registration_join_wait must be greater than zero",
		"coupang.read_refresh_join_wait must be greater than zero",
		"coupang tier intervals must be greater than zero",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %v", want, problems)
		}
	}
}

func TestAutoQueryHelperCoverage(t *testing.T) {
	t.Parallel()

	rooms := []AutoQueryRoomConfig{
		{
			ChatID:            " room-1 ",
			Mode:              autoQueryModeLocalAuto,
			AllowedHandlers:   []string{"stock", " stock ", "coin"},
			BudgetPerHour:     5,
			CooldownWindow:    time.Minute,
			DegradationTarget: autoQueryModeExplicitOnly,
		},
	}

	rawRooms := toRawAutoQueryRooms(rooms)
	if len(rawRooms) != 1 || rawRooms[0].ChatID != " room-1 " {
		t.Fatalf("toRawAutoQueryRooms = %+v", rawRooms)
	}
	if roomFieldPath(2, "chat_id") != "auto_query.rooms[2].chat_id" {
		t.Fatalf("roomFieldPath = %q", roomFieldPath(2, "chat_id"))
	}

	materialized, err := materializeAutoQueryRooms(rawRooms)
	if err != nil {
		t.Fatalf("materializeAutoQueryRooms: %v", err)
	}
	if len(materialized) != 1 || materialized[0].ChatID != "room-1" {
		t.Fatalf("materialized rooms = %+v", materialized)
	}
	if len(materialized[0].AllowedHandlers) != 2 {
		t.Fatalf("allowed handlers = %v", materialized[0].AllowedHandlers)
	}

	var problems []string
	validateAutoQueryDefaultPolicy(AutoQueryPolicyConfig{
		Mode:              "bad",
		BudgetPerHour:     0,
		CooldownWindow:    0,
		DegradationTarget: "bad",
	}, &problems)
	if len(problems) != 4 {
		t.Fatalf("expected 4 default policy problems, got %v", problems)
	}

	problems = nil
	validateAutoQueryRooms([]AutoQueryRoomConfig{
		{ChatID: "room-1", Mode: "bad", BudgetPerHour: -1, CooldownWindow: -time.Second, DegradationTarget: "bad"},
		{ChatID: "room-1"},
	}, &problems)
	if len(problems) < 5 {
		t.Fatalf("expected room validation problems, got %v", problems)
	}
}
