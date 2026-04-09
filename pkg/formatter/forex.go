package formatter

import (
	"fmt"
	"math"
	"strings"

	"github.com/shift-click/masterbot/internal/scraper/providers"
)

// FormatForexRates formats multi-currency exchange rates into a chat message.
// displayOrder determines the order of currencies in the output.
func FormatForexRates(rates map[string]providers.CurrencyRate, displayOrder []string) string {
	var b strings.Builder
	b.WriteString("💱 환율")

	for _, code := range displayOrder {
		rate, ok := rates[code]
		if !ok {
			continue
		}

		country := rate.Country
		if country == "" {
			country = code
		}

		priceStr := formatForexPrice(rate.BasePrice)
		changeStr := formatForexChange(rate.SignedChangePrice)

		b.WriteByte('\n')
		b.WriteString(fmt.Sprintf("%s: %s %s", country, priceStr, changeStr))
	}

	return b.String()
}

func formatForexPrice(price float64) string {
	if price >= 100 {
		return addCommasFloat(price, 2)
	}
	if price >= 1 {
		return fmt.Sprintf("%.2f", price)
	}
	return fmt.Sprintf("%.2f", price)
}

func formatForexChange(change float64) string {
	abs := math.Abs(change)
	switch {
	case change > 0:
		return fmt.Sprintf("▲%.2f", abs)
	case change < 0:
		return fmt.Sprintf("▼%.2f", abs)
	default:
		return "―"
	}
}
