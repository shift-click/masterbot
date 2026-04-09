package command

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/lotto"
	"github.com/shift-click/masterbot/internal/transport"
)

type lottoService interface {
	Summary(context.Context, string, time.Time) (lotto.SummaryResult, error)
	Recommend(context.Context, string, string, string, time.Time) (lotto.RecommendResult, error)
	RegisterManual(context.Context, string, string, string, []int, time.Time) (lotto.RegisterResult, error)
	QueryMine(context.Context, string, string, string, time.Time) (lotto.QueryResult, error)
	DeleteMine(context.Context, string, string, string, *int, time.Time) (lotto.DeleteResult, error)
}

type LottoHandler struct {
	descriptorSupport
	service lottoService
	logger  *slog.Logger
}

func NewLottoHandler(service lottoService, logger *slog.Logger) *LottoHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &LottoHandler{
		descriptorSupport: newDescriptorSupport("lotto"),
		service:           service,
		logger:            logger.With("component", "lotto_handler"),
	}
}

func (h *LottoHandler) Execute(ctx context.Context, cmd bot.CommandContext) error {
	now := time.Now()
	if cmd.Now != nil {
		now = cmd.Now()
	}

	userID := strings.TrimSpace(cmd.Message.Raw.UserID)
	chatID := strings.TrimSpace(cmd.Message.Raw.ChatID)
	sender := strings.TrimSpace(cmd.Message.Sender)
	if userID == "" || chatID == "" {
		return cmd.Reply(ctx, bot.Reply{
			Type: transport.ReplyTypeText,
			Text: "로또 기능을 위한 사용자 정보를 확인할 수 없습니다.",
		})
	}

	message := strings.TrimSpace(cmd.Message.Msg)
	if strings.HasPrefix(message, "!로또") {
		return h.handleBangCommand(ctx, cmd, userID, chatID, sender, now)
	}
	return h.handlePlainCommand(ctx, cmd, userID, chatID, sender, now)
}

func (h *LottoHandler) handlePlainCommand(ctx context.Context, cmd bot.CommandContext, userID, chatID, sender string, now time.Time) error {
	switch {
	case len(cmd.Args) == 0:
		result, err := h.service.Summary(ctx, chatID, now)
		if err != nil {
			return h.replyText(ctx, cmd, "로또 정보를 가져올 수 없습니다.")
		}
		return h.replyText(ctx, cmd, formatLottoSummary(result))
	case len(cmd.Args) == 1 && strings.TrimSpace(cmd.Args[0]) == "추천":
		result, err := h.service.Recommend(ctx, userID, chatID, sender, now)
		if err != nil {
			return h.replyText(ctx, cmd, "로또 추천 번호를 생성할 수 없습니다.")
		}
		return h.replyText(ctx, cmd, formatLottoRecommend(result))
	default:
		return h.replyText(ctx, cmd, lottoUsageText())
	}
}

func (h *LottoHandler) handleBangCommand(ctx context.Context, cmd bot.CommandContext, userID, chatID, sender string, now time.Time) error {
	if len(cmd.Args) == 0 {
		result, err := h.service.QueryMine(ctx, userID, chatID, sender, now)
		if err != nil {
			return h.replyText(ctx, cmd, "로또 정보를 조회할 수 없습니다.")
		}
		return h.replyText(ctx, cmd, formatLottoMine(result))
	}

	if len(cmd.Args) == 1 && strings.TrimSpace(cmd.Args[0]) == "추천" {
		result, err := h.service.Recommend(ctx, userID, chatID, sender, now)
		if err != nil {
			return h.replyText(ctx, cmd, "로또 추천 번호를 생성할 수 없습니다.")
		}
		return h.replyText(ctx, cmd, formatLottoRecommend(result))
	}

	if lineNo, ok := parseDeleteArgs(cmd.Args); ok {
		result, err := h.service.DeleteMine(ctx, userID, chatID, sender, lineNo, now)
		if err != nil {
			return h.replyText(ctx, cmd, "로또 번호를 삭제할 수 없습니다.")
		}
		if len(result.Deleted) == 0 {
			return h.replyText(ctx, cmd, "삭제할 로또 번호가 없습니다.")
		}
		return h.replyText(ctx, cmd, formatLottoDelete(result))
	}

	values, err := parseNumberArgs(cmd.Args)
	if err != nil {
		if errors.Is(err, lotto.ErrInvalidNumbers) {
			return h.replyText(ctx, cmd, "올바르지 못한 숫자입니다")
		}
		return h.replyText(ctx, cmd, lottoUsageText())
	}

	result, err := h.service.RegisterManual(ctx, userID, chatID, sender, values, now)
	if err != nil {
		if errors.Is(err, lotto.ErrInvalidNumbers) {
			return h.replyText(ctx, cmd, "올바르지 못한 숫자입니다")
		}
		return h.replyText(ctx, cmd, "로또 번호를 등록할 수 없습니다.")
	}
	return h.replyText(ctx, cmd, formatLottoRegister(result))
}

