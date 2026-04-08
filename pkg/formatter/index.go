package formatter

import (
	"fmt"
	"strings"
)

// IndexData holds the fields needed for market index quote formatting.
type IndexData struct {
	Name            string
	Price           string
	Change          string
	ChangePercent   string
	ChangeDirection string // RISING, FALLING, FLAT
}

// FormatIndexQuote formats a market index quote into a KakaoTalk-friendly text message.
func FormatIndexQuote(d IndexData) string {
	var b strings.Builder

	b.WriteString(d.Name)
	b.WriteByte('\n')

	if d.Price == "" {
		b.WriteString("데이터 없음")
		return b.String()
	}

	arrow := directionSymbol(d.ChangeDirection)
	sign := ""
	if d.ChangeDirection == "RISING" {
		sign = "+"
	}

	b.WriteString(d.Price)
	if d.ChangePercent != "" {
		b.WriteString(fmt.Sprintf(" (%s%s%%)", sign, d.ChangePercent))
	}
	b.WriteByte('\n')

	if d.Change != "" {
		b.WriteString(fmt.Sprintf("%s %s", arrow, formatChange(d.Change)))
	}

	return strings.TrimRight(b.String(), "\n")
}
