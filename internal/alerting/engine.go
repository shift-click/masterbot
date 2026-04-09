package alerting

import (
	"fmt"
	"time"

	"github.com/shift-click/masterbot/internal/metrics"
)

type Thresholds struct {
	ErrorRateThreshold float64
	P95ThresholdMS     int64
	MinCommands        int64
}

type Snapshot struct {
	Now      time.Time
	Critical metrics.Reliability
	Warning  metrics.Reliability
}

type Alert struct {
	Key      string
	Severity string
	Title    string
	Detail   string
}

func Evaluate(snapshot Snapshot, thresholds Thresholds) []Alert {
	if thresholds.MinCommands <= 0 {
		thresholds.MinCommands = 10
	}
	if thresholds.ErrorRateThreshold <= 0 {
		thresholds.ErrorRateThreshold = 0.05
	}
	if thresholds.P95ThresholdMS <= 0 {
		thresholds.P95ThresholdMS = 1500
	}

	alerts := make([]Alert, 0, 3)

	if snapshot.Critical.ReplyFailedCount > 0 {
		alerts = append(alerts, Alert{
			Key:      "critical_reply_failed",
			Severity: "critical",
			Title:    "Reply 전송 실패 감지",
			Detail:   fmt.Sprintf("최근 3분 reply_failed=%d", snapshot.Critical.ReplyFailedCount),
		})
	}

	if snapshot.Critical.TotalCommands >= thresholds.MinCommands && snapshot.Critical.ErrorRate >= thresholds.ErrorRateThreshold {
		alerts = append(alerts, Alert{
			Key:      "critical_error_rate",
			Severity: "critical",
			Title:    "명령 실패율 임계치 초과",
			Detail: fmt.Sprintf(
				"최근 3분 error_rate=%.1f%% (%d/%d)",
				snapshot.Critical.ErrorRate*100,
				snapshot.Critical.FailedCommands,
				snapshot.Critical.TotalCommands,
			),
		})
	}

	if snapshot.Warning.TotalCommands >= thresholds.MinCommands && snapshot.Warning.P95LatencyMS >= thresholds.P95ThresholdMS {
		alerts = append(alerts, Alert{
			Key:      "warning_high_latency",
			Severity: "warning",
			Title:    "지연시간 임계치 초과",
			Detail: fmt.Sprintf(
				"최근 10분 p95=%dms (기준=%dms)",
				snapshot.Warning.P95LatencyMS,
				thresholds.P95ThresholdMS,
			),
		})
	}

	return alerts
}