func (h *LottoHandler) replyText(ctx context.Context, cmd bot.CommandContext, text string) error {
	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
	})
}

func parseDeleteArgs(args []string) (*int, bool) {
	if len(args) == 1 && strings.TrimSpace(args[0]) == "삭제" {
		return nil, true
	}
	if len(args) != 2 || strings.TrimSpace(args[1]) != "삭제" {
		return nil, false
	}
	if !isNumericToken(args[0]) {
		return nil, false
	}
	lineNo, err := strconv.Atoi(args[0])
	if err != nil || lineNo <= 0 {
		return nil, false
	}
	return &lineNo, true
}

func parseNumberArgs(args []string) ([]int, error) {
	if len(args) == 0 || len(args)%6 != 0 {
		return nil, lotto.ErrInvalidNumbers
	}
	values := make([]int, 0, len(args))
	for _, arg := range args {
		if !isNumericToken(arg) {
			return nil, lotto.ErrInvalidNumbers
		}
		value, err := strconv.Atoi(arg)
		if err != nil {
			return nil, lotto.ErrInvalidNumbers
		}
		values = append(values, value)
	}
	return values, nil
}

func isNumericToken(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, r := range value {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func lottoUsageText() string {
	return strings.Join([]string{
		"🎱 로또",
		"최신 당첨번호: 로또",
		"랜덤 번호 등록: 로또 추천",
		"내 번호 조회: !로또",
		"번호 등록: !로또 <n1> <n2> <n3> <n4> <n5> <n6>",
		"세트 삭제: !로또 <번호(옵션)> 삭제",
	}, "\n")
}

func formatLottoSummary(result lotto.SummaryResult) string {
	if result.Draw == nil {
		return "로또 정보를 가져올 수 없습니다."
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("%d회 로또 당첨번호 (%s)", result.Draw.Round, formatDate(result.Draw.DrawDate)))
	if result.LatestUpdatePending {
		b.WriteString("  *최신회차 정보가 아직 업데이트되지 않았습니다*")
	}
	b.WriteString("\n━━━━━━━━━━━━━━\n")
	b.WriteString("     ")
	b.WriteString(formatNumberSequence(result.Draw.Numbers[:]))
	b.WriteString(" + ")
	b.WriteString(strconv.Itoa(result.Draw.BonusNumber))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("💰 1등 %s원 (%d명)\n", formatKRW(result.Draw.Rank1Prize), result.Draw.Rank1Winners))
	b.WriteString(fmt.Sprintf("🥈 2등 %s원\n", formatKRW(result.Draw.Rank2Prize)))
	b.WriteString(fmt.Sprintf("🥉 3등 %s원\n", formatKRW(result.Draw.Rank3Prize)))
	b.WriteString("━━━━━━━━━━━━━━\n")
	b.WriteString(fmt.Sprintf("🤖 마스터봇 로또 참여: 총 %d명\n", result.Participants))
	b.WriteString("🎉 행운의 당첨자 🎉\n")
	if len(result.Winners) == 0 {
		b.WriteString("  - 아직 봇 내 당첨자가 없습니다.\n")
	} else {
		for _, winner := range result.Winners {
			b.WriteString("  - ")
			b.WriteString(rankBadge(winner.Rank))
			b.WriteString(" ")
			b.WriteString(winner.MaskedName)
			b.WriteString("님: ")
			b.WriteString(strconv.Itoa(winner.Rank))
			b.WriteString("등\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("⏳ 제%d회 추첨 대기: %d명\n", result.WaitingRound, result.WaitingParticipants))
	b.WriteString(" ㄴ 참여방법: 채팅창에 '로또 추천' 입력\n")
	if len(result.FirstPrizeRegions) > 0 {
		b.WriteString("━━━━━━━━━━━━━━\n")
		b.WriteString("📍 1등 배출지역\n")
		for _, item := range result.FirstPrizeRegions {
			b.WriteString("  ")
			b.WriteString(item.Label)
			b.WriteString(" ")
			b.WriteString(strconv.Itoa(item.Count))
			b.WriteString("명\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func formatLottoRecommend(result lotto.RecommendResult) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🎱 %s님의 제%d회 추천 번호\n", result.DisplayName, result.Round))
	b.WriteString("━━━━━━━━━━━━━━\n")
	for _, ticket := range result.Tickets {
		b.WriteString(formatTicketLine(ticket))
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func formatLottoRegister(result lotto.RegisterResult) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("✅ %s님의 번호가 제%d회에 등록되었습니다! (총 %d줄)\n", result.DisplayName, result.Round, result.TotalLines))
	for _, ticket := range result.Added {
		b.WriteString(formatTicketLine(ticket))
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("📅 추첨일: %s", formatDateTime(drawTime(result.DrawDate))))
	return strings.TrimSpace(b.String())
}

func formatLottoMine(result lotto.QueryResult) string {
	if len(result.LatestResults) == 0 && len(result.PendingTickets) == 0 && len(result.CurrentTickets) == 0 {
		return strings.Join([]string{
			fmt.Sprintf("❓ %s님의 등록된 로또 번호가 없습니다.", result.DisplayName),
			"등록: !로또 <n1> <n2> ... <n6>",
		}, "\n")
	}

	var b strings.Builder
	if len(result.LatestResults) > 0 && result.LatestDraw != nil {
		b.WriteString(fmt.Sprintf("🎱 %s님의 로또 번호 (제%d회)\n", result.DisplayName, result.LatestDraw.Round))
		b.WriteString(fmt.Sprintf("당첨번호: %s + %d\n", formatNumberSequence(result.LatestDraw.Numbers[:]), result.LatestDraw.BonusNumber))
		b.WriteString("━━━━━━━━━━━━━━\n")
		for _, match := range result.LatestResults {
			b.WriteString(formatMatchLine(match))
			b.WriteString("\n")
		}
	}

	if len(result.PendingTickets) > 0 {
		if b.Len() > 0 {
			b.WriteString("━━━━━━━━━━━━━━\n")
		}
		b.WriteString(fmt.Sprintf("⏳ 추첨 결과 업데이트 대기: 제%d회, %d줄\n", result.PendingRound, len(result.PendingTickets)))
		for _, ticket := range result.PendingTickets {
			b.WriteString(formatTicketLine(ticket))
			b.WriteString("\n")
		}
		b.WriteString("📌 최신회차 정보가 아직 업데이트되지 않았습니다.\n")
	}

	if len(result.CurrentTickets) > 0 {
		if len(result.LatestResults) == 0 && len(result.PendingTickets) == 0 {
			b.WriteString(fmt.Sprintf("⏳ %s님의 로또 번호 (제%d회, %d줄)\n", result.DisplayName, result.CurrentRound, len(result.CurrentTickets)))
			b.WriteString("━━━━━━━━━━━━━━\n")
			for _, ticket := range result.CurrentTickets {
				b.WriteString(formatTicketLine(ticket))
				b.WriteString("\n")
			}
			b.WriteString("━━━━━━━━━━━━━━\n")
			b.WriteString(fmt.Sprintf("📌 현재 최신 추첨: 제%d회\n", result.LatestOfficialRound))
			b.WriteString("💬 아직 추첨 전입니다. 추첨 후 다시 확인해주세요!")
			return strings.TrimSpace(b.String())
		}

		if b.Len() > 0 {
			b.WriteString("━━━━━━━━━━━━━━\n")
		}
		b.WriteString(fmt.Sprintf("⏳ 다음 회차 로또 번호: 제%d회, %d줄\n", result.CurrentRound, len(result.CurrentTickets)))
		b.WriteString("━━━━━━━━━━━━━━\n")
		for _, ticket := range result.CurrentTickets {
			b.WriteString(formatTicketLine(ticket))
			b.WriteString("\n")
		}
		b.WriteString("━━━━━━━━━━━━━━\n")
		b.WriteString(fmt.Sprintf("%d회 추첨일: %s\n", result.CurrentRound, formatDateTime(drawTime(result.CurrentDrawDate))))
	}
	return strings.TrimSpace(b.String())
}

func formatLottoDelete(result lotto.DeleteResult) string {
	if len(result.Deleted) == 1 {
		return fmt.Sprintf("✅ %s님의 %s 삭제완료", result.DisplayName, formatTicketLine(result.Deleted[0]))
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("✅ %s님의\n", result.DisplayName))
	for _, ticket := range result.Deleted {
		b.WriteString(formatTicketLine(ticket))
		b.WriteString("\n")
	}
	b.WriteString("삭제완료")
	return strings.TrimSpace(b.String())
}

func formatMatchLine(match lotto.TicketMatch) string {
	var b strings.Builder
	b.WriteString(formatTicketLine(match.Line))
	b.WriteString(" → ")
	if match.Rank > 0 {
		b.WriteString(rankText(match.Rank))
		b.WriteString(" (")
		b.WriteString(formatKRW(match.PrizeAmount))
		b.WriteString("원)")
		return b.String()
	}
	b.WriteString(fmt.Sprintf("😭 낙첨 (%d개)", match.MatchCount))
	return b.String()
}

func formatTicketLine(ticket lotto.TicketLine) string {
	return fmt.Sprintf("[%d] %s", ticket.LineNo, formatNumberSequence(ticket.Numbers[:]))
}

func formatNumberSequence(numbers []int) string {
	parts := make([]string, 0, len(numbers))
	for _, value := range numbers {
		parts = append(parts, fmt.Sprintf("%2d", value))
	}
	return strings.Join(parts, " ")
}

func rankText(rank int) string {
	switch rank {
	case 1:
		return "👑 1등"
	case 2:
		return "🥇 2등"
	case 3:
		return "🥈 3등"
	case 4:
		return "🎖️ 4등"
	case 5:
		return "🎉 5등"
	default:
		return "낙첨"
	}
}

func rankBadge(rank int) string {
	switch rank {
	case 1:
		return "👑"
	case 2:
		return "🥇"
	case 3:
		return "🥈"
	case 4:
		return "🎖️"
	case 5:
		return "🍀"
	default:
		return "•"
	}
}

func formatKRW(value int64) string {
	raw := strconv.FormatInt(value, 10)
	if len(raw) <= 3 {
		return raw
	}
	var parts []string
	for len(raw) > 3 {
		parts = append([]string{raw[len(raw)-3:]}, parts...)
		raw = raw[:len(raw)-3]
	}
	if raw != "" {
		parts = append([]string{raw}, parts...)
	}
	return strings.Join(parts, ",")
}

func formatDate(ts time.Time) string {
	ts = ts.In(kstLocation())
	return fmt.Sprintf("%d. %d. %d. (%s)", ts.Year(), ts.Month(), ts.Day(), weekdayKorean(ts.Weekday()))
}

func formatDateTime(ts time.Time) string {
	ts = ts.In(kstLocation())
	return fmt.Sprintf("%d. %d. %d. (%s) %02d:%02d", ts.Year(), ts.Month(), ts.Day(), weekdayKorean(ts.Weekday()), ts.Hour(), ts.Minute())
}

func drawTime(date time.Time) time.Time {
	date = date.In(kstLocation())
	return time.Date(date.Year(), date.Month(), date.Day(), 20, 35, 0, 0, date.Location())
}

func weekdayKorean(day time.Weekday) string {
	switch day {
	case time.Sunday:
		return "일"
	case time.Monday:
		return "월"
	case time.Tuesday:
		return "화"
	case time.Wednesday:
		return "수"
	case time.Thursday:
		return "목"
	case time.Friday:
		return "금"
	default:
		return "토"
	}
}

func kstLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil || loc == nil {
		return time.FixedZone("KST", 9*60*60)
	}
	return loc
}
