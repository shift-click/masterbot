package fortune

import (
	"context"
	"testing"
	"time"
)

func TestServiceTodayDeterministicWithinSameDay(t *testing.T) {
	t.Parallel()

	service, err := NewService([]string{"첫 번째", "두 번째", "세 번째"})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	kst := time.FixedZone("KST", 9*60*60)
	morning := time.Date(2026, 4, 9, 9, 0, 0, 0, kst)
	evening := time.Date(2026, 4, 9, 21, 30, 0, 0, kst)

	first, err := service.Today(context.Background(), "user-1", morning)
	if err != nil {
		t.Fatalf("Today() morning error = %v", err)
	}
	second, err := service.Today(context.Background(), "user-1", evening)
	if err != nil {
		t.Fatalf("Today() evening error = %v", err)
	}

	if first.Index != second.Index || first.Text != second.Text {
		t.Fatalf("fortune changed within same day: %+v vs %+v", first, second)
	}
}

func TestServiceTodayChangesByDateOrUser(t *testing.T) {
	t.Parallel()

	service, err := NewService([]string{"첫 번째", "두 번째", "세 번째", "네 번째", "다섯 번째"})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	kst := time.FixedZone("KST", 9*60*60)
	dayOne := time.Date(2026, 4, 9, 9, 0, 0, 0, kst)
	dayTwo := time.Date(2026, 4, 10, 9, 0, 0, 0, kst)

	first, err := service.Today(context.Background(), "user-1", dayOne)
	if err != nil {
		t.Fatalf("Today() dayOne error = %v", err)
	}
	otherDay, err := service.Today(context.Background(), "user-1", dayTwo)
	if err != nil {
		t.Fatalf("Today() dayTwo error = %v", err)
	}
	otherUser, err := service.Today(context.Background(), "user-2", dayOne)
	if err != nil {
		t.Fatalf("Today() otherUser error = %v", err)
	}

	if first.DateKey == otherDay.DateKey && first.Index == otherDay.Index {
		t.Fatalf("expected different day to affect result: %+v vs %+v", first, otherDay)
	}
	if first.Index == otherUser.Index && first.Text == otherUser.Text && first.DateKey == otherUser.DateKey {
		t.Fatalf("expected different user to affect result: %+v vs %+v", first, otherUser)
	}
}

func TestDeterministicFortuneIndexHandlesEdgeCases(t *testing.T) {
	t.Parallel()

	if got := deterministicFortuneIndex("260409", "user-1", 0); got != 0 {
		t.Fatalf("index with zero total = %d, want 0", got)
	}
	if got := deterministicFortuneIndex("260409", "user-1", 1); got != 0 {
		t.Fatalf("index with total 1 = %d, want 0", got)
	}
}
