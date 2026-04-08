package providers

import "time"

// Timeframe represents a chart time period.
type Timeframe string

const (
	Timeframe1D Timeframe = "1d"
	Timeframe1W Timeframe = "1w"
	Timeframe1M Timeframe = "1M"
	Timeframe3M Timeframe = "3M"
	Timeframe6M Timeframe = "6M"
	Timeframe1Y Timeframe = "1Y"
)

// ParseTimeframe converts a user-facing string to a Timeframe.
// Returns empty string and false for unrecognized inputs.
func ParseTimeframe(s string) (Timeframe, bool) {
	switch s {
	case "1일", "1d", "1D":
		return Timeframe1D, true
	case "1주", "1w", "1W":
		return Timeframe1W, true
	case "1달", "1개월", "1m", "1M":
		return Timeframe1M, true
	case "3달", "3개월", "3m", "3M":
		return Timeframe3M, true
	case "6달", "6개월", "6m", "6M":
		return Timeframe6M, true
	case "1년", "1y", "1Y":
		return Timeframe1Y, true
	default:
		return "", false
	}
}

// TimeframeDays returns the approximate number of calendar days for a timeframe.
func TimeframeDays(tf Timeframe) int {
	switch tf {
	case Timeframe1D:
		return 5 // minimum 5 days for chart (1 daily candle is not chartable)
	case Timeframe1W:
		return 7
	case Timeframe1M:
		return 30
	case Timeframe3M:
		return 90
	case Timeframe6M:
		return 180
	case Timeframe1Y:
		return 365
	default:
		return 30
	}
}

// TimeframeLabel returns the Korean display label for a timeframe.
func TimeframeLabel(tf Timeframe) string {
	switch tf {
	case Timeframe1D:
		return "1일"
	case Timeframe1W:
		return "1주"
	case Timeframe1M:
		return "1개월"
	case Timeframe3M:
		return "3개월"
	case Timeframe6M:
		return "6개월"
	case Timeframe1Y:
		return "1년"
	default:
		return string(tf)
	}
}

// OHLCPoint represents a single OHLCV candlestick data point.
type OHLCPoint struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// OHLCData is a chronologically ordered series of candlestick data.
type OHLCData struct {
	Symbol string
	Points []OHLCPoint
}
