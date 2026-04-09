package providers

import "time"

// BaseballMatchStatus represents the state of a baseball game.
type BaseballMatchStatus string

const (
	BaseballScheduled BaseballMatchStatus = "scheduled"
	BaseballLive      BaseballMatchStatus = "live"
	BaseballFinished  BaseballMatchStatus = "finished"
	BaseballCancelled BaseballMatchStatus = "cancelled"
)

// InningHalf indicates top or bottom of an inning.
type InningHalf string

const (
	InningTop    InningHalf = "top"
	InningBottom InningHalf = "bottom"
)

// BaseballMatch holds data for a single baseball game.
type BaseballMatch struct {
	ID        string
	League    string // league ID from registry (e.g. "mlb", "kbo", "npb")
	HomeTeam  string
	AwayTeam  string
	HomeScore int
	AwayScore int
	Status    BaseballMatchStatus
	Inning    int        // current inning number (0 if not started)
	Half      InningHalf // "top" or "bottom"
	StartTime time.Time
}
