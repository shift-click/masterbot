package lotto

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sort"
	"strings"
	"time"

	"github.com/shift-click/masterbot/internal/scraper/providers"
)

var ErrInvalidNumbers = errors.New("invalid lotto numbers")

type Provider interface {
	FetchLatestDraw(context.Context) (*providers.DHLotteryDraw, error)
	FetchFirstPrizeShops(context.Context, int) ([]providers.DHLotteryFirstPrizeShop, error)
}

type ServiceConfig struct {
	SyncCooldown time.Duration
}

type Service struct {
	repo         Repository
	provider     Provider
	logger       *slog.Logger
	syncCooldown time.Duration
}

type latestDrawState struct {
	draw                *Draw
	updatePending       bool
	registrationRound   int
	registrationDrawDate time.Time
	pendingRound        int
	pendingDrawDate     time.Time
}

func NewService(repo Repository, provider Provider, cfg ServiceConfig, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.SyncCooldown <= 0 {
		cfg.SyncCooldown = 10 * time.Minute
	}
	return &Service{
		repo:         repo,
		provider:     provider,
		logger:       logger.With("component", "lotto_service"),
		syncCooldown: cfg.SyncCooldown,
	}
}

func (s *Service) Summary(ctx context.Context, chatID string, now time.Time) (SummaryResult, error) {
	state, err := s.latestState(ctx, now)
	if err != nil {
		return SummaryResult{}, err
	}
	if state.draw == nil {
		return SummaryResult{}, fmt.Errorf("latest lotto draw is unavailable")
	}
	shops, err := s.ensureFirstPrizeShops(ctx, state.draw)
	if err != nil {
		return SummaryResult{}, err
	}
	tickets, err := s.repo.ListRoundTickets(ctx, state.draw.Round, true)
	if err != nil {
		return SummaryResult{}, err
	}
	waitingTickets, err := s.repo.ListRoundTickets(ctx, state.registrationRound, true)
	if err != nil {
		return SummaryResult{}, err
	}
	return SummaryResult{
		Draw:                state.draw,
		LatestUpdatePending: state.updatePending,
		Participants:        uniqueUserCount(tickets),
		Winners:             s.buildWinnerSummaries(ctx, tickets, state.draw, chatID),
		WaitingRound:        state.registrationRound,
		WaitingParticipants: uniqueUserCount(waitingTickets),
		FirstPrizeRegions:   buildRegionCounts(shops),
	}, nil
}

func (s *Service) Recommend(ctx context.Context, userID, chatID, sender string, now time.Time) (RecommendResult, error) {
	if err := s.rememberProfile(ctx, userID, chatID, sender, now); err != nil {
		return RecommendResult{}, err
	}
	state, err := s.latestState(ctx, now)
	if err != nil {
		return RecommendResult{}, err
	}
	existing, err := s.repo.ListUserTickets(ctx, userID, state.registrationRound, true)
	if err != nil {
		return RecommendResult{}, err
	}
	recommended := filterTicketsBySource(existing, TicketSourceRecommend)
	if len(recommended) > 0 {
		return RecommendResult{
			DisplayName: displayName(sender),
			Round:       state.registrationRound,
			DrawDate:    state.registrationDrawDate,
			Tickets:     recommended,
		}, nil
	}

	nextLineNo, err := s.repo.NextLineNo(ctx, userID, state.registrationRound)
	if err != nil {
		return RecommendResult{}, err
	}
	tickets := make([]TicketLine, 0, 5)
	for i := 0; i < 5; i++ {
		tickets = append(tickets, TicketLine{
			UserID:  strings.TrimSpace(userID),
			Round:   state.registrationRound,
			LineNo:  nextLineNo + i,
			Numbers: generateRandomNumbers(),
			Source:  TicketSourceRecommend,
			Status:  TicketStatusActive,
		})
	}
	if err := s.repo.InsertTickets(ctx, tickets); err != nil {
		return RecommendResult{}, err
	}
	return RecommendResult{
		DisplayName: displayName(sender),
		Round:       state.registrationRound,
		DrawDate:    state.registrationDrawDate,
		Tickets:     tickets,
	}, nil
}

