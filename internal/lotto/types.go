package lotto

import (
	"context"
	"sort"
	"strings"
	"time"
)

type TicketSource string

const (
	TicketSourceRecommend TicketSource = "recommend"
	TicketSourceManual    TicketSource = "manual"
)

type TicketStatus string

const (
	TicketStatusActive   TicketStatus = "active"
	TicketStatusInactive TicketStatus = "inactive"
)

type Draw struct {
	Round        int
	DrawDate     time.Time
	Numbers      [6]int
	BonusNumber  int
	Rank1Winners int
	Rank1Prize   int64
	Rank2Winners int
	Rank2Prize   int64
	Rank3Winners int
	Rank3Prize   int64
	Rank4Winners int
	Rank4Prize   int64
	Rank5Winners int
	Rank5Prize   int64
	TotalWinners int
	TotalSales   int64
	CreateTime   time.Time
	UpdateTime   time.Time
}

type FirstPrizeShop struct {
	Round         int
	ShopID        string
	ShopName      string
	Region        string
	District      string
	Address1      string
	Address2      string
	Address3      string
	Address4      string
	FullAddress   string
	WinMethodCode string
	WinMethodText string
	Latitude      float64
	Longitude     float64
	CreateTime    time.Time
	UpdateTime    time.Time
}

func (s FirstPrizeShop) RegionLabel() string {
	parts := make([]string, 0, 2)
	if region := strings.TrimSpace(s.Region); region != "" {
		parts = append(parts, region)
	}
	if district := strings.TrimSpace(s.District); district != "" {
		parts = append(parts, district)
	}
	if len(parts) == 0 {
		return "기타"
	}
	return strings.Join(parts, " ")
}

type TicketLine struct {
	ID         int64
	UserID     string
	Round      int
	LineNo     int
	Numbers    [6]int
	Source     TicketSource
	Status     TicketStatus
	CreateTime time.Time
	UpdateTime time.Time
}

type UserRoomProfile struct {
	UserID     string
	ChatID     string
	SenderName string
	LastSeenAt time.Time
	CreateTime time.Time
	UpdateTime time.Time
}

type TicketMatch struct {
	Line         TicketLine
	MatchCount   int
	BonusMatched bool
	Rank         int
	PrizeAmount  int64
}

type WinnerSummary struct {
	UserID      string
	MaskedName  string
	Rank        int
	PrizeAmount int64
}

type RegionCount struct {
	Label string
	Count int
}

type SummaryResult struct {
	Draw                 *Draw
	LatestUpdatePending  bool
	Participants         int
	Winners              []WinnerSummary
	WaitingRound         int
	WaitingParticipants  int
	FirstPrizeRegions    []RegionCount
}

type RecommendResult struct {
	DisplayName string
	Round       int
	DrawDate    time.Time
	Tickets     []TicketLine
}

type RegisterResult struct {
	DisplayName string
	Round       int
	DrawDate    time.Time
	TotalLines  int
	Added       []TicketLine
}

type QueryResult struct {
	DisplayName         string
	LatestOfficialRound int
	LatestDraw          *Draw
	LatestResults       []TicketMatch
	PendingRound        int
	PendingDrawDate     time.Time
	PendingTickets      []TicketLine
	CurrentRound        int
	CurrentDrawDate     time.Time
	CurrentTickets      []TicketLine
	AwaitingDraw        bool
}

type DeleteResult struct {
	DisplayName string
	Round       int
	Deleted     []TicketLine
}

type Repository interface {
	UpsertDraw(context.Context, Draw) error
	TouchDrawUpdateTime(context.Context, int, time.Time) error
	LatestDraw(context.Context) (*Draw, error)
	GetDraw(context.Context, int) (*Draw, error)
	ReplaceFirstPrizeShops(context.Context, int, []FirstPrizeShop) error
	ListFirstPrizeShops(context.Context, int) ([]FirstPrizeShop, error)
	UpsertUserRoomProfile(context.Context, UserRoomProfile) error
	GetUserRoomProfile(context.Context, string, string) (*UserRoomProfile, error)
	NextLineNo(context.Context, string, int) (int, error)
	InsertTickets(context.Context, []TicketLine) error
	ListUserTickets(context.Context, string, int, bool) ([]TicketLine, error)
	ListRoundTickets(context.Context, int, bool) ([]TicketLine, error)
	DeactivateAllUserTickets(context.Context, string, int) ([]TicketLine, error)
	DeactivateTicketLines(context.Context, string, int, []int) ([]TicketLine, error)
	Close() error
}

func NormalizeNumbers(values []int) [6]int {
	out := make([]int, len(values))
	copy(out, values)
	sort.Ints(out)
	var set [6]int
	copy(set[:], out)
	return set
}
