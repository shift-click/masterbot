package providers

import "strings"

// BaseballLeague defines a supported baseball league.
type BaseballLeague struct {
	ID      string   // internal identifier
	Name    string   // Korean display name
	Aliases []string // user input aliases (lowercase)
}

var baseballLeagues = []BaseballLeague{
	{
		ID:      "mlb",
		Name:    "MLB",
		Aliases: []string{"mlb", "엠엘비", "메이저리그", "미국야구"},
	},
	{
		ID:      "kbo",
		Name:    "KBO",
		Aliases: []string{"kbo", "케이비오", "프로야구", "한국야구"},
	},
	{
		ID:      "npb",
		Name:    "NPB",
		Aliases: []string{"npb", "엔피비", "일본야구", "일야"},
	},
}

// BaseballLeagues returns a copy of all registered baseball leagues.
func BaseballLeagues() []BaseballLeague {
	out := make([]BaseballLeague, len(baseballLeagues))
	copy(out, baseballLeagues)
	return out
}

// LookupBaseballLeague resolves user input to a BaseballLeague.
func LookupBaseballLeague(input string) (BaseballLeague, bool) {
	normalized := strings.TrimSpace(strings.ToLower(input))
	for _, league := range baseballLeagues {
		if strings.ToLower(league.ID) == normalized || strings.ToLower(league.Name) == normalized {
			return league, true
		}
		for _, alias := range league.Aliases {
			if strings.ToLower(alias) == normalized {
				return league, true
			}
		}
	}
	return BaseballLeague{}, false
}