func (s *Service) RegisterManual(ctx context.Context, userID, chatID, sender string, values []int, now time.Time) (RegisterResult, error) {
	if err := s.rememberProfile(ctx, userID, chatID, sender, now); err != nil {
		return RegisterResult{}, err
	}
	sets, err := normalizeManualNumberSets(values)
	if err != nil {
		return RegisterResult{}, err
	}

	state, err := s.latestState(ctx, now)
	if err != nil {
		return RegisterResult{}, err
	}
	existing, err := s.repo.ListUserTickets(ctx, userID, state.registrationRound, true)
	if err != nil {
		return RegisterResult{}, err
	}
	nextLineNo, err := s.repo.NextLineNo(ctx, userID, state.registrationRound)
	if err != nil {
		return RegisterResult{}, err
	}

	added := make([]TicketLine, 0, len(sets))
	for i, numbers := range sets {
		added = append(added, TicketLine{
			UserID:  strings.TrimSpace(userID),
			Round:   state.registrationRound,
			LineNo:  nextLineNo + i,
			Numbers: numbers,
			Source:  TicketSourceManual,
			Status:  TicketStatusActive,
		})
	}
	if err := s.repo.InsertTickets(ctx, added); err != nil {
		return RegisterResult{}, err
	}
	return RegisterResult{
		DisplayName: displayName(sender),
		Round:       state.registrationRound,
		DrawDate:    state.registrationDrawDate,
		TotalLines:  len(existing) + len(added),
		Added:       added,
	}, nil
}

func (s *Service) QueryMine(ctx context.Context, userID, chatID, sender string, now time.Time) (QueryResult, error) {
	if err := s.rememberProfile(ctx, userID, chatID, sender, now); err != nil {
		return QueryResult{}, err
	}
	state, err := s.latestState(ctx, now)
	if err != nil {
		return QueryResult{}, err
	}

	var latestResults []TicketMatch
	if state.draw != nil {
		latestTickets, err := s.repo.ListUserTickets(ctx, userID, state.draw.Round, true)
		if err != nil {
			return QueryResult{}, err
		}
		latestResults = evaluateTickets(state.draw, latestTickets)
	}

	var pendingTickets []TicketLine
	if state.pendingRound > 0 {
		pendingTickets, err = s.repo.ListUserTickets(ctx, userID, state.pendingRound, true)
		if err != nil {
			return QueryResult{}, err
		}
	}

	currentTickets, err := s.repo.ListUserTickets(ctx, userID, state.registrationRound, true)
	if err != nil {
		return QueryResult{}, err
	}

	return QueryResult{
		DisplayName:         displayName(sender),
		LatestOfficialRound: latestOfficialRound(state.draw),
		LatestDraw:          state.draw,
		LatestResults:       latestResults,
		PendingRound:        state.pendingRound,
		PendingDrawDate:     state.pendingDrawDate,
		PendingTickets:      pendingTickets,
		CurrentRound:        state.registrationRound,
		CurrentDrawDate:     state.registrationDrawDate,
		CurrentTickets:      currentTickets,
		AwaitingDraw:        (len(currentTickets) > 0 && state.registrationRound > latestOfficialRound(state.draw)) || len(pendingTickets) > 0,
	}, nil
}

func (s *Service) DeleteMine(ctx context.Context, userID, chatID, sender string, lineNo *int, now time.Time) (DeleteResult, error) {
	if err := s.rememberProfile(ctx, userID, chatID, sender, now); err != nil {
		return DeleteResult{}, err
	}
	state, err := s.latestState(ctx, now)
	if err != nil {
		return DeleteResult{}, err
	}

	var deleted []TicketLine
	if lineNo == nil {
		deleted, err = s.repo.DeactivateAllUserTickets(ctx, userID, state.registrationRound)
	} else {
		deleted, err = s.repo.DeactivateTicketLines(ctx, userID, state.registrationRound, []int{*lineNo})
	}
	if err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{
		DisplayName: displayName(sender),
		Round:       state.registrationRound,
		Deleted:     deleted,
	}, nil
}

