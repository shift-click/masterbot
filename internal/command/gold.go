package command

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/pkg/formatter"
)

const (
	gramsPerDon    = 3.75
	gramsPerTroyOz = 31.1035
)

// goldAliases maps trigger keywords to metal type ("gold" or "silver").
var goldAliases = map[string]string{
	"금":      "gold",
	"골드":     "gold",
	"gold":   "gold",
	"금값":     "gold",
	"금시세":    "gold",
	"은":      "silver",
	"실버":     "silver",
	"silver": "silver",
	"은값":     "silver",
	"은시세":    "silver",
}

// goldThemeExclusions are second-word patterns that should NOT trigger gold handler.
var goldThemeExclusions = map[string]bool{
	"테마":  true,
	"관련주": true,
	"관련":  true,
	"주식":  true,
	"종목":  true,
}

// goldUnitPattern parses unit expressions: "2돈", "한돈", "10g", "1oz", etc.
var goldUnitPattern = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*(돈|g|그램|oz|온스)$`)
var goldKoreanNumberUnit = regexp.MustCompile(`^(한|두|세|네|다섯|여섯|일곱|여덟|아홉|열)(돈|g|그램|oz|온스)$`)

var koreanNumbers = map[string]float64{
	"한":  1,
	"두":  2,
	"세":  3,
	"네":  4,
	"다섯": 5,
	"여섯": 6,
	"일곱": 7,
	"여덟": 8,
	"아홉": 9,
	"열":  10,
}

type goldQuery struct {
	metal string  // "gold" or "silver"
	qty   float64 // quantity in the given unit
	unit  string  // "돈", "g", "oz"
}

// GoldHandler handles gold/silver price lookups.
type GoldHandler struct {
	descriptorSupport
	provider *providers.NaverGold
	logger   *slog.Logger
}

func NewGoldHandler(provider *providers.NaverGold, logger *slog.Logger) *GoldHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &GoldHandler{
		descriptorSupport: newDescriptorSupport("gold"),
		provider:          provider,
		logger:            logger.With("component", "gold_handler"),
	}
}

func (h *GoldHandler) SupportsSlashCommands() bool { return false }

func (h *GoldHandler) MatchBareQuery(_ context.Context, content string) ([]string, bool) {
	q, ok := parseGoldQuery(content)
	if !ok {
		return nil, false
	}
	_ = q
	return []string{content}, true
}

func (h *GoldHandler) Execute(ctx context.Context, cmd bot.CommandContext) error {
	input := strings.TrimSpace(cmd.Message.Msg)
	if len(cmd.Args) > 0 {
		input = strings.Join(cmd.Args, " ")
	}

	q, ok := parseGoldQuery(input)
	if !ok {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "금/은 시세를 조회할 수 없습니다.",
		})
	}

	var price *providers.GoldPrice
	var err error
	if q.metal == "gold" {
		price, err = h.provider.Gold(ctx)
	} else {
		price, err = h.provider.Silver(ctx)
	}
	if err != nil {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "금/은 시세를 가져올 수 없습니다.",
		})
	}

	// Calculate total price.
	totalGrams := unitToGrams(q.qty, q.unit)
	totalPrice := price.PricePerG * totalGrams

	metalName := "금"
	if q.metal == "silver" {
		metalName = "은"
	}

	// Build formatter data.
	data := formatter.GoldData{
		Metal:    metalName,
		Quantity: q.qty,
		Unit:     q.unit,
		Grams:    totalGrams,
		PriceKRW: totalPrice,
	}

	// Set alternate unit display.
	switch q.unit {
	case "돈":
		// no alt needed, grams already set
	case "g":
		data.AltQty = totalGrams / gramsPerDon
		data.AltUnit = "돈"
	case "oz":
		// grams already set
	}

	text := formatter.FormatGoldQuote(data)
	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
	})
}

// parseGoldQuery parses input like "금", "금 한돈", "금 2돈", "금 10g", "금 1oz", "금 * 2".
func parseGoldQuery(input string) (goldQuery, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return goldQuery{}, false
	}

	fields := strings.Fields(input)
	metal, ok := parseGoldMetal(fields, input)
	if !ok {
		return goldQuery{}, false
	}
	if len(fields) == 1 {
		return defaultGoldQuery(metal), true
	}
	if len(fields) == 2 {
		return parseTwoFieldGoldQuery(input, metal, fields[1])
	}
	if len(fields) == 3 {
		return parseThreeFieldGoldQuery(input)
	}
	return goldQuery{}, false
}

func parseGoldMetal(fields []string, input string) (string, bool) {
	if len(fields) == 0 {
		return "", false
	}
	metal, ok := goldAliases[strings.ToLower(fields[0])]
	if ok {
		return metal, true
	}
	metal, ok = goldAliases[strings.ToLower(input)]
	return metal, ok
}

func defaultGoldQuery(metal string) goldQuery {
	return goldQuery{
		metal: metal,
		qty:   1,
		unit:  defaultGoldUnit(metal),
	}
}

func defaultGoldUnit(metal string) string {
	if metal == "silver" {
		return "g"
	}
	return "돈"
}

func parseTwoFieldGoldQuery(input, metal, second string) (goldQuery, bool) {
	if goldThemeExclusions[second] {
		return goldQuery{}, false
	}
	if query, qty, ok := parseQuantifiedQuery(input); ok && qty != 1 {
		return parseQuantifiedGoldQuery(query, qty)
	}
	if uq, ok := parseGoldUnit(second); ok {
		return goldQuery{metal: metal, qty: uq.qty, unit: uq.unit}, true
	}
	return goldQuery{}, false
}

func parseThreeFieldGoldQuery(input string) (goldQuery, bool) {
	query, qty, ok := parseQuantifiedQuery(input)
	if !ok {
		return goldQuery{}, false
	}
	return parseQuantifiedGoldQuery(query, qty)
}

func parseQuantifiedGoldQuery(query string, qty float64) (goldQuery, bool) {
	metal, ok := goldAliases[strings.ToLower(query)]
	if !ok {
		return goldQuery{}, false
	}
	return goldQuery{metal: metal, qty: qty, unit: defaultGoldUnit(metal)}, true
}

type unitQty struct {
	qty  float64
	unit string
}

// parseGoldUnit parses "2돈", "한돈", "10g", "10그램", "1oz", "1온스".
func parseGoldUnit(s string) (unitQty, bool) {
	// Try Korean number: "한돈", "두돈"
	if m := goldKoreanNumberUnit.FindStringSubmatch(s); m != nil {
		num, ok := koreanNumbers[m[1]]
		if !ok {
			return unitQty{}, false
		}
		unit := normalizeGoldUnit(m[2])
		return unitQty{qty: num, unit: unit}, true
	}

	// Try numeric: "2돈", "10g", "1oz"
	if m := goldUnitPattern.FindStringSubmatch(s); m != nil {
		num, err := strconv.ParseFloat(m[1], 64)
		if err != nil || num <= 0 {
			return unitQty{}, false
		}
		unit := normalizeGoldUnit(m[2])
		return unitQty{qty: num, unit: unit}, true
	}

	return unitQty{}, false
}

func normalizeGoldUnit(s string) string {
	switch s {
	case "돈":
		return "돈"
	case "g", "그램":
		return "g"
	case "oz", "온스":
		return "oz"
	default:
		return s
	}
}

func unitToGrams(qty float64, unit string) float64 {
	switch unit {
	case "돈":
		return qty * gramsPerDon
	case "g":
		return qty
	case "oz":
		return qty * gramsPerTroyOz
	default:
		return qty * gramsPerDon // default to 돈
	}
}

func (h *GoldHandler) MatchAutoQueryCandidate(_ context.Context, content string) bool {
	query, ok := extractAutoCandidate(content, 3)
	if !ok || len([]rune(query)) > 20 {
		return false
	}
	_, matched := goldAliases[strings.ToLower(query)]
	return matched
}

func (h *GoldHandler) HandleFallback(ctx context.Context, cmd bot.CommandContext) error {
	query, ok := extractAutoCandidate(cmd.Message.Msg, 3)
	if !ok || len([]rune(query)) > 20 {
		return nil
	}

	_, matched := goldAliases[strings.ToLower(query)]
	if !matched {
		return nil
	}

	// Construct args and execute.
	cmd.Args = []string{query}
	if err := h.Execute(ctx, cmd); err != nil {
		return err
	}
	return bot.ErrHandled
}

// Ensure GoldHandler implements the needed interfaces.
var _ bot.Handler = (*GoldHandler)(nil)

// Verify it is usable as BareQueryMatcher.
var _ interface {
	MatchBareQuery(context.Context, string) ([]string, bool)
} = (*GoldHandler)(nil)

// Stub methods to satisfy bot.Handler.
func (h *GoldHandler) ID() string { return h.descriptor.ID }

func init() {
	// Suppress unused import warning for fmt if not used elsewhere.
	_ = fmt.Sprintf
}
