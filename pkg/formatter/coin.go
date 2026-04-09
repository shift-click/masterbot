package formatter

import (
	"fmt"
	"math"
	"strings"
)

// CEXCoinData holds CEX coin data for formatting.
type CEXCoinData struct {
	Name          string
	Symbol        string
	MarketCap     string  // pre-formatted Korean style (e.g. "2,193조 8,124억")
	USDPrice      float64
	USDChangePct  float64
	USDPrevClose  float64
	USDChange     float64
	KRWPrice      float64
	KRWChangePct  float64
	KRWPrevClose  float64
	KRWChange     float64
	KimchiPremium float64
	HasKimchi     bool
}

// DEXCoinData holds DEX token data for formatting.
type DEXCoinData struct {
	Name            string
	Symbol          string
	ChainID         string
	DEXName         string
	USDPrice        float64
	USDChangePct24h float64
	Volume24h       float64
	Liquidity       float64
	MarketCap       string // pre-formatted
	KRWPrice        float64
	KRWChangePct    float64
}

// FormatCEXCoinQuote formats a CEX coin quote into a chat-friendly text message.
func FormatCEXCoinQuote(d CEXCoinData) string {
	var b strings.Builder

	// Header: 비트코인 | BTC
	b.WriteString(d.Name)
	if d.Symbol != "" {
		b.WriteString(" | ")
		b.WriteString(d.Symbol)
	}
	b.WriteByte('\n')

	// 시총
	if d.MarketCap != "" {
		b.WriteString("시총: ")
		b.WriteString(d.MarketCap)
		b.WriteByte('\n')
	}

	// USD price
	if d.USDPrice > 0 {
		sign, arrow := changeSymbols(d.USDChangePct)
		b.WriteString(fmt.Sprintf("USD: %s (%s%.2f%%)\n",
			formatUSDPrice(d.USDPrice), sign, math.Abs(d.USDChangePct)))

		if d.USDPrevClose > 0 {
			b.WriteString(fmt.Sprintf("전일: %s (%s %s)\n",
				formatUSDPrice(d.USDPrevClose), arrow, formatUSDPrice(math.Abs(d.USDChange))))
		}
	}

	// Separator between USD and KRW (only when both exist)
	if d.USDPrice > 0 && d.KRWPrice > 0 {
		b.WriteString("-\n")
	}

	// KRW price
	if d.KRWPrice > 0 {
		sign, arrow := changeSymbols(d.KRWChangePct)
		b.WriteString(fmt.Sprintf("KRW: %s (%s%.2f%%)\n",
			formatKRWPrice(d.KRWPrice), sign, math.Abs(d.KRWChangePct)))

		if d.KRWPrevClose > 0 {
			b.WriteString(fmt.Sprintf("전일: %s\n", formatKRWPrice(d.KRWPrevClose)))
			b.WriteString(fmt.Sprintf("( %s %s)\n", arrow, formatKRWPrice(math.Abs(d.KRWChange))))
		}
	}

	// Kimchi premium
	if d.HasKimchi {
		b.WriteString("-\n")
		b.WriteString(fmt.Sprintf("🌶️: %+.2f%%\n", d.KimchiPremium))
	}

	return strings.TrimRight(b.String(), "\n")
}

