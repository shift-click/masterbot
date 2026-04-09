package fortune

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

type DailyFortune struct {
	DateKey string
	Index   int
	Text    string
}

type Service struct {
	presets []string
}

func NewService(presets []string) (*Service, error) {
	if len(presets) == 0 {
		return nil, fmt.Errorf("fortune presets are required")
	}
	cloned := append([]string(nil), presets...)
	return &Service{presets: cloned}, nil
}

func (s *Service) Today(_ context.Context, userID string, now time.Time) (DailyFortune, error) {
	if s == nil || len(s.presets) == 0 {
		return DailyFortune{}, fmt.Errorf("fortune service is not initialized")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return DailyFortune{}, fmt.Errorf("fortune user id is required")
	}

	dateKey := kstDateKey(now)
	index := deterministicFortuneIndex(dateKey, userID, len(s.presets))
	return DailyFortune{
		DateKey: dateKey,
		Index:   index,
		Text:    s.presets[index],
	}, nil
}

func deterministicFortuneIndex(dateKey, userID string, total int) int {
	if total <= 0 {
		return 0
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(dateKey) + ":" + strings.TrimSpace(userID)))
	value := binary.BigEndian.Uint64(sum[:8])
	return int(value % uint64(total))
}

func kstDateKey(now time.Time) string {
	return now.In(kstLocation()).Format("060102")
}

func kstLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil || loc == nil {
		return time.FixedZone("KST", 9*60*60)
	}
	return loc
}
