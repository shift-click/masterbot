package formatter

import (
	"fmt"
	"strings"
	"time"
)

// CoupangPriceData holds all data needed to format a Coupang price trend response.
type CoupangPriceData struct {
	Name                string
	CurrentPrice        int
	MinPrice            int
	MinDate             time.Time
	MaxPrice            int
	MaxDate             time.Time
	AvgPrice            int
	Prices              []int // chronological price history for sparkline
	HistorySpanDays     int   // inclusive day span across the observed history
	ProductURL          string
	ComparativeMinPrice int
	StatsEligible       bool
	IsStale             bool
	LastObservedAt      time.Time
	SampleCount         int
	RefreshStatus       string
	RefreshRequested    bool
	DeferredNotice      bool

	HasChart bool // whether a chart image was sent
}

// FormatCoupangPrice formats a complete Coupang price trend message.
func FormatCoupangPrice(d CoupangPriceData) string {
	var b strings.Builder

	// Product name
	b.WriteString("🛒 ")
	b.WriteString(d.Name)
	b.WriteString("\n\n")

	// Current price
	b.WriteString(fmt.Sprintf("💰 현재가 %s원", formatKRWInt(d.CurrentPrice)))

	// Lowest price reference
	referenceMin := selectLowestPriceReference(d)
	if referenceMin > 0 {
		delta := formatLowestPriceDelta(d.CurrentPrice, referenceMin)
		b.WriteByte('\n')
		b.WriteString(fmt.Sprintf("📉 최저가 %s원 (%s)", formatKRWInt(referenceMin), delta))
	}

	// Highest & Average from local stats
	if d.StatsEligible && d.MaxPrice > 0 {
		b.WriteByte('\n')
		b.WriteString(fmt.Sprintf("📈 최고가 %s원", formatKRWInt(d.MaxPrice)))
	}
	if d.StatsEligible && d.AvgPrice > 0 {
		b.WriteByte('\n')
		b.WriteString(fmt.Sprintf("📊 평균가 %s원", formatKRWInt(d.AvgPrice)))
	}

	// Sparkline text if prices available and chart not sent
	if len(d.Prices) >= 3 && !d.HasChart {
		sparkline := buildSparkline(d.Prices)
		if sparkline != "" {
			b.WriteString("\n\n")
			b.WriteString(sparkline)
		}
	}

	// Stale indicator
	if d.IsStale && !d.LastObservedAt.IsZero() {
		b.WriteString("\n\n")
		b.WriteString(fmt.Sprintf("⏰ 최근 확인 %s", formatObservedAgo(d.LastObservedAt)))
	}
	if d.DeferredNotice {
		b.WriteString("\n\n")
		b.WriteString("⏳ 가격 이력 보강 중")
	}

	return b.String()
}

// buildSparkline creates a unicode sparkline chart from price points.
func buildSparkline(prices []int) string {
	if len(prices) < 2 {
		return ""
	}
	bars := []rune("▁▂▃▄▅▆▇█")
	minP, maxP := prices[0], prices[0]
	for _, p := range prices {
		if p < minP {
			minP = p
		}
		if p > maxP {
			maxP = p
		}
	}
	priceRange := maxP - minP
	if priceRange == 0 {
		// All prices the same — flat line
		return strings.Repeat("▄", len(prices))
	}
	var sb strings.Builder
	for _, p := range prices {
		idx := int(float64(p-minP) / float64(priceRange) * 7)
		if idx > 7 {
			idx = 7
		}
		sb.WriteRune(bars[idx])
	}
	return sb.String()
}

// extractProductID pulls the product ID from a URL like "coupang.com/vp/products/9055094692?..."
func extractProductID(url string) string {
	idx := strings.Index(url, "products/")
	if idx < 0 {
		return ""
	}
	rest := url[idx+len("products/"):]
	end := strings.IndexAny(rest, "?&#")
	if end >= 0 {
		return rest[:end]
	}
	return rest
}

// formatKRWInt formats an integer price with comma separators.
func formatKRWInt(price int) string {
	return addCommasInt(int64(price))
}

func formatObservedAgo(t time.Time) string {
	if t.IsZero() {
		return "알 수 없음"
	}
	diff := time.Since(t)
	switch {
	case diff < time.Minute:
		return "방금 전"
	case diff < time.Hour:
		return fmt.Sprintf("%d분 전", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%d시간 전", int(diff.Hours()))
	default:
		return fmt.Sprintf("%d일 전", int(diff.Hours()/24))
	}
}

func selectLowestPriceReference(d CoupangPriceData) int {
	if d.StatsEligible && d.MinPrice > 0 {
		return d.MinPrice
	}
	if d.ComparativeMinPrice > 0 {
		return d.ComparativeMinPrice
	}
	return 0
}

func formatLowestPriceDelta(current, lowest int) string {
	if lowest <= 0 {
		return ""
	}
	diffPct := float64(current-lowest) / float64(lowest) * 100
	switch {
	case diffPct > 0:
		return fmt.Sprintf("+%.1f%%", diffPct)
	case diffPct < 0:
		return fmt.Sprintf("%.1f%%", diffPct)
	default:
		return "0.0%"
	}
}
