package alerting

import (
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/metrics"
)

func TestEvaluate(t *testing.T) {
	t.Run("critical when reply failures exist", func(t *testing.T) {
		alerts := Evaluate(Snapshot{
			Now: time.Now(),
			Critical: metrics.Reliability{
				TotalCommands:    1,
				ReplyFailedCount: 1,
			},
			Warning: metrics.Reliability{},
		}, Thresholds{
			ErrorRateThreshold: 0.05,
			P95ThresholdMS:     1500,
			MinCommands:        10,
		})
		if len(alerts) != 1 || alerts[0].Key != "critical_reply_failed" {
			t.Fatalf("expected reply_failed critical alert, got %+v", alerts)
		}
	})

	t.Run("critical when error rate exceeds threshold", func(t *testing.T) {
		alerts := Evaluate(Snapshot{
			Now: time.Now(),
			Critical: metrics.Reliability{
				TotalCommands:  20,
				FailedCommands: 3,
				ErrorRate:      0.15,
			},
		}, Thresholds{
			ErrorRateThreshold: 0.05,
			P95ThresholdMS:     1500,
			MinCommands:        10,
		})
		if len(alerts) != 1 || alerts[0].Key != "critical_error_rate" {
			t.Fatalf("expected critical_error_rate alert, got %+v", alerts)
		}
	})

	t.Run("warning when p95 exceeds threshold", func(t *testing.T) {
		alerts := Evaluate(Snapshot{
			Now: time.Now(),
			Warning: metrics.Reliability{
				TotalCommands: 20,
				P95LatencyMS:  1800,
			},
		}, Thresholds{
			ErrorRateThreshold: 0.05,
			P95ThresholdMS:     1500,
			MinCommands:        10,
		})
		if len(alerts) != 1 || alerts[0].Key != "warning_high_latency" {
			t.Fatalf("expected warning_high_latency alert, got %+v", alerts)
		}
	})
}

func TestDeduperAllow(t *testing.T) {
	now := time.Now()
	d := NewDeduper(15 * time.Minute)

	if !d.Allow("critical_reply_failed", now) {
		t.Fatalf("first alert should be allowed")
	}
	if d.Allow("critical_reply_failed", now.Add(10*time.Minute)) {
		t.Fatalf("duplicate alert within window should be blocked")
	}
	if !d.Allow("critical_reply_failed", now.Add(16*time.Minute)) {
		t.Fatalf("alert after dedup window should be allowed")
	}
}
