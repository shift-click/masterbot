package formatter

import "fmt"

// FormatChartSummary formats a brief text summary to accompany a chart image.
func FormatChartSummary(name, symbol, currentPrice, changePercent, period string) string {
	header := name
	if symbol != "" && symbol != name {
		header = fmt.Sprintf("%s (%s)", name, symbol)
	}
	return fmt.Sprintf("%s\n현재: %s (%s)\n기간: %s", header, currentPrice, changePercent, period)
}
