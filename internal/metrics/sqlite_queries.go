package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *SQLiteStore) QueryOverview(ctx context.Context, now time.Time) (Overview, error) {
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UTC()
	last24 := now.Add(-24 * time.Hour).UTC()
	prev24Start := now.Add(-48 * time.Hour).UTC()
	prev24End := last24

	requestsToday, err := s.countEvents(ctx, `event_name = 'message_received' AND occurred_at >= ?`, timestampString(startOfDay))
	if err != nil {
		return Overview{}, err
	}
	totalCommands, failedCommands, err := s.commandStats(ctx, startOfDay, now.UTC(), "", "")
	if err != nil {
		return Overview{}, err
	}
	activeRooms, err := s.countDistinct(ctx, "room_id_hash", `event_name = 'message_received' AND occurred_at >= ?`, timestampString(startOfDay))
	if err != nil {
		return Overview{}, err
	}
	activeUsers, err := s.countDistinct(ctx, "user_id_hash", `event_name = 'message_received' AND occurred_at >= ?`, timestampString(startOfDay))
	if err != nil {
		return Overview{}, err
	}
	p95, _, err := s.latencyPercentile(ctx, startOfDay, now.UTC(), "", "")
	if err != nil {
		return Overview{}, err
	}
	currentCmds, currentFails, err := s.commandStats(ctx, last24, now.UTC(), "", "")
	if err != nil {
		return Overview{}, err
	}
	prevCmds, prevFails, err := s.commandStats(ctx, prev24Start, prev24End, "", "")
	if err != nil {
		return Overview{}, err
	}
	currentP95, _, err := s.latencyPercentile(ctx, last24, now.UTC(), "", "")
	if err != nil {
		return Overview{}, err
	}
	prevP95, _, err := s.latencyPercentile(ctx, prev24Start, prev24End, "", "")
	if err != nil {
		return Overview{}, err
	}
	currentDenied, err := s.countEvents(ctx, `event_name = 'access_denied' AND occurred_at >= ?`, timestampString(last24))
	if err != nil {
		return Overview{}, err
	}
	prevDenied, err := s.countEvents(ctx, `event_name = 'access_denied' AND occurred_at >= ? AND occurred_at < ?`, timestampString(prev24Start), timestampString(prev24End))
	if err != nil {
		return Overview{}, err
	}

	anomalies := detectOverviewAnomalies(overviewAnomalyInputs{
		currentCmds:   currentCmds,
		currentFails:  currentFails,
		prevCmds:      prevCmds,
		prevFails:     prevFails,
		currentP95:    currentP95,
		prevP95:       prevP95,
		currentDenied: currentDenied,
		prevDenied:    prevDenied,
	})

	return Overview{
		RequestsToday: requestsToday,
		ErrorRate:     ratio(failedCommands, totalCommands),
		P95LatencyMS:  p95,
		ActiveRooms:   activeRooms,
		ActiveUsers:   activeUsers,
		Anomalies:     anomalies,
	}, nil
}

type overviewAnomalyInputs struct {
	currentCmds   int64
	currentFails  int64
	prevCmds      int64
	prevFails     int64
	currentP95    int64
	prevP95       int64
	currentDenied int64
	prevDenied    int64
}

func detectOverviewAnomalies(inputs overviewAnomalyInputs) []Anomaly {
	anomalies := make([]Anomaly, 0, 3)
	currentErrorRate := ratio(inputs.currentFails, inputs.currentCmds)
	prevErrorRate := ratio(inputs.prevFails, inputs.prevCmds)
	if isErrorRateSpike(inputs.currentCmds, currentErrorRate, prevErrorRate) {
		anomalies = append(anomalies, Anomaly{
			Severity: "high",
			Title:    "에러율 급증",
			Detail:   fmt.Sprintf("최근 24시간 에러율 %.1f%%", currentErrorRate*100),
		})
	}
	if isLatencyRegression(inputs.currentP95, inputs.prevP95) {
		anomalies = append(anomalies, Anomaly{
			Severity: "medium",
			Title:    "응답 지연 악화",
			Detail:   fmt.Sprintf("최근 24시간 p95 %dms", inputs.currentP95),
		})
	}
	if isAccessDeniedSpike(inputs.currentDenied, inputs.prevDenied) {
		anomalies = append(anomalies, Anomaly{
			Severity: "medium",
			Title:    "ACL 차단 증가",
			Detail:   fmt.Sprintf("최근 24시간 차단 %d건", inputs.currentDenied),
		})
	}
	return anomalies
}