// FormatDEXCoinQuote formats a DEX token quote into a chat-friendly text message.
func FormatDEXCoinQuote(d DEXCoinData) string {
	var b strings.Builder

	// Header: Pepe | PEPE
	b.WriteString(d.Name)
	if d.Symbol != "" {
		b.WriteString(" | ")
		b.WriteString(d.Symbol)
	}
	b.WriteByte('\n')

	// Chain and DEX
	if d.ChainID != "" {
		chain := formatChainName(d.ChainID)
		if d.DEXName != "" {
			b.WriteString(fmt.Sprintf("체인: %s (%s)\n", chain, formatDEXName(d.DEXName)))
		} else {
			b.WriteString(fmt.Sprintf("체인: %s\n", chain))
		}
	}

	// USD price
	if d.USDPrice > 0 {
		sign, _ := changeSymbols(d.USDChangePct24h)
		b.WriteString(fmt.Sprintf("USD: %s (%s%.2f%%)\n",
			formatUSDPrice(d.USDPrice), sign, math.Abs(d.USDChangePct24h)))
	}

	// Volume
	if d.Volume24h > 0 {
		b.WriteString(fmt.Sprintf("24h Vol: %s\n", formatCompactUSD(d.Volume24h)))
	}

	// Liquidity
	if d.Liquidity > 0 {
		b.WriteString(fmt.Sprintf("유동성: %s\n", formatCompactUSD(d.Liquidity)))
	}

	// Market cap
	if d.MarketCap != "" {
		b.WriteString(fmt.Sprintf("시총: %s\n", d.MarketCap))
	}

	// Separator + KRW
	if d.KRWPrice > 0 {
		b.WriteString("-\n")
	}
	if d.KRWPrice > 0 {
		sign, _ := changeSymbols(d.KRWChangePct)
		b.WriteString(fmt.Sprintf("KRW: %s (%s%.2f%%)\n",
			formatKRWPrice(d.KRWPrice), sign, math.Abs(d.KRWChangePct)))
	}

	// DEX warning
	b.WriteString("-\n")
	if d.Liquidity > 0 && d.Liquidity < 10000 {
		b.WriteString("⚠️ DEX 토큰 (유동성 주의)")
	} else {
		b.WriteString("⚠️ DEX 토큰")
	}

	return b.String()
}

// --- helpers ---

func changeSymbols(pct float64) (signStr, arrow string) {
	if pct > 0 {
		return "+", "▲"
	} else if pct < 0 {
		return "-", "▼"
	}
	return "", "-"
}

func formatUSDPrice(price float64) string {
	if price >= 1 {
		return addCommasFloat(price, 2)
	}
	// For very small prices, show more decimals.
	if price >= 0.01 {
		return fmt.Sprintf("%.4f", price)
	}
	if price >= 0.0001 {
		return fmt.Sprintf("%.6f", price)
	}
	return fmt.Sprintf("%.8f", price)
}

func formatKRWPrice(price float64) string {
	if price >= 1 {
		return addCommasFloat(price, 0)
	}
	// For sub-1 KRW prices (meme coins), show enough precision.
	if price >= 0.01 {
		return fmt.Sprintf("%.4f", price)
	}
	if price >= 0.0001 {
		return fmt.Sprintf("%.6f", price)
	}
	return fmt.Sprintf("%.8f", price)
}

func addCommasFloat(f float64, decimals int) string {
	intPart := int64(f)
	s := addCommasInt(intPart)
	if decimals > 0 {
		frac := math.Abs(f - float64(intPart))
		fracStr := fmt.Sprintf("%.*f", decimals, frac)
		s += fracStr[1:] // strip leading "0"
	}
	return s
}

func addCommasInt(n int64) string {
	negative := n < 0
	if negative {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		if negative {
			return "-" + s
		}
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
	result := b.String()
	if negative {
		return "-" + result
	}
	return result
}

func formatCompactUSD(amount float64) string {
	switch {
	case amount >= 1_000_000_000:
		return fmt.Sprintf("$%.1fB", amount/1_000_000_000)
	case amount >= 1_000_000:
		return fmt.Sprintf("$%.1fM", amount/1_000_000)
	case amount >= 1_000:
		return fmt.Sprintf("$%.1fK", amount/1_000)
	default:
		return fmt.Sprintf("$%.0f", amount)
	}
}

func formatChainName(chainID string) string {
	names := map[string]string{
		"ethereum": "Ethereum",
		"solana":   "Solana",
		"bsc":      "BSC",
		"arbitrum": "Arbitrum",
		"base":     "Base",
		"polygon":  "Polygon",
		"avalanche": "Avalanche",
		"optimism": "Optimism",
		"fantom":   "Fantom",
		"cronos":   "Cronos",
	}
	if name, ok := names[strings.ToLower(chainID)]; ok {
		return name
	}
	return chainID
}

func formatDEXName(dexID string) string {
	names := map[string]string{
		"uniswap":    "Uniswap V3",
		"uniswapv3":  "Uniswap V3",
		"uniswapv2":  "Uniswap V2",
		"raydium":    "Raydium",
		"pancakeswap": "PancakeSwap",
		"sushiswap":  "SushiSwap",
		"orca":       "Orca",
		"camelot":    "Camelot",
		"trader_joe": "Trader Joe",
		"aerodrome":  "Aerodrome",
	}
	if name, ok := names[strings.ToLower(dexID)]; ok {
		return name
	}
	return dexID
}
