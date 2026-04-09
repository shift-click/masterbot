package command

import "testing"

func TestLookupLocation(t *testing.T) {
	tests := []struct {
		input     string
		wantOK    bool
		wantShort string
	}{
		{"강남구", true, "강남구"},
		{"강남", true, "강남구"},
		{"서울", true, "서울"},
		{"부산", true, "부산"},
		{"제주", true, "제주"},
		{"강릉", true, "강릉"},
		{"의정부", true, "의정부"},
		{"서귀포", true, "서귀포"},
		{"종로구", true, "종로구"},
		{"종로", true, "종로구"},
		{"동대문구", true, "동대문구"},
		{"동대문", true, "동대문구"},
		{"없는지역", false, ""},
		{"", false, ""},
		{"  서울  ", true, "서울"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			loc, ok := LookupLocation(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("LookupLocation(%q) ok=%v, want %v", tt.input, ok, tt.wantOK)
			}
			if ok && loc.ShortName != tt.wantShort {
				t.Errorf("LookupLocation(%q) ShortName=%q, want %q", tt.input, loc.ShortName, tt.wantShort)
			}
		})
	}
}

func TestNationalCities(t *testing.T) {
	cities := NationalCities()
	if len(cities) != 10 {
		t.Errorf("NationalCities() len=%d, want 10", len(cities))
	}
	// verify first and last
	if cities[0].ShortName != "서울" {
		t.Errorf("first city=%q, want 서울", cities[0].ShortName)
	}
	if cities[9].ShortName != "제주" {
		t.Errorf("last city=%q, want 제주", cities[9].ShortName)
	}
}
