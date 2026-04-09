package metrics

import "time"

type EventName string

const (
	EventMessageReceived         EventName = "message_received"
	EventCommandDispatched       EventName = "command_dispatched"
	EventCommandSucceeded        EventName = "command_succeeded"
	EventCommandFailed           EventName = "command_failed"
	EventReplySent               EventName = "reply_sent"
	EventReplyFailed             EventName = "reply_failed"
	EventAccessDenied            EventName = "access_denied"
	EventUnmatchedMessage        EventName = "unmatched_message"
	EventPolicySkip              EventName = "policy_skip"
	EventRateLimited             EventName = "rate_limited"
	EventCoupangRefreshStarted   EventName = "coupang_refresh_started"
	EventCoupangRefreshSucceeded EventName = "coupang_refresh_succeeded"
	EventCoupangRefreshFailed    EventName = "coupang_refresh_failed"
	EventCoupangLookupCoalesced  EventName = "coupang_lookup_coalesced"
	EventCoupangRegistrationPath EventName = "coupang_registration_path"
	EventCoupangChartPipeline    EventName = "coupang_chart_pipeline"
	EventReplyCompositeOutcome   EventName = "reply_composite_outcome"
	EventTransportOverload       EventName = "transport_overload"
	EventAcquisition             EventName = "acquisition"
	EventActivation              EventName = "activation"
	EventEngagement              EventName = "engagement"
	EventConversion              EventName = "conversion"
	EventRetentionReturn         EventName = "retention_return"
	EventChurnSignal             EventName = "churn_signal"
)

type CommandSource string

const (
	CommandSourceSlash         CommandSource = "slash"
	CommandSourceExplicit      CommandSource = "explicit"
	CommandSourceAuto          CommandSource = "auto"
	CommandSourceDeterministic CommandSource = "deterministic"
	CommandSourceSystem        CommandSource = "system"
)

type Event struct {
	OccurredAt     time.Time
	RequestID      string
	EventName      EventName
	RawRoomID      string
	RawTenantID    string
	RawScopeRoomID string
	RoomName       string
	RawUserID      string
	CommandID      string
	CommandSource  CommandSource
	Audience       string
	FeatureKey     string
	Attribution    string
	Success        *bool
	ErrorClass     string
	Latency        time.Duration
	Denied         bool
	RateLimited    bool
	ReplyType      string
	Metadata       map[string]any
}

type StoredEvent struct {
	OccurredAt       time.Time
	RequestID        string
	EventName        string
	RoomIDHash       string
	TenantIDHash     string
	RoomScopeHash    string
	RoomLabel        string
	RoomNameSnapshot string
	UserIDHash       string
	CommandID        string
	CommandSource    string
	Audience         string
	FeatureKey       string
	Attribution      string
	Success          *bool
	ErrorClass       string
	LatencyMS        int64
	Denied           bool
	RateLimited      bool
	ReplyType        string
	MetadataJSON     string
}

type RetentionPolicy struct {
	Raw    time.Duration
	Hourly time.Duration
	Daily  time.Duration
	Error  time.Duration
}

type TrendPoint struct {
	Bucket string `json:"bucket"`
	Count  int64  `json:"count"`
}

type Anomaly struct {
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
}

type Overview struct {
	RequestsToday int64     `json:"requests_today"`
	ErrorRate     float64   `json:"error_rate"`
	P95LatencyMS  int64     `json:"p95_latency_ms"`
	ActiveRooms   int64     `json:"active_rooms"`
	ActiveUsers   int64     `json:"active_users"`
	Anomalies     []Anomaly `json:"anomalies"`
}

type RoomSummary struct {
	RoomIDHash       string       `json:"room_id_hash"`
	RoomLabel        string       `json:"room_label"`
	RoomNameSnapshot string       `json:"room_name_snapshot"`
	Requests         int64        `json:"requests"`
	ActiveUsers      int64        `json:"active_users"`
	ErrorCount       int64        `json:"error_count"`
	ErrorRate        float64      `json:"error_rate"`
	Trend            []TrendPoint `json:"trend"`
}

