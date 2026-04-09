package formatter

import (
	"fmt"
	"strings"
)

// StockData holds the fields needed for stock quote formatting.
// This avoids a direct dependency on the providers package.
type StockData struct {
	Name            string
	Market          string
	Price           string
	PrevClose       string
	Change          string
	ChangePercent   string
	ChangeDirection string // RISING, FALLING, FLAT
	MarketCap       string
	PER             string
	PBR             string
	Revenue         string
	OperatingProfit string
	ForeignNet      string
	InstitutionNet  string
	IndividualNet   string
	TrendDate       string

	// World stock fields
	IsWorldStock bool
	Currency     string // e.g. "USD"
	EBITDA       string
	SymbolCode   string // e.g. "GOOGL"
}

// FormatStockQuote formats stock data into a KakaoTalk-friendly text message.
func FormatStockQuote(d StockData) string {
	if d.IsWorldStock {
		return FormatWorldStockQuote(d)
	}
	var b strings.Builder

	appendStockHeader(&b, d.Name, d.SymbolCode, d.Market, false)
	appendMarketCapLine(&b, d.MarketCap)
	appendPriceSection(&b, d, "현재")
	b.WriteString("━\n")
	appendPerPbrLine(&b, d.PER, d.PBR)
	appendAmountLine(&b, "매출", d.Revenue)
	appendAmountLine(&b, "영업이익", d.OperatingProfit)
	appendInvestorTrendSection(&b, d)

	return strings.TrimRight(b.String(), "\n")
}

// FormatWorldStockQuote formats world stock data into a KakaoTalk-friendly text message.
// Differences from Korean stock format:
// - Header includes symbol code: "알파벳 Class A (GOOGL) | NASDAQ"
// - Price label uses currency name: "USD:" instead of "현재:"
// - Shows EBITDA instead of 영업이익
// - Omits investor trend section (외국인/기관/개인)
// - Market cap uses KRW conversion with "원" suffix removed
func FormatWorldStockQuote(d StockData) string {
	var b strings.Builder

	appendStockHeader(&b, d.Name, d.SymbolCode, d.Market, true)
	appendMarketCapLine(&b, strings.TrimSuffix(d.MarketCap, "원"))
	label := "현재"
	if d.Currency != "" {
		label = d.Currency
	}
	appendPriceSection(&b, d, label)
	b.WriteString("━\n")
	appendPerPbrLine(&b, d.PER, d.PBR)
	appendAmountLine(&b, "매출", d.Revenue)
	appendAmountLine(&b, "EBITDA", d.EBITDA)

	return strings.TrimRight(b.String(), "\n")
}

func appendStockHeader(b *strings.Builder, name, symbol, market string, withSymbol bool) {
	b.WriteString(name)
	if withSymbol && symbol != "" {
		b.WriteString(fmt.Sprintf(" (%s)", symbol))
	}
	if market != "" {
		b.WriteString(" | ")
		b.WriteString(market)
	}
	b.WriteByte('\n')
}

func appendMarketCapLine(b *strings.Builder, marketCap string) {
	if marketCap == "" {
		return
	}
	b.WriteString("시총: ")
	b.WriteString(marketCap)
	b.WriteByte('\n')
}

func appendPriceSection(b *strings.Builder, d StockData, label string) {
	if d.Price == "" {
		return
	}
	arrow := directionSymbol(d.ChangeDirection)
	sign := ""
	if d.ChangeDirection == "RISING" {
		sign = "+"
	}
	b.WriteString(label)
	b.WriteString(": ")
	b.WriteString(d.Price)
	if d.ChangePercent != "" {
		b.WriteString(fmt.Sprintf(" (%s%s%%)", sign, d.ChangePercent))
	}
	b.WriteByte('\n')
	if d.PrevClose == "" {
		return
	}
	b.WriteString("전일: ")
	b.WriteString(d.PrevClose)
	if d.Change != "" {
		b.WriteString(fmt.Sprintf(" ( %s %s)", arrow, formatChange(d.Change)))
	}
	b.WriteByte('\n')
}

func appendPerPbrLine(b *strings.Builder, per, pbr string) {
	if per == "" && pbr == "" {
		return
	}
	parts := make([]string, 0, 2)
	if per != "" {
		parts = append(parts, "PER: "+per)
	}
	if pbr != "" {
		parts = append(parts, "PBR: "+pbr)
	}
	b.WriteString(strings.Join(parts, "  "))
	b.WriteByte('\n')
}

