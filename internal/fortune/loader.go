package fortune

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed data/fortunes.json
var fortuneFS embed.FS

const (
	fortuneFile          = "data/fortunes.json"
	ExpectedFortuneCount = 5000
)

var (
	fortunesOnce sync.Once
	fortunesData []string
	fortunesErr  error
)

func LoadFortunes() ([]string, error) {
	fortunesOnce.Do(func() {
		raw, err := fortuneFS.ReadFile(fortuneFile)
		if err != nil {
			fortunesErr = fmt.Errorf("read embedded fortunes: %w", err)
			return
		}
		var parsed []string
		if err := json.Unmarshal(raw, &parsed); err != nil {
			fortunesErr = fmt.Errorf("parse embedded fortunes: %w", err)
			return
		}
		if err := validateFortunes(parsed); err != nil {
			fortunesErr = err
			return
		}
		fortunesData = append([]string(nil), parsed...)
	})
	if fortunesErr != nil {
		return nil, fortunesErr
	}
	return append([]string(nil), fortunesData...), nil
}

func validateFortunes(values []string) error {
	if len(values) == 0 {
		return fmt.Errorf("fortune presets are empty")
	}
	if len(values) != ExpectedFortuneCount {
		return fmt.Errorf("fortune preset count = %d, want %d", len(values), ExpectedFortuneCount)
	}
	for idx, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("fortune preset %d is empty", idx)
		}
	}
	return nil
}
