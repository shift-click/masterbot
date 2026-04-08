package providers

import "testing"

func TestLookupFootballLeague(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{"EPL", "epl", true},
		{"epl", "epl", true},
		{"프리미어리그", "epl", true},
		{"챔스", "ucl", true},
		{"UCL", "ucl", true},
		{"K리그", "kleague", true},
		{"k리그", "kleague", true},
		{"분데스", "bundesliga", true},
		{"세리에", "seriea", true},
		{"라리가", "laliga", true},
		{"없는리그", "", false},
	}

	for _, tt := range tests {
		league, ok := LookupFootballLeague(tt.input)
		if ok != tt.ok {
			t.Errorf("LookupFootballLeague(%q) ok = %v, want %v", tt.input, ok, tt.ok)
			continue
		}
		if ok && league.ID != tt.want {
			t.Errorf("LookupFootballLeague(%q) = %q, want %q", tt.input, league.ID, tt.want)
		}
	}
}

func TestLookupEsportsLeague(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{"LCK", "lck", true},
		{"lck", "lck", true},
		{"LPL", "lpl", true},
		{"LEC", "lec", true},
		{"LCS", "lcs", true},
		{"LCP", "lcp", true},
		{"없는리그", "", false},
	}

	for _, tt := range tests {
		league, ok := LookupEsportsLeague(tt.input)
		if ok != tt.ok {
			t.Errorf("LookupEsportsLeague(%q) ok = %v, want %v", tt.input, ok, tt.ok)
			continue
		}
		if ok && league.ID != tt.want {
			t.Errorf("LookupEsportsLeague(%q) = %q, want %q", tt.input, league.ID, tt.want)
		}
	}
}

func TestFootballLeaguesCount(t *testing.T) {
	leagues := FootballLeagues()
	if len(leagues) != 6 {
		t.Errorf("FootballLeagues() returned %d leagues, want 6", len(leagues))
	}
}

func TestEsportsLeaguesCount(t *testing.T) {
	leagues := EsportsLeagues()
	if len(leagues) != 5 {
		t.Errorf("EsportsLeagues() returned %d leagues, want 5", len(leagues))
	}
}