func (s *Service) latestState(ctx context.Context, now time.Time) (latestDrawState, error) {
	draw, updatePending, err := s.ensureLatestDraw(ctx, now)
	if err != nil {
		return latestDrawState{}, err
	}
	if draw == nil {
		return latestDrawState{}, fmt.Errorf("latest lotto draw is unavailable")
	}

	registrationDate := openRegistrationDrawDate(now)
	weeksUntilRegistration := weeksBetween(drawDateOnly(draw.DrawDate), registrationDate)
	if weeksUntilRegistration < 1 {
		weeksUntilRegistration = 1
	}
	registrationRound := draw.Round + weeksUntilRegistration

	pendingRound := 0
	pendingDrawDate := time.Time{}
	if weeksUntilRegistration > 1 {
		pendingRound = draw.Round + 1
		pendingDrawDate = drawDateOnly(draw.DrawDate).AddDate(0, 0, 7)
	}

	return latestDrawState{
		draw:                 draw,
		updatePending:        updatePending,
		registrationRound:    registrationRound,
		registrationDrawDate: registrationDate,
		pendingRound:         pendingRound,
		pendingDrawDate:      pendingDrawDate,
	}, nil
}

func (s *Service) ensureLatestDraw(ctx context.Context, now time.Time) (*Draw, bool, error) {
	now = now.In(kstLocation())
	expectedDate := expectedPublishedDrawDate(now)

	latest, err := s.repo.LatestDraw(ctx)
	if err != nil {
		return nil, false, err
	}
	if latest != nil && sameDate(drawDateOnly(latest.DrawDate), expectedDate) {
		return latest, false, nil
	}
	if latest != nil && now.Sub(latest.UpdateTime.In(kstLocation())) < s.syncCooldown {
		return latest, !sameDate(drawDateOnly(latest.DrawDate), expectedDate), nil
	}

	fetched, fetchErr := s.provider.FetchLatestDraw(ctx)
	if fetchErr != nil {
		if latest != nil {
			_ = s.repo.TouchDrawUpdateTime(ctx, latest.Round, now)
			return latest, !sameDate(drawDateOnly(latest.DrawDate), expectedDate), nil
		}
		return nil, false, fetchErr
	}

	draw := mapProviderDraw(fetched)
	if err := s.repo.UpsertDraw(ctx, draw); err != nil {
		return nil, false, err
	}
	if err := s.syncFirstPrizeShops(ctx, draw.Round); err != nil {
		s.logger.Warn("sync first prize shops failed", "round", draw.Round, "error", err)
	}
	return &draw, !sameDate(drawDateOnly(draw.DrawDate), expectedDate), nil
}

func (s *Service) ensureFirstPrizeShops(ctx context.Context, draw *Draw) ([]FirstPrizeShop, error) {
	if draw == nil {
		return nil, nil
	}
	shops, err := s.repo.ListFirstPrizeShops(ctx, draw.Round)
	if err != nil {
		return nil, err
	}
	if len(shops) > 0 {
		return shops, nil
	}
	if err := s.syncFirstPrizeShops(ctx, draw.Round); err != nil {
		return nil, err
	}
	return s.repo.ListFirstPrizeShops(ctx, draw.Round)
}

func (s *Service) syncFirstPrizeShops(ctx context.Context, round int) error {
	items, err := s.provider.FetchFirstPrizeShops(ctx, round)
	if err != nil {
		return err
	}
	shops := make([]FirstPrizeShop, 0, len(items))
	for _, item := range items {
		shops = append(shops, FirstPrizeShop{
			Round:         round,
			ShopID:        item.ShopID,
			ShopName:      item.ShopName,
			Region:        item.Region,
			District:      item.District,
			Address1:      item.Address1,
			Address2:      item.Address2,
			Address3:      item.Address3,
			Address4:      item.Address4,
			FullAddress:   item.FullAddress,
			WinMethodCode: item.WinMethodCode,
			WinMethodText: item.WinMethodText,
			Latitude:      item.Latitude,
			Longitude:     item.Longitude,
		})
	}
	return s.repo.ReplaceFirstPrizeShops(ctx, round, shops)
}

func (s *Service) rememberProfile(ctx context.Context, userID, chatID, sender string, now time.Time) error {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(chatID) == "" || strings.TrimSpace(sender) == "" {
		return nil
	}
	return s.repo.UpsertUserRoomProfile(ctx, UserRoomProfile{
		UserID:     strings.TrimSpace(userID),
		ChatID:     strings.TrimSpace(chatID),
		SenderName: strings.TrimSpace(sender),
		LastSeenAt: now.In(kstLocation()),
	})
}