type CommandUsage struct {
	CommandID string `json:"command_id"`
	Requests  int64  `json:"requests"`
	Errors    int64  `json:"errors"`
}

type IssueEvent struct {
	OccurredAt string `json:"occurred_at"`
	EventName  string `json:"event_name"`
	CommandID  string `json:"command_id"`
	ErrorClass string `json:"error_class"`
	Detail     string `json:"detail"`
}

type RoomDetail struct {
	RoomIDHash       string         `json:"room_id_hash"`
	RoomLabel        string         `json:"room_label"`
	RoomNameSnapshot string         `json:"room_name_snapshot"`
	Requests         int64          `json:"requests"`
	ActiveUsers      int64          `json:"active_users"`
	ErrorCount       int64          `json:"error_count"`
	ErrorRate        float64        `json:"error_rate"`
	ExplicitCount    int64          `json:"explicit_count"`
	AutoCount        int64          `json:"auto_count"`
	Hourly           []TrendPoint   `json:"hourly"`
	Commands         []CommandUsage `json:"commands"`
	RecentIssues     []IssueEvent   `json:"recent_issues"`
}

type ErrorBreakdown struct {
	ErrorClass string `json:"error_class"`
	Count      int64  `json:"count"`
}

type Reliability struct {
	TotalCommands     int64            `json:"total_commands"`
	FailedCommands    int64            `json:"failed_commands"`
	ErrorRate         float64          `json:"error_rate"`
	P95LatencyMS      int64            `json:"p95_latency_ms"`
	LatencySamples    int64            `json:"latency_samples"`
	RateLimitedCount  int64            `json:"rate_limited_count"`
	AccessDeniedCount int64            `json:"access_denied_count"`
	ReplyFailedCount  int64            `json:"reply_failed_count"`
	OverloadCount     int64            `json:"overload_count"`
	ErrorsByClass     []ErrorBreakdown `json:"errors_by_class"`
}

type CoupangFeatureStats struct {
	TrackedProducts     int64            `json:"tracked_products"`
	StaleProducts       int64            `json:"stale_products"`
	StaleRatio          float64          `json:"stale_ratio"`
	RefreshSuccessCount int64            `json:"refresh_success_count"`
	RefreshFailureCount int64            `json:"refresh_failure_count"`
	CompositeTotalCount int64            `json:"composite_total_count"`
	PartialCount        int64            `json:"partial_count"`
	PartialRatio        float64          `json:"partial_ratio"`
	JoinTotalCount      int64            `json:"join_total_count"`
	JoinTimeoutCount    int64            `json:"join_timeout_count"`
	JoinTimeoutRatio    float64          `json:"join_timeout_ratio"`
	RegistrationCount   int64            `json:"registration_count"`
	DeferredCount       int64            `json:"deferred_count"`
	BudgetExceededCount int64            `json:"budget_exceeded_count"`
	SeedDeferredCount   int64            `json:"seed_deferred_count"`
	RescueDeferredCount int64            `json:"rescue_deferred_count"`
	ChartSkippedCount   int64            `json:"chart_skipped_count"`
	ChartSkipReasons    []ErrorBreakdown `json:"chart_skip_reasons"`
}

type FeatureOps struct {
	FeatureUsage []CommandUsage      `json:"feature_usage"`
	Coupang      CoupangFeatureStats `json:"coupang"`
}

type ProductFunnelPoint struct {
	Stage          string  `json:"stage"`
	Count          int64   `json:"count"`
	ConversionRate float64 `json:"conversion_rate"`
	DropoffRate    float64 `json:"dropoff_rate"`
}

type ProductCohortPoint struct {
	CohortDate      string `json:"cohort_date"`
	ActivationUsers int64  `json:"activation_users"`
}

type ProductRetentionPoint struct {
	CohortDate    string  `json:"cohort_date"`
	BucketDate    string  `json:"bucket_date"`
	CohortSize    int64   `json:"cohort_size"`
	RetainedUsers int64   `json:"retained_users"`
	RetentionRate float64 `json:"retention_rate"`
}
