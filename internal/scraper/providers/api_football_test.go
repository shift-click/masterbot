package providers

import (
	"testing"
	"time"
)

func TestAPIFootballBudgetTracking(t *testing.T) {
	af := NewAPIFootball("test-key", nil)

	if af.DailyBudgetRemaining() != 100 {
		t.Errorf("initial budget = %d, want 100", af.DailyBudgetRemaining())
	}

	// Simulate usage
	af.mu.Lock()
	af.dailyUsed = 99
	af.mu.Unlock()

	if af.DailyBudgetRemaining() != 1 {
		t.Errorf("after 99 used, remaining = %d, want 1", af.DailyBudgetRemaining())
	}

	// Simulate day change
	af.mu.Lock()
	af.lastReset = time.Now().UTC().AddDate(0, 0, -1)
	af.mu.Unlock()

	// After day change, resetIfNewDay should reset (via DailyBudgetRemaining)
	// DailyBudgetRemaining internally calls resetIfNewDay

	if af.DailyBudgetRemaining() != 100 {
		t.Errorf("after day reset, remaining = %d, want 100", af.DailyBudgetRemaining())
	}
}