func (s *Service) buildWinnerSummaries(ctx context.Context, tickets []TicketLine, draw *Draw, chatID string) []WinnerSummary {
	if draw == nil || len(tickets) == 0 {
		return nil
	}
	bestByUser := make(map[string]WinnerSummary)
	for _, result := range evaluateTickets(draw, tickets) {
		if result.Rank <= 0 {
			continue
		}
		current, exists := bestByUser[result.Line.UserID]
		if exists && current.Rank <= result.Rank {
			continue
		}
		bestByUser[result.Line.UserID] = WinnerSummary{
			UserID:      result.Line.UserID,
			Rank:        result.Rank,
			PrizeAmount: result.PrizeAmount,
		}
	}

	winners := make([]WinnerSummary, 0, len(bestByUser))
	for userID, winner := range bestByUser {
		profile, err := s.repo.GetUserRoomProfile(ctx, userID, chatID)
		if err != nil {
			s.logger.Debug("lotto winner profile lookup failed", "user_id", userID, "chat_id", chatID, "error", err)
		}
		name := ""
		if profile != nil {
			name = profile.SenderName
		}
		winner.MaskedName = maskName(name)
		winners = append(winners, winner)
	}

	sort.Slice(winners, func(i, j int) bool {
		if winners[i].Rank != winners[j].Rank {
			return winners[i].Rank < winners[j].Rank
		}
		if winners[i].PrizeAmount != winners[j].PrizeAmount {
			return winners[i].PrizeAmount > winners[j].PrizeAmount
		}
		return winners[i].UserID < winners[j].UserID
	})
	if len(winners) > 10 {
		winners = winners[:10]
	}
	return winners
}

func evaluateTickets(draw *Draw, tickets []TicketLine) []TicketMatch {
	results := make([]TicketMatch, 0, len(tickets))
	for _, ticket := range tickets {
		results = append(results, evaluateTicket(draw, ticket))
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Line.LineNo < results[j].Line.LineNo
	})
	return results
}

func evaluateTicket(draw *Draw, ticket TicketLine) TicketMatch {
	winSet := make(map[int]struct{}, len(draw.Numbers))
	for _, value := range draw.Numbers {
		winSet[value] = struct{}{}
	}
	matchCount := 0
	bonusMatched := false
	for _, value := range ticket.Numbers {
		if value == draw.BonusNumber {
			bonusMatched = true
		}
		if _, ok := winSet[value]; ok {
			matchCount++
		}
	}

	result := TicketMatch{
		Line:         ticket,
		MatchCount:   matchCount,
		BonusMatched: bonusMatched,
	}
	switch {
	case matchCount == 6:
		result.Rank = 1
		result.PrizeAmount = draw.Rank1Prize
	case matchCount == 5 && bonusMatched:
		result.Rank = 2
		result.PrizeAmount = draw.Rank2Prize
	case matchCount == 5:
		result.Rank = 3
		result.PrizeAmount = draw.Rank3Prize
	case matchCount == 4:
		result.Rank = 4
		result.PrizeAmount = draw.Rank4Prize
	case matchCount == 3:
		result.Rank = 5
		result.PrizeAmount = draw.Rank5Prize
	}
	return result
}

func filterTicketsBySource(tickets []TicketLine, source TicketSource) []TicketLine {
	filtered := make([]TicketLine, 0, len(tickets))
	for _, ticket := range tickets {
		if ticket.Source == source {
			filtered = append(filtered, ticket)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].LineNo < filtered[j].LineNo
	})
	return filtered
}

func normalizeManualNumberSets(values []int) ([][6]int, error) {
	if len(values) == 0 || len(values)%6 != 0 {
		return nil, ErrInvalidNumbers
	}
	sets := make([][6]int, 0, len(values)/6)
	for i := 0; i < len(values); i += 6 {
		seen := make(map[int]struct{}, 6)
		setValues := make([]int, 0, 6)
		for _, value := range values[i : i+6] {
			if value < 1 || value > 45 {
				return nil, ErrInvalidNumbers
			}
			if _, ok := seen[value]; ok {
				return nil, ErrInvalidNumbers
			}
			seen[value] = struct{}{}
			setValues = append(setValues, value)
		}
		sets = append(sets, NormalizeNumbers(setValues))
	}
	return sets, nil
}

