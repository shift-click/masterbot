package command

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/pkg/formatter"
)

type currencyAlias struct {
	Alias string
	Code  string
}

// Sorted longest-first so "홍콩달러" matches before "달러".
var currencyAliases = []currencyAlias{
	{"타이완달러", "TWD"},
	{"베트남동", "VND"},
	{"대만달러", "TWD"},
	{"홍콩달러", "HKD"},
	{"대만불", "TWD"},
	{"홍콩불", "HKD"},
	{"위안", "CNY"},
	{"원안", "CNY"},
	{"달러", "USD"},
	{"유로", "EUR"},
	{"바트", "THB"},
	{"엔", "JPY"},
	{"옌", "JPY"},
	{"밧", "THB"},
	{"불", "USD"},
	{"동", "VND"},
}

var koreanMultipliers = map[string]float64{
	"천": 1_000,
	"만": 10_000,
	"억": 100_000_000,
}

var currencyConvertPattern *regexp.Regexp

func init() {
	aliases := make([]string, len(currencyAliases))
	for i, a := range currencyAliases {
		aliases[i] = regexp.QuoteMeta(a.Alias)
	}
	// Pattern: {number}{optional whitespace}{optional 천/만/억}{optional whitespace}{currency alias}
	currencyConvertPattern = regexp.MustCompile(
		`(\d+(?:\.\d+)?)\s*(천|만|억)?\s*(` + strings.Join(aliases, "|") + `)`,
	)
}

type currencyMatch struct {
	Amount float64
	Code   string
}

func parseCurrencyExpressions(text string) []currencyMatch {
	matches := currencyConvertPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}

	results := make([]currencyMatch, 0, len(matches))
	for _, m := range matches {
		numStr := m[1]
		unit := m[2]
		alias := m[3]

		amount, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			continue
		}

		if mult, ok := koreanMultipliers[unit]; ok {
			amount *= mult
		}

		code := aliasToCode(alias)
		if code == "" {
			continue
		}

		results = append(results, currencyMatch{Amount: amount, Code: code})
	}
	return results
}

func aliasToCode(alias string) string {
	for _, a := range currencyAliases {
		if a.Alias == alias {
			return a.Code
		}
	}
	return ""
}

func convertToKRW(amount float64, rate providers.CurrencyRate) float64 {
	if rate.CurrencyUnit == 0 {
		return 0
	}
	return amount * (rate.BasePrice / float64(rate.CurrencyUnit))
}

// ForexConvertHandler detects currency expressions in chat and responds with KRW conversion.
type ForexConvertHandler struct {
	descriptorSupport
	forex *providers.DunamuForex
}

func NewForexConvertHandler(forex *providers.DunamuForex) *ForexConvertHandler {
	return &ForexConvertHandler{
		descriptorSupport: newDescriptorSupport("forex-convert"),
		forex:             forex,
	}
}

func (h *ForexConvertHandler) HandleFallback(ctx context.Context, cmd bot.CommandContext) error {
	msg := strings.TrimSpace(cmd.Message.Msg)
	if msg == "" {
		return nil
	}

	matches := parseCurrencyExpressions(msg)
	if len(matches) == 0 {
		return nil
	}

	rates := h.forex.Rates()
	if len(rates.Rates) == 0 {
		return nil
	}

	var results []formatter.ForexConvertResult
	for _, m := range matches {
		rate, ok := rates.Rates[m.Code]
		if !ok {
			continue
		}
		krw := convertToKRW(m.Amount, rate)
		perUnit := rate.BasePrice / float64(rate.CurrencyUnit)
		results = append(results, formatter.ForexConvertResult{
			Code:        m.Code,
			Amount:      m.Amount,
			KRW:         math.Round(krw),
			RatePerUnit: perUnit,
		})
	}

	if len(results) == 0 {
		return nil
	}

	text := formatter.FormatForexConvert(results)
	if err := cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
	}); err != nil {
		return fmt.Errorf("forex convert reply: %w", err)
	}
	return bot.ErrHandled
}
