package formatter

import (
	"fmt"
	"math"
	"strings"
)

// ForexConvertResult holds data for a single currency conversion.
type ForexConvertResult struct {
	Code        string  // e.g. "USD"
	Amount      float64 // original amount in foreign currency
	KRW         float64 // converted KRW amount (rounded)
	RatePerUnit float64 // KRW per 1 unit of foreign currency
}

var currencyEmoji = map[string]string{
	"USD": "💵",
	"JPY": "💴",
	"CNY": "💴",
	"EUR": "💶",
	"THB": "🇹🇭",
	"TWD": "🇹🇼",
	"HKD": "🇭🇰",
	"VND": "🇻🇳",
}

var currencySymbol = map[string]string{
	"USD": "$",
	"JPY": "¥",
	"CNY": "¥",
	"EUR": "€",
	"THB": "฿",
	"TWD": "NT$",
	"HKD": "HK$",
	"VND": "₫",
}

// FormatForexConvert formats one or more currency conversion results.
func FormatForexConvert(results []ForexConvertResult) string {
	var b strings.Builder
	for i, r := range results {
		if i > 0 {
			b.WriteString("\n\n")
		}
		emoji := currencyEmoji[r.Code]
		symbol := currencySymbol[r.Code]
		amountStr := formatConvertAmount(r.Amount)
		krwStr := addCommasFloat(r.KRW, 0)
		rateStr := formatConvertRate(r.RatePerUnit)

		b.WriteString(fmt.Sprintf("%s %s%s = %s원\n(환율: %s원)", emoji, symbol, amountStr, krwStr, rateStr))
	}
	return b.String()
}

// formatConvertAmount formats the foreign currency amount, omitting decimals if integer.
func formatConvertAmount(amount float64) string {
	if amount == math.Trunc(amount) {
		return addCommasInt(int64(amount))
	}
	return addCommasFloat(amount, 2)
}

// formatConvertRate formats the exchange rate per unit.
func formatConvertRate(rate float64) string {
	if rate >= 100 {
		return addCommasFloat(rate, 2)
	}
	if rate >= 1 {
		return fmt.Sprintf("%.2f", rate)
	}
	return fmt.Sprintf("%.4f", rate)
}