func generateRandomNumbers() [6]int {
	seen := make(map[int]struct{}, 6)
	values := make([]int, 0, 6)
	for len(values) < 6 {
		value := rand.IntN(45) + 1
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return NormalizeNumbers(values)
}

func buildRegionCounts(shops []FirstPrizeShop) []RegionCount {
	counts := make(map[string]int)
	for _, shop := range shops {
		counts[shop.RegionLabel()]++
	}
	regions := make([]RegionCount, 0, len(counts))
	for label, count := range counts {
		regions = append(regions, RegionCount{Label: label, Count: count})
	}
	sort.Slice(regions, func(i, j int) bool {
		return regions[i].Label < regions[j].Label
	})
	return regions
}

func uniqueUserCount(tickets []TicketLine) int {
	seen := make(map[string]struct{}, len(tickets))
	for _, ticket := range tickets {
		if strings.TrimSpace(ticket.UserID) == "" {
			continue
		}
		seen[ticket.UserID] = struct{}{}
	}
	return len(seen)
}

func maskName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "익****"
	}
	runes := []rune(name)
	if len(runes) == 0 {
		return "익****"
	}
	return string(runes[0]) + "****"
}

func displayName(sender string) string {
	sender = strings.TrimSpace(sender)
	if sender == "" {
		return "사용자"
	}
	return sender
}

func latestOfficialRound(draw *Draw) int {
	if draw == nil {
		return 0
	}
	return draw.Round
}

func mapProviderDraw(input *providers.DHLotteryDraw) Draw {
	return Draw{
		Round:        input.Round,
		DrawDate:     drawDateOnly(input.DrawDate),
		Numbers:      input.Numbers,
		BonusNumber:  input.BonusNumber,
		Rank1Winners: input.Rank1Winners,
		Rank1Prize:   input.Rank1Prize,
		Rank2Winners: input.Rank2Winners,
		Rank2Prize:   input.Rank2Prize,
		Rank3Winners: input.Rank3Winners,
		Rank3Prize:   input.Rank3Prize,
		Rank4Winners: input.Rank4Winners,
		Rank4Prize:   input.Rank4Prize,
		Rank5Winners: input.Rank5Winners,
		Rank5Prize:   input.Rank5Prize,
		TotalWinners: input.TotalWinners,
		TotalSales:   input.TotalSales,
	}
}

func expectedPublishedDrawDate(now time.Time) time.Time {
	now = now.In(kstLocation())
	latestSaturday := recentSaturday(now)
	if now.Weekday() == time.Saturday && beforeClock(now, 21, 30) {
		return latestSaturday.AddDate(0, 0, -7)
	}
	return latestSaturday
}

func openRegistrationDrawDate(now time.Time) time.Time {
	now = now.In(kstLocation())
	start := startOfDay(now)
	daysUntilSaturday := (int(time.Saturday) - int(now.Weekday()) + 7) % 7
	target := start.AddDate(0, 0, daysUntilSaturday)
	if now.Weekday() == time.Saturday && !beforeClock(now, 20, 0) {
		target = target.AddDate(0, 0, 7)
	}
	return target
}

func recentSaturday(now time.Time) time.Time {
	now = now.In(kstLocation())
	daysSinceSaturday := (int(now.Weekday()) - int(time.Saturday) + 7) % 7
	return startOfDay(now).AddDate(0, 0, -daysSinceSaturday)
}

func startOfDay(ts time.Time) time.Time {
	ts = ts.In(kstLocation())
	return time.Date(ts.Year(), ts.Month(), ts.Day(), 0, 0, 0, 0, ts.Location())
}

func drawDateOnly(ts time.Time) time.Time {
	return startOfDay(ts)
}

func weeksBetween(from, to time.Time) int {
	from = drawDateOnly(from)
	to = drawDateOnly(to)
	if !to.After(from) {
		return 0
	}
	return int(to.Sub(from).Hours() / (24 * 7))
}

func sameDate(a, b time.Time) bool {
	a = drawDateOnly(a)
	b = drawDateOnly(b)
	return a.Equal(b)
}

func beforeClock(now time.Time, hour, minute int) bool {
	current := now.Hour()*60 + now.Minute()
	target := hour*60 + minute
	return current < target
}

func kstLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil || loc == nil {
		return time.FixedZone("KST", 9*60*60)
	}
	return loc
}
