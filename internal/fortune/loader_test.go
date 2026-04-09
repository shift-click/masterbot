package fortune

import "testing"

func TestLoadFortunes(t *testing.T) {
	t.Parallel()

	fortunes, err := LoadFortunes()
	if err != nil {
		t.Fatalf("LoadFortunes() error = %v", err)
	}
	if len(fortunes) != ExpectedFortuneCount {
		t.Fatalf("len(fortunes) = %d, want %d", len(fortunes), ExpectedFortuneCount)
	}
	if fortunes[0] == "" {
		t.Fatal("expected first fortune to be non-empty")
	}
}