func isErrorRateSpike(currentCmds int64, currentErrorRate, prevErrorRate float64) bool {
	return currentCmds >= 10 && currentErrorRate >= 0.10 && currentErrorRate > prevErrorRate+0.05
}

func isLatencyRegression(currentP95, prevP95 int64) bool {
	return currentP95 >= 1500 && currentP95 > prevP95+500
}

func isAccessDeniedSpike(currentDenied, prevDenied int64) bool {
	return currentDenied >= 10 && currentDenied > prevDenied+5
}

func (s *SQLiteStore) QueryRooms(ctx context.Context, since, until time.Time, roomIDHash, commandID string, limit int) ([]RoomSummary, error) {
	args := []any{timestampString(since), timestampString(until)}
	filter := `occurred_at >= ? AND occurred_at < ?`
	if strings.TrimSpace(roomIDHash) != "" {
		filter += ` AND room_id_hash = ?`
		args = append(args, strings.TrimSpace(roomIDHash))
	}
	if strings.TrimSpace(commandID) != "" {
		filter += ` AND command_id = ?`
		args = append(args, strings.TrimSpace(commandID))
	}
	query := `
		SELECT
			room_id_hash,
			MAX(COALESCE(room_label, '')),
			MAX(COALESCE(room_name_snapshot, '')),
			SUM(CASE WHEN event_name = 'message_received' THEN 1 ELSE 0 END) AS requests,
			COUNT(DISTINCT CASE WHEN event_name = 'message_received' AND user_id_hash <> '' THEN user_id_hash END) AS active_users,
			SUM(CASE WHEN event_name = 'command_failed' THEN 1 ELSE 0 END) AS error_count,
			SUM(CASE WHEN event_name IN ('command_succeeded', 'command_failed') THEN 1 ELSE 0 END) AS completed_commands
		FROM metrics_events
		WHERE ` + filter + `
		GROUP BY room_id_hash
		HAVING requests > 0
		ORDER BY requests DESC
	`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.sdb.Read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rooms []RoomSummary
	for rows.Next() {
		var item RoomSummary
		var completed int64
		if err := rows.Scan(&item.RoomIDHash, &item.RoomLabel, &item.RoomNameSnapshot, &item.Requests, &item.ActiveUsers, &item.ErrorCount, &completed); err != nil {
			return nil, err
		}
		item.ErrorRate = ratio(item.ErrorCount, completed)
		rooms = append(rooms, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range rooms {
		trend, err := s.queryRoomTrend(ctx, rooms[i].RoomIDHash, since, until)
		if err != nil {
			return nil, err
		}
		rooms[i].Trend = trend
	}
	return rooms, nil
}

func (s *SQLiteStore) QueryRoomDetail(ctx context.Context, roomIDHash string, since, until time.Time, commandID string) (RoomDetail, error) {
	var detail RoomDetail
	roomIDHash = strings.TrimSpace(roomIDHash)
	if roomIDHash == "" {
		return detail, fmt.Errorf("room id hash is required")
	}
	args := []any{roomIDHash, timestampString(since), timestampString(until)}
	filter := `room_id_hash = ? AND occurred_at >= ? AND occurred_at < ?`
	if strings.TrimSpace(commandID) != "" {
		filter += ` AND command_id = ?`
		args = append(args, strings.TrimSpace(commandID))
	}
	row := s.sdb.Read.QueryRowContext(ctx, `
		SELECT
			room_id_hash,
			MAX(COALESCE(room_label, '')),
			MAX(COALESCE(room_name_snapshot, '')),
			SUM(CASE WHEN event_name = 'message_received' THEN 1 ELSE 0 END),
			COUNT(DISTINCT CASE WHEN event_name = 'message_received' AND user_id_hash <> '' THEN user_id_hash END),
			SUM(CASE WHEN event_name = 'command_failed' THEN 1 ELSE 0 END),
			SUM(CASE WHEN event_name IN ('command_succeeded', 'command_failed') AND command_source IN ('slash', 'explicit') THEN 1 ELSE 0 END),
			SUM(CASE WHEN event_name IN ('command_succeeded', 'command_failed') AND command_source IN ('auto', 'deterministic') THEN 1 ELSE 0 END),
			SUM(CASE WHEN event_name IN ('command_succeeded', 'command_failed') THEN 1 ELSE 0 END)
		FROM metrics_events
		WHERE `+filter, args...)
	var completed int64
	if err := row.Scan(
		&detail.RoomIDHash,
		&detail.RoomLabel,
		&detail.RoomNameSnapshot,
		&detail.Requests,
		&detail.ActiveUsers,
		&detail.ErrorCount,
		&detail.ExplicitCount,
		&detail.AutoCount,
		&completed,
	); err != nil {
		return detail, err
	}
	detail.ErrorRate = ratio(detail.ErrorCount, completed)

	hourly, err := s.queryRoomHourly(ctx, roomIDHash, since, until, commandID)
	if err != nil {
		return detail, err
	}
	detail.Hourly = hourly
	commands, err := s.queryRoomCommands(ctx, roomIDHash, since, until)
	if err != nil {
		return detail, err
	}
	detail.Commands = commands
	issues, err := s.queryRoomIssues(ctx, roomIDHash, since, until)
	if err != nil {
		return detail, err
	}
	detail.RecentIssues = issues
	return detail, nil
}

func (s *SQLiteStore) QueryReliability(ctx context.Context, since, until time.Time, roomIDHash, commandID string) (Reliability, error) {
	total, failed, err := s.commandStats(ctx, since, until, roomIDHash, commandID)
	if err != nil {
		return Reliability{}, err
	}
	p95, latencySamples, err := s.latencyPercentile(ctx, since, until, roomIDHash, commandID)
	if err != nil {
		return Reliability{}, err
	}
	filter, args := buildFilter(since, until, roomIDHash, commandID)
	rateLimited, err := s.countEvents(ctx, "event_name = 'rate_limited' AND "+filter, args...)
	if err != nil {
		return Reliability{}, err
	}
	accessDenied, err := s.countEvents(ctx, "event_name = 'access_denied' AND "+filter, args...)
	if err != nil {
		return Reliability{}, err
	}
	replyFailed, err := s.countEvents(ctx, "event_name = 'reply_failed' AND "+filter, args...)
	if err != nil {
		return Reliability{}, err
	}
	overload, err := s.countEvents(ctx, "event_name = 'transport_overload' AND "+filter, args...)
	if err != nil {
		return Reliability{}, err
	}
	rows, err := s.sdb.Read.QueryContext(ctx, `
		SELECT error_class, COUNT(*)
		FROM metrics_events
		WHERE error_class <> '' AND `+filter+`
		GROUP BY error_class
		ORDER BY COUNT(*) DESC`, args...)
	if err != nil {
		return Reliability{}, err
	}
	defer rows.Close()
	var breakdown []ErrorBreakdown
	for rows.Next() {
		var item ErrorBreakdown
		if err := rows.Scan(&item.ErrorClass, &item.Count); err != nil {
			return Reliability{}, err
		}
		breakdown = append(breakdown, item)
	}
	return Reliability{
		TotalCommands:     total,
		FailedCommands:    failed,
		ErrorRate:         ratio(failed, total),
		P95LatencyMS:      p95,
		LatencySamples:    latencySamples,
		RateLimitedCount:  rateLimited,
		AccessDeniedCount: accessDenied,
		ReplyFailedCount:  replyFailed,
		OverloadCount:     overload,
		ErrorsByClass:     breakdown,
	}, nil
}

func (s *SQLiteStore) QueryFeatureUsage(ctx context.Context, since, until time.Time, roomIDHash string) ([]CommandUsage, error) {
	filter, args := buildFilter(since, until, roomIDHash, "")
	rows, err := s.sdb.Read.QueryContext(ctx, `
		SELECT
			command_id,
			SUM(CASE WHEN event_name IN ('command_succeeded', 'command_failed') THEN 1 ELSE 0 END) AS requests,
			SUM(CASE WHEN event_name = 'command_failed' THEN 1 ELSE 0 END) AS errors
		FROM metrics_events
		WHERE command_id <> '' AND `+filter+`
		GROUP BY command_id
		HAVING requests > 0
		ORDER BY requests DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var usage []CommandUsage
	for rows.Next() {
		var item CommandUsage
		if err := rows.Scan(&item.CommandID, &item.Requests, &item.Errors); err != nil {
			return nil, err
		}
		usage = append(usage, item)
	}
	return usage, rows.Err()
}

func (s *SQLiteStore) QueryCoupangRefreshStats(ctx context.Context, since, until time.Time, roomIDHash string) (CoupangFeatureStats, error) {
	filter, args := buildFilter(since, until, roomIDHash, "쿠팡")
	successCount, err := s.countEvents(ctx, "event_name = 'coupang_refresh_succeeded' AND "+filter, args...)
	if err != nil {
		return CoupangFeatureStats{}, err
	}
	failureCount, err := s.countEvents(ctx, "event_name = 'coupang_refresh_failed' AND "+filter, args...)
	if err != nil {
		return CoupangFeatureStats{}, err
	}

	stats := CoupangFeatureStats{
		RefreshSuccessCount: successCount,
		RefreshFailureCount: failureCount,
	}

	joinMetadata, err := s.queryMetadataRows(ctx, `
		SELECT metadata_json
		FROM metrics_events
		WHERE event_name = 'coupang_lookup_coalesced' AND `+filter, args...)
	if err != nil {
		return CoupangFeatureStats{}, err
	}
	aggregateCoupangJoinStats(&stats, decodeCoupangJoinMetadata(joinMetadata))

	registrationMetadata, err := s.queryMetadataRows(ctx, `
		SELECT metadata_json
		FROM metrics_events
		WHERE event_name = 'coupang_registration_path' AND `+filter, args...)
	if err != nil {
		return CoupangFeatureStats{}, err
	}
	aggregateCoupangRegistrationStats(&stats, decodeCoupangRegistrationMetadata(registrationMetadata))

	compositeFilter, compositeArgs := buildFilter(since, until, roomIDHash, "")
	compositeMetadata, err := s.queryMetadataRows(ctx, `
		SELECT metadata_json
		FROM metrics_events
		WHERE event_name = 'reply_composite_outcome'
		  AND feature_key = 'coupang'
		  AND `+compositeFilter, compositeArgs...)
	if err != nil {
		return CoupangFeatureStats{}, err
	}
	aggregateCoupangCompositeStats(&stats, decodeCoupangCompositeMetadata(compositeMetadata))

	stats.PartialRatio = ratio(stats.PartialCount, stats.CompositeTotalCount)
	stats.JoinTimeoutRatio = ratio(stats.JoinTimeoutCount, stats.JoinTotalCount)

	return stats, nil
}

type coupangJoinObservation struct {
	coalescedHit bool
	joinTimeout  bool
}

type coupangRegistrationObservation struct {
	stage                string
	budgetExceeded       bool
	seedDeferred         bool
	rescueDeferred       bool
	registrationDeferred bool
}

type coupangCompositeObservation struct {
	deliveryOutcome     string
	chartFinalMode      string
	chartDecisionReason string
}

func (s *SQLiteStore) queryMetadataRows(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := s.sdb.Read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metadataRows []string
	for rows.Next() {
		var metadataJSON string
		if err := rows.Scan(&metadataJSON); err != nil {
			return nil, err
		}
		metadataRows = append(metadataRows, metadataJSON)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return metadataRows, nil
}

func decodeCoupangJoinMetadata(rows []string) []coupangJoinObservation {
	if len(rows) == 0 {
		return nil
	}
	observations := make([]coupangJoinObservation, 0, len(rows))
	for _, raw := range rows {
		metadata := parseMetadataJSON(raw)
		observations = append(observations, coupangJoinObservation{
			coalescedHit: metadataBoolValue(metadata["coalesced_hit"]),
			joinTimeout:  metadataBoolValue(metadata["join_timeout"]),
		})
	}
	return observations
}

func aggregateCoupangJoinStats(stats *CoupangFeatureStats, observations []coupangJoinObservation) {
	for _, observation := range observations {
		if !observation.coalescedHit {
			continue
		}
		stats.JoinTotalCount++
		if observation.joinTimeout {
			stats.JoinTimeoutCount++
		}
	}
}

func decodeCoupangRegistrationMetadata(rows []string) []coupangRegistrationObservation {
	if len(rows) == 0 {
		return nil
	}
	observations := make([]coupangRegistrationObservation, 0, len(rows))
	for _, raw := range rows {
		metadata := parseMetadataJSON(raw)
		observations = append(observations, coupangRegistrationObservation{
			stage:                metadataStringValue(metadata["registration_stage"]),
			budgetExceeded:       metadataBoolValue(metadata["budget_exhausted"]),
			seedDeferred:         metadataBoolValue(metadata["seed_deferred"]),
			rescueDeferred:       metadataBoolValue(metadata["rescue_deferred"]),
			registrationDeferred: metadataBoolValue(metadata["registration_deferred"]),
		})
	}
	return observations
}

func aggregateCoupangRegistrationStats(stats *CoupangFeatureStats, observations []coupangRegistrationObservation) {
	for _, observation := range observations {
		switch observation.stage {
		case "direct_resolved", "rescue_resolved", "deferred":
			stats.RegistrationCount++
		}
		if observation.registrationDeferred {
			stats.DeferredCount++
		}
		if observation.budgetExceeded {
			stats.BudgetExceededCount++
		}
		if observation.seedDeferred {
			stats.SeedDeferredCount++
		}
		if observation.rescueDeferred {
			stats.RescueDeferredCount++
		}
	}
}

func decodeCoupangCompositeMetadata(rows []string) []coupangCompositeObservation {
	if len(rows) == 0 {
		return nil
	}
	observations := make([]coupangCompositeObservation, 0, len(rows))
	for _, raw := range rows {
		metadata := parseMetadataJSON(raw)
		observations = append(observations, coupangCompositeObservation{
			deliveryOutcome:     metadataStringValue(metadata["composite_delivery_outcome"]),
			chartFinalMode:      metadataStringValue(metadata["chart_final_mode"]),
			chartDecisionReason: normalizeChartDecisionReason(metadataStringValue(metadata["chart_decision_reason"])),
		})
	}
	return observations
}

func aggregateCoupangCompositeStats(stats *CoupangFeatureStats, observations []coupangCompositeObservation) {
	reasons := make(map[string]int64)
	for _, observation := range observations {
		stats.CompositeTotalCount++
		if observation.deliveryOutcome == "partial_success" {
			stats.PartialCount++
		}
		if observation.chartFinalMode != "text_only_skipped" {
			continue
		}
		stats.ChartSkippedCount++
		reasons[observation.chartDecisionReason]++
	}
	stats.ChartSkipReasons = mapToBreakdown(reasons)
}

func normalizeChartDecisionReason(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return "unknown"
	}
	return reason
}

func mapToBreakdown(values map[string]int64) []ErrorBreakdown {
	if len(values) == 0 {
		return nil
	}
	out := make([]ErrorBreakdown, 0, len(values))
	for key, count := range values {
		out = append(out, ErrorBreakdown{ErrorClass: key, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].ErrorClass < out[j].ErrorClass
		}
		return out[i].Count > out[j].Count
	})
	return out
}

func parseMetadataJSON(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := make(map[string]any)
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func metadataStringValue(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func metadataBoolValue(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case float64:
		return v != 0
	case int64:
		return v != 0
	case int:
		return v != 0
	case string:
		normalized := strings.ToLower(strings.TrimSpace(v))
		return normalized == "true" || normalized == "1"
	default:
		return false
	}
}

func (s *SQLiteStore) QueryProductFunnel(
	ctx context.Context,
	since, until time.Time,
	audience, tenantIDHash, roomScopeHash, featureKey string,
) ([]ProductFunnelPoint, error) {
	filter, args := buildProductFilter(since, until, audience, tenantIDHash, roomScopeHash, featureKey)
	rows, err := s.sdb.Read.QueryContext(ctx, `
		SELECT stage, SUM(event_count)
		FROM metrics_product_funnel_rollups
		WHERE `+filter+`
		GROUP BY stage
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stageCounts := make(map[string]int64)
	for rows.Next() {
		var stage string
		var count int64
		if err := rows.Scan(&stage, &count); err != nil {
			return nil, err
		}
		stageCounts[strings.TrimSpace(stage)] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	stages := []string{"acquisition", "activation", "engagement", "conversion", "retention_return", "churn_signal"}
	out := make([]ProductFunnelPoint, 0, len(stages))
	var prev int64
	first := true
	for _, stage := range stages {
		count := stageCounts[stage]
		item := ProductFunnelPoint{
			Stage: stage,
			Count: count,
		}
		if first {
			item.ConversionRate = 1
			item.DropoffRate = 0
			first = false
		} else if prev > 0 {
			item.ConversionRate = float64(count) / float64(prev)
			item.DropoffRate = 1 - item.ConversionRate
		}
		out = append(out, item)
		prev = count
	}
	return out, nil
}

func (s *SQLiteStore) QueryProductCohorts(
	ctx context.Context,
	since, until time.Time,
	audience, tenantIDHash, roomScopeHash, featureKey string,
) ([]ProductCohortPoint, error) {
	filter, args := buildProductEventFilter(since, until, audience, tenantIDHash, roomScopeHash, featureKey)
	rows, err := s.sdb.Read.QueryContext(ctx, `
		SELECT
			substr(occurred_at, 1, 10) AS cohort_date,
			COUNT(DISTINCT user_id_hash) AS activation_users
		FROM metrics_events
		WHERE event_name = 'activation'
		  AND user_id_hash <> ''
		  AND `+filter+`
		GROUP BY cohort_date
		ORDER BY cohort_date ASC
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProductCohortPoint
	for rows.Next() {
		var item ProductCohortPoint
		if err := rows.Scan(&item.CohortDate, &item.ActivationUsers); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) QueryProductRetention(
	ctx context.Context,
	since, until time.Time,
	audience, tenantIDHash, roomScopeHash, featureKey string,
) ([]ProductRetentionPoint, error) {
	filter, args := buildProductRetentionFilter(since, until, audience, tenantIDHash, roomScopeHash, featureKey)
	rows, err := s.sdb.Read.QueryContext(ctx, `
		SELECT
			cohort_date,
			bucket_date,
			cohort_size,
			retained_users
		FROM metrics_product_retention_rollups
		WHERE `+filter+`
		ORDER BY cohort_date ASC, bucket_date ASC
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProductRetentionPoint
	for rows.Next() {
		var item ProductRetentionPoint
		if err := rows.Scan(&item.CohortDate, &item.BucketDate, &item.CohortSize, &item.RetainedUsers); err != nil {
			return nil, err
		}
		item.RetentionRate = ratio(item.RetainedUsers, item.CohortSize)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) countEvents(ctx context.Context, where string, args ...any) (int64, error) {
	var count int64
	err := s.sdb.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM metrics_events WHERE `+where, args...).Scan(&count)
	return count, err
}

func (s *SQLiteStore) countDistinct(ctx context.Context, column, where string, args ...any) (int64, error) {
	var count int64
	query := fmt.Sprintf(`SELECT COUNT(DISTINCT CASE WHEN %s <> '' THEN %s END) FROM metrics_events WHERE %s`, column, column, where)
	err := s.sdb.Read.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

func (s *SQLiteStore) commandStats(ctx context.Context, since, until time.Time, roomIDHash, commandID string) (int64, int64, error) {
	filter, args := buildFilter(since, until, roomIDHash, commandID)
	var total int64
	if err := s.sdb.Read.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM metrics_events
		WHERE event_name IN ('command_succeeded', 'command_failed') AND `+filter, args...).Scan(&total); err != nil {
		return 0, 0, err
	}
	var failed int64
	if err := s.sdb.Read.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM metrics_events
		WHERE event_name = 'command_failed' AND `+filter, args...).Scan(&failed); err != nil {
		return 0, 0, err
	}
	return total, failed, nil
}

func (s *SQLiteStore) latencyPercentile(ctx context.Context, since, until time.Time, roomIDHash, commandID string) (int64, int64, error) {
	filter, args := buildFilter(since, until, roomIDHash, commandID)
	rows, err := s.sdb.Read.QueryContext(ctx, `
		SELECT latency_ms
		FROM metrics_events
		WHERE latency_ms > 0
		  AND event_name IN ('command_succeeded', 'command_failed', 'reply_sent', 'reply_failed')
		  AND `+filter, args...)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	var values []int
	for rows.Next() {
		var value int
		if err := rows.Scan(&value); err != nil {
			return 0, 0, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	if len(values) == 0 {
		return 0, 0, nil
	}
	sort.Ints(values)
	index := int(float64(len(values)-1) * 0.95)
	return int64(values[index]), int64(len(values)), nil
}

func (s *SQLiteStore) queryRoomTrend(ctx context.Context, roomIDHash string, since, until time.Time) ([]TrendPoint, error) {
	rows, err := s.sdb.Read.QueryContext(ctx, `
		SELECT bucket_date, SUM(request_count)
		FROM metrics_daily_rollups
		WHERE room_id_hash = ? AND bucket_date >= ? AND bucket_date <= ?
		GROUP BY bucket_date
		ORDER BY bucket_date ASC`, roomIDHash, dateString(since), dateString(until))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var trend []TrendPoint
	for rows.Next() {
		var point TrendPoint
		if err := rows.Scan(&point.Bucket, &point.Count); err != nil {
			return nil, err
		}
		trend = append(trend, point)
	}
	return trend, rows.Err()
}

func (s *SQLiteStore) queryRoomHourly(ctx context.Context, roomIDHash string, since, until time.Time, commandID string) ([]TrendPoint, error) {
	args := []any{roomIDHash, hourBucketString(since), timestampString(until)}
	query := `
		SELECT bucket_at, SUM(request_count)
		FROM metrics_hourly_rollups
		WHERE room_id_hash = ? AND bucket_at >= ? AND bucket_at < ?
	`
	if strings.TrimSpace(commandID) != "" {
		query += ` AND command_id = ?`
		args = append(args, strings.TrimSpace(commandID))
	}
	query += ` GROUP BY bucket_at ORDER BY bucket_at ASC`
	rows, err := s.sdb.Read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var trend []TrendPoint
	for rows.Next() {
		var point TrendPoint
		if err := rows.Scan(&point.Bucket, &point.Count); err != nil {
			return nil, err
		}
		trend = append(trend, point)
	}
	return trend, rows.Err()
}

func (s *SQLiteStore) queryRoomCommands(ctx context.Context, roomIDHash string, since, until time.Time) ([]CommandUsage, error) {
	rows, err := s.sdb.Read.QueryContext(ctx, `
		SELECT
			command_id,
			SUM(CASE WHEN event_name IN ('command_succeeded', 'command_failed') THEN 1 ELSE 0 END) AS requests,
			SUM(CASE WHEN event_name = 'command_failed' THEN 1 ELSE 0 END) AS errors
		FROM metrics_events
		WHERE room_id_hash = ? AND occurred_at >= ? AND occurred_at < ? AND command_id <> ''
		GROUP BY command_id
		HAVING requests > 0
		ORDER BY requests DESC
	`, roomIDHash, timestampString(since), timestampString(until))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var usage []CommandUsage
	for rows.Next() {
		var item CommandUsage
		if err := rows.Scan(&item.CommandID, &item.Requests, &item.Errors); err != nil {
			return nil, err
		}
		usage = append(usage, item)
	}
	return usage, rows.Err()
}

func (s *SQLiteStore) queryRoomIssues(ctx context.Context, roomIDHash string, since, until time.Time) ([]IssueEvent, error) {
	rows, err := s.sdb.Read.QueryContext(ctx, `
		SELECT occurred_at, event_name, command_id, error_class, metadata_json
		FROM metrics_events
		WHERE room_id_hash = ?
		  AND occurred_at >= ?
		  AND occurred_at < ?
		  AND event_name IN ('command_failed', 'access_denied', 'policy_skip', 'unmatched_message', 'rate_limited', 'reply_failed', 'coupang_refresh_failed', 'transport_overload')
		ORDER BY occurred_at DESC
		LIMIT 20
	`, roomIDHash, timestampString(since), timestampString(until))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var issues []IssueEvent
	for rows.Next() {
		var occurredAt string
		var metadataJSON string
		var issue IssueEvent
		if err := rows.Scan(&occurredAt, &issue.EventName, &issue.CommandID, &issue.ErrorClass, &metadataJSON); err != nil {
			return nil, err
		}
		issue.OccurredAt = occurredAt
		issue.Detail = summarizeIssueDetail(issue.EventName, metadataJSON)
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

func summarizeIssueDetail(eventName, metadataJSON string) string {
	metadataJSON = strings.TrimSpace(metadataJSON)
	if metadataJSON == "" || metadataJSON == "{}" {
		return ""
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return ""
	}

	switch strings.TrimSpace(eventName) {
	case string(EventTransportOverload):
		reason := firstNonEmptyMetadataString(metadata, "drop_reason", "reason")
		queueLen := firstNonEmptyMetadataString(metadata, "queue_len")
		if reason == "" && queueLen == "" {
			return ""
		}
		if queueLen == "" {
			return fmt.Sprintf("drop_reason=%s", reason)
		}
		if reason == "" {
			return fmt.Sprintf("queue_len=%s", queueLen)
		}
		return fmt.Sprintf("drop_reason=%s queue_len=%s", reason, queueLen)
	default:
		errorText := firstNonEmptyMetadataString(metadata, "error", "message", "reason")
		if errorText == "" {
			return ""
		}
		return truncateIssueDetail(errorText, 240)
	}
}

func firstNonEmptyMetadataString(metadata map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := metadataStringValue(metadata[key]); value != "" {
			return value
		}
	}
	return ""
}

func truncateIssueDetail(value string, limit int) string {
	value = sanitizeIssueText(value)
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	return string([]rune(value)[:limit]) + "..."
}

func sanitizeIssueText(value string) string {
	replacer := strings.NewReplacer("<", "(", ">", ")", "\n", " ", "\r", " ", "\t", " ")
	value = replacer.Replace(strings.TrimSpace(value))
	return strings.Join(strings.Fields(value), " ")
}

func buildFilter(since, until time.Time, roomIDHash, commandID string) (string, []any) {
	filter := "occurred_at >= ? AND occurred_at < ?"
	args := []any{timestampString(since), timestampString(until)}
	if strings.TrimSpace(roomIDHash) != "" {
		filter += " AND room_id_hash = ?"
		args = append(args, strings.TrimSpace(roomIDHash))
	}
	if strings.TrimSpace(commandID) != "" {
		filter += " AND command_id = ?"
		args = append(args, strings.TrimSpace(commandID))
	}
	return filter, args
}

func buildProductFilter(since, until time.Time, audience, tenantIDHash, roomScopeHash, featureKey string) (string, []any) {
	filter := "bucket_date >= ? AND bucket_date <= ?"
	args := []any{dateString(since), dateString(until)}
	return appendProductDimensionFilters(filter, args, audience, tenantIDHash, roomScopeHash, featureKey)
}

func buildProductRetentionFilter(since, until time.Time, audience, tenantIDHash, roomScopeHash, featureKey string) (string, []any) {
	return buildProductFilter(since, until, audience, tenantIDHash, roomScopeHash, featureKey)
}

func buildProductEventFilter(since, until time.Time, audience, tenantIDHash, roomScopeHash, featureKey string) (string, []any) {
	filter := "occurred_at >= ? AND occurred_at < ?"
	args := []any{timestampString(since), timestampString(until)}
	return appendProductDimensionFilters(filter, args, audience, tenantIDHash, roomScopeHash, featureKey)
}

func appendProductDimensionFilters(filter string, args []any, audience, tenantIDHash, roomScopeHash, featureKey string) (string, []any) {
	if strings.TrimSpace(audience) != "" {
		filter += filterAudienceClause
		args = append(args, strings.TrimSpace(audience))
	}
	if strings.TrimSpace(tenantIDHash) != "" {
		filter += filterTenantClause
		args = append(args, strings.TrimSpace(tenantIDHash))
	}
	if strings.TrimSpace(roomScopeHash) != "" {
		filter += filterRoomScopeClause
		args = append(args, strings.TrimSpace(roomScopeHash))
	}
	if strings.TrimSpace(featureKey) != "" {
		filter += filterFeatureKeyClause
		args = append(args, strings.TrimSpace(featureKey))
	}
	return filter, args
}