func appendAmountLine(b *strings.Builder, label, value string) {
	if value == "" {
		return
	}
	b.WriteString(label)
	b.WriteString(": ")
	b.WriteString(FormatAmountBillions(value))
	b.WriteByte('\n')
}

func appendInvestorTrendSection(b *strings.Builder, d StockData) {
	if d.ForeignNet == "" && d.InstitutionNet == "" && d.IndividualNet == "" {
		return
	}
	b.WriteString("━\n")
	dateLabel := d.TrendDate
	if dateLabel == "" {
		dateLabel = "-"
	}
	b.WriteString(fmt.Sprintf("[투자자 %s]\n", dateLabel))
	if d.ForeignNet != "" {
		b.WriteString(fmt.Sprintf("외국인: %s\n", d.ForeignNet))
	}
	if d.InstitutionNet != "" {
		b.WriteString(fmt.Sprintf("기관:   %s\n", d.InstitutionNet))
	}
	if d.IndividualNet != "" {
		b.WriteString(fmt.Sprintf("개인:   %s\n", d.IndividualNet))
	}
}

// FormatAmountBillions converts a number string like "3,336,059" (unit: 억원 from Naver)
// into a human-readable Korean format like "333조 6,059억".
func FormatAmountBillions(s string) string {
	// Remove commas to parse.
	cleaned := strings.ReplaceAll(s, ",", "")
	if cleaned == "" || cleaned == "-" {
		return s
	}

	// Parse as integer (unit: 억원 from Naver finance API).
	var amount int64
	for _, c := range cleaned {
		if c >= '0' && c <= '9' {
			amount = amount*10 + int64(c-'0')
		}
	}

	negative := strings.HasPrefix(cleaned, "-")
	if negative {
		amount = -amount
	}

	if amount == 0 {
		return "0"
	}

	absAmount := amount
	if absAmount < 0 {
		absAmount = -absAmount
	}

	prefix := ""
	if negative {
		prefix = "-"
	}

	cho := absAmount / 10000 // 조 단위
	eok := absAmount % 10000 // 억 단위

	if cho > 0 && eok > 0 {
		return fmt.Sprintf("%s%s조 %s억", prefix, formatWithCommas(cho), formatWithCommas(eok))
	}
	if cho > 0 {
		return fmt.Sprintf("%s%s조", prefix, formatWithCommas(cho))
	}
	return fmt.Sprintf("%s%s억", prefix, formatWithCommas(eok))
}

// directionSymbol returns ▲, ▼, or - based on direction.
func directionSymbol(direction string) string {
	switch direction {
	case "RISING":
		return "▲"
	case "FALLING":
		return "▼"
	default:
		return "-"
	}
}

// formatChange strips any leading +/- sign as we add our own arrow.
func formatChange(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "+")
	s = strings.TrimPrefix(s, "-")
	return s
}

// ThemeStockData holds the fields needed for theme stock list formatting.
type ThemeStockData struct {
	Name            string
	Market          string
	Price           string
	Change          string
	ChangePercent   string
	ChangeDirection string
}

// FormatThemeStocks formats a theme's stock list into a KakaoTalk-friendly text message.
func FormatThemeStocks(themeName string, stocks []ThemeStockData) string {
	var b strings.Builder

	b.WriteString("📊 ")
	b.WriteString(themeName)
	b.WriteString(" 관련주\n")

	for i, s := range stocks {
		sign := ""
		if s.ChangeDirection == "RISING" || s.ChangeDirection == "UPPER_LIMIT" {
			sign = "+"
		}

		b.WriteString(fmt.Sprintf("\n%d. %s (%s) | %s원 %s%s(%s%s%%)",
			i+1,
			s.Name,
			s.Market,
			s.Price,
			sign,
			s.Change,
			sign,
			s.ChangePercent,
		))
	}

	return b.String()
}

// FormatThemeDisambiguation formats a list of matched theme names for disambiguation.
func FormatThemeDisambiguation(keyword string, names []string) string {
	var b strings.Builder
	b.WriteString("📊 \"")
	b.WriteString(keyword)
	b.WriteString("\" 관련 테마:\n")

	for i, name := range names {
		if i > 0 {
			b.WriteString(" · ")
		}
		b.WriteString(name)
	}

	return b.String()
}

// formatWithCommas adds commas to a number.
func formatWithCommas(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}

	var b strings.Builder
	remainder := len(s) % 3
	if remainder > 0 {
		b.WriteString(s[:remainder])
		if len(s) > remainder {
			b.WriteByte(',')
		}
	}
	for i := remainder; i < len(s); i += 3 {
		if i > remainder {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}
