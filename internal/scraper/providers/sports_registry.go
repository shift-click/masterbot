package providers

import "strings"

// FootballLeague defines a supported football league with all provider-specific identifiers.
type FootballLeague struct {
	ID              string   // internal identifier
	Name            string   // Korean display name
	Aliases         []string // user input aliases
	ESPNSlug        string   // ESPN API league slug (empty if not supported)
	LivescoreName   string   // Livescore API stage name filter
	APIFootballID   int      // API-Football league ID
	OddsAPISportKey string   // The Odds API sport key
}

var footballLeagues = []FootballLeague{
	newFootballLeague("epl", "EPL", []string{"epl", "프리미어리그", "프리미어", "잉프리", "영프리"}, "eng.1", "Premier League", 39, "soccer_epl"),
	newFootballLeague("laliga", "라리가", []string{"라리가", "laliga", "la liga", "스페인"}, "esp.1", "LaLiga", 140, "soccer_spain_la_liga"),
	newFootballLeague("seriea", "세리에A", []string{"세리에", "세리에a", "seriea", "serie a", "이탈리아"}, "ita.1", "Serie A", 135, "soccer_italy_serie_a"),
	newFootballLeague("bundesliga", "분데스리가", []string{"분데스", "분데스리가", "bundesliga", "독일"}, "ger.1", "Bundesliga", 78, "soccer_germany_bundesliga"),
	newFootballLeague("kleague", "K리그", []string{"k리그", "k리그1", "kleague", "k league", "케이리그"}, "", "K-League 1", 292, "soccer_korea_kleague1"),
	newFootballLeague("ucl", "챔피언스리그", []string{"챔스", "챔피언스리그", "ucl", "champions league", "cl"}, "uefa.champions", "Champions League", 2, "soccer_uefa_champs_league"),
}

// FootballLeagues returns a copy of all registered football leagues.
func FootballLeagues() []FootballLeague {
	return cloneCatalog(footballLeagues)
}

// LookupFootballLeague resolves user input to a FootballLeague.
// Returns the league and true if found, zero value and false otherwise.
func LookupFootballLeague(input string) (FootballLeague, bool) {
	return lookupCatalog(input, footballLeagues, func(league FootballLeague) (string, string, []string) {
		return league.ID, league.Name, league.Aliases
	})
}

// EsportsLeague defines a supported LoL esports league.
type EsportsLeague struct {
	ID       string   // internal identifier
	Name     string   // Korean display name
	Aliases  []string // user input aliases
	LeagueID string   // LoL Esports API leagueId
}

var esportsLeagues = []EsportsLeague{
	newEsportsLeague("lck", "LCK", []string{"lck", "엘씨케이"}, "98767991310872058"),
	newEsportsLeague("lpl", "LPL", []string{"lpl", "엘피엘"}, "98767991314006698"),
	newEsportsLeague("lec", "LEC", []string{"lec", "엘이씨"}, "98767991302996019"),
	newEsportsLeague("lcs", "LCS", []string{"lcs", "엘씨에스"}, "98767991299243165"),
	newEsportsLeague("lcp", "LCP", []string{"lcp", "엘씨피"}, "113476371197627891"),
}

// EsportsLeagues returns a copy of all registered esports leagues.
func EsportsLeagues() []EsportsLeague {
	return cloneCatalog(esportsLeagues)
}

// LookupEsportsLeague resolves user input to an EsportsLeague.
func LookupEsportsLeague(input string) (EsportsLeague, bool) {
	return lookupCatalog(input, esportsLeagues, func(league EsportsLeague) (string, string, []string) {
		return league.ID, league.Name, league.Aliases
	})
}

func cloneCatalog[T any](items []T) []T {
	out := make([]T, len(items))
	copy(out, items)
	return out
}

func newFootballLeague(id, name string, aliases []string, espnSlug, livescoreName string, apiFootballID int, oddsAPISportKey string) FootballLeague {
	return FootballLeague{
		ID:              id,
		Name:            name,
		Aliases:         aliases,
		ESPNSlug:        espnSlug,
		LivescoreName:   livescoreName,
		APIFootballID:   apiFootballID,
		OddsAPISportKey: oddsAPISportKey,
	}
}

func newEsportsLeague(id, name string, aliases []string, leagueID string) EsportsLeague {
	return EsportsLeague{
		ID:       id,
		Name:     name,
		Aliases:  aliases,
		LeagueID: leagueID,
	}
}

func lookupCatalog[T any](input string, items []T, fields func(T) (string, string, []string)) (T, bool) {
	normalized := strings.TrimSpace(strings.ToLower(input))
	for _, item := range items {
		id, name, aliases := fields(item)
		if strings.ToLower(id) == normalized || strings.ToLower(name) == normalized {
			return item, true
		}
		for _, alias := range aliases {
			if strings.ToLower(alias) == normalized {
				return item, true
			}
		}
	}
	var zero T
	return zero, false
}
