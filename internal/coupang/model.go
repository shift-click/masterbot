package coupang

import "time"

type CoupangRefreshTier string

const (
	CoupangTierHot  CoupangRefreshTier = "hot"
	CoupangTierWarm CoupangRefreshTier = "warm"
	CoupangTierCold CoupangRefreshTier = "cold"
)

type CoupangSourceMappingState string

const (
	CoupangSourceMappingUnknown      CoupangSourceMappingState = ""
	CoupangSourceMappingVerified     CoupangSourceMappingState = "verified"
	CoupangSourceMappingNeedsRecheck CoupangSourceMappingState = "needs_recheck"
	CoupangSourceMappingFailed       CoupangSourceMappingState = "failed"
)

type CoupangSnapshot struct {
	TrackID              string
	Price                int
	LastSeenAt           time.Time
	LastRefreshAttemptAt time.Time
	RefreshSource        string
	Tier                 CoupangRefreshTier
	RefreshInFlight      bool
}

type CoupangSourceMapping struct {
	TrackID              string
	FallcentProductID    string
	SearchKeyword        string
	State                CoupangSourceMappingState
	VerifiedAt           time.Time
	FailureCount         int
	LastFailureReason    string
	ComparativeMinPrice  int
	LastChartBackfillAt  time.Time
}

type CoupangProductRecord struct {
	TrackID              string
	ProductID            string
	VendorItemID         string
	ItemID               string
	Name                 string
	ImageURL             string
	CreatedAt            time.Time
	LastQueried          time.Time
	QueryCount           int
	RecentQueryCount     int
	QueryWindowStartedAt time.Time
	Snapshot             CoupangSnapshot
	SourceMapping        CoupangSourceMapping
}

type PricePoint struct {
	Price     int
	IsSeed    bool
	FetchedAt time.Time
}

type PriceStats struct {
	MinPrice    int
	MinDate     time.Time
	MaxPrice    int
	MaxDate     time.Time
	AvgPrice    int
	TotalPoints int
}

type CoupangLookupResult struct {
	Product                CoupangProductRecord
	History                []PricePoint
	Stats                  *PriceStats
	SampleCount            int
	DistinctDays           int
	HistorySpanDays        int
	StatsEligible          bool
	IsStale                bool
	LastObservedAt         time.Time
	RefreshRequested       bool
	ReadRefresh            CoupangReadRefreshStatus
	RegistrationStage      CoupangRegistrationStage
	ResponseMode           CoupangResponseMode
	RegistrationDeferred   bool
	RegistrationDeferredUI bool
	BudgetExhausted        bool
	SeedDeferred           bool
	RescueDeferred         bool
}
