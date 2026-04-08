package formatter

import (
	"fmt"
	"strings"
)

// GoldData holds the fields needed for gold/silver price formatting.
type GoldData struct {
	Metal    string  // "금" or "은"
	Quantity float64 // e.g. 2.0
	Unit     string  // "돈", "g", "oz"
	Grams    float64 // total grams
	AltQty   float64 // alternate unit quantity (e.g. 2.67돈 when unit=g)
	AltUnit  string  // alternate unit name
	PriceKRW float64 // total price in KRW
}

// FormatGoldQuote formats a gold/silver quote.
//
//	금 2.0돈 (7.50g)
//	= 1,791,698원
func FormatGoldQuote(d GoldData) string {
	var b strings.Builder

	b.WriteString(d.Metal)
	b.WriteByte(' ')

	switch d.Unit {
	case "돈":
		b.WriteString(fmt.Sprintf("%.1f돈 (%.2fg)", d.Quantity, d.Grams))
	case "g":
		b.WriteString(fmt.Sprintf("%.1fg (%.2f돈)", d.Quantity, d.AltQty))
	case "oz":
		b.WriteString(fmt.Sprintf("%.1foz (%.2fg)", d.Quantity, d.Grams))
	default:
		b.WriteString(fmt.Sprintf("%.1f%s", d.Quantity, d.Unit))
	}

	b.WriteByte('\n')
	b.WriteString("= ")
	b.WriteString(addCommasFloat(d.PriceKRW, 0))
	b.WriteString("원")

	return b.String()
}

// CoinQuantityData holds data for coin quantity multiplier formatting.
type CoinQuantityData struct {
	Name     string
	Symbol   string
	Quantity float64
	USDTotal float64
	KRWTotal float64
}

// FormatCoinQuantity formats a coin price × quantity result.
//
//	솔라나(SOL) × 2
//	$188.20 (≈275,420원)
func FormatCoinQuantity(d CoinQuantityData) string {
	var b strings.Builder

	b.WriteString(d.Name)
	if d.Symbol != "" {
		b.WriteString(fmt.Sprintf("(%s)", d.Symbol))
	}
	b.WriteString(fmt.Sprintf(" × %s", formatQuantity(d.Quantity)))
	b.WriteByte('\n')

	if d.USDTotal > 0 {
		b.WriteString("$")
		b.WriteString(formatUSDPrice(d.USDTotal))
		if d.KRWTotal > 0 {
			b.WriteString(fmt.Sprintf(" (≈%s원)", addCommasFloat(d.KRWTotal, 0)))
		}
	} else if d.KRWTotal > 0 {
		b.WriteString(addCommasFloat(d.KRWTotal, 0))
		b.WriteString("원")
	}

	return b.String()
}

// StockQuantityData holds data for stock quantity multiplier formatting.
type StockQuantityData struct {
	Name       string
	SymbolCode string // for world stocks
	Quantity   float64
	Price      float64 // numeric price
	Currency   string  // "KRW", "USD", etc.
}

// FormatStockQuantity formats a stock price × quantity result.
//
//	삼성전자 × 10
//	568,000원
func FormatStockQuantity(d StockQuantityData) string {
	var b strings.Builder

	b.WriteString(d.Name)
	if d.SymbolCode != "" {
		b.WriteString(fmt.Sprintf("(%s)", d.SymbolCode))
	}
	b.WriteString(fmt.Sprintf(" × %s", formatQuantity(d.Quantity)))
	b.WriteByte('\n')

	total := d.Price * d.Quantity
	if d.Currency != "" && d.Currency != "KRW" {
		b.WriteString("$")
		b.WriteString(formatUSDPrice(total))
	} else {
		b.WriteString(addCommasFloat(total, 0))
		b.WriteString("원")
	}

	return b.String()
}

// FormatCalcResult formats a calculator result.
func FormatCalcResult(result float64) string {
	// If the result is effectively an integer, format without decimals.
	if result == float64(int64(result)) && result >= -1e15 && result <= 1e15 {
		return addCommasInt(int64(result))
	}
	// Decimal: show up to 2 decimal places, trim trailing zeros.
	s := fmt.Sprintf("%.2f", result)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")

	// Add commas to integer part.
	parts := strings.SplitN(s, ".", 2)
	var n int64
	fmt.Sscanf(parts[0], "%d", &n)
	intPart := addCommasInt(n)
	if len(parts) == 2 {
		return intPart + "." + parts[1]
	}
	return intPart
}

func formatQuantity(q float64) string {
	if q == float64(int64(q)) {
		return fmt.Sprintf("%d", int64(q))
	}
	s := fmt.Sprintf("%.2f", q)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}
