package lotto_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	lotto "github.com/shift-click/masterbot/internal/lotto"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/store"
)

type stubLottoProvider struct {
	latestDraw *providers.DHLotteryDraw
	shops      []providers.DHLotteryFirstPrizeShop
}

func (s stubLottoProvider) FetchLatestDraw(context.Context) (*providers.DHLotteryDraw, error) {
	return s.latestDraw, nil
}

func (s stubLottoProvider) FetchFirstPrizeShops(context.Context, int) ([]providers.DHLotteryFirstPrizeShop, error) {
	return append([]providers.DHLotteryFirstPrizeShop(nil), s.shops...), nil
}

func TestLottoServiceRecommendAndQueryAcrossRooms(t *testing.T) {
	t.Parallel()

	kst := time.FixedZone("KST", 9*60*60)
	repo, err := store.NewSQLiteLottoStore(filepath.Join(t.TempDir(), "lotto.db"))
	if err != nil {
		t.Fatalf("NewSQLiteLottoStore() error = %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	service := lotto.NewService(repo, stubLottoProvider{
		latestDraw: &providers.DHLotteryDraw{
			Round:       1218,
			DrawDate:    time.Date(2026, 4, 4, 0, 0, 0, 0, kst),
			Numbers:     [6]int{3, 28, 31, 32, 42, 45},
			BonusNumber: 25,
			Rank1Prize:  1714482042,
			Rank2Prize:  64293077,
			Rank3Prize:  1780356,
			Rank4Prize:  50000,
			Rank5Prize:  5000,
		},
	}, lotto.ServiceConfig{SyncCooldown: time.Second}, nil)

	now := time.Date(2026, 4, 8, 10, 0, 0, 0, kst)
	first, err := service.Recommend(context.Background(), "user-1", "room-1", "홍길동", now)
	if err != nil {
		t.Fatalf("Recommend() error = %v", err)
	}
	if first.Round != 1219 || len(first.Tickets) != 5 {
		t.Fatalf("first recommend = %+v", first)
	}

	second, err := service.Recommend(context.Background(), "user-1", "room-1", "홍길동", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Recommend() second error = %v", err)
	}
	if len(second.Tickets) != 5 {
		t.Fatalf("second tickets len = %d, want 5", len(second.Tickets))
	}
	for i := range first.Tickets {
		if first.Tickets[i].Numbers != second.Tickets[i].Numbers {
			t.Fatalf("ticket %d changed across recommend calls: %v != %v", i, first.Tickets[i].Numbers, second.Tickets[i].Numbers)
		}
	}

	query, err := service.QueryMine(context.Background(), "user-1", "room-3", "코딩조아", now)
	if err != nil {
		t.Fatalf("QueryMine() error = %v", err)
	}
	if query.DisplayName != "코딩조아" {
		t.Fatalf("display name = %q, want 코딩조아", query.DisplayName)
	}
	if query.LatestOfficialRound != 1218 || query.CurrentRound != 1219 {
		t.Fatalf("query rounds = latest %d current %d", query.LatestOfficialRound, query.CurrentRound)
	}
	if len(query.CurrentTickets) != 5 || !query.AwaitingDraw {
		t.Fatalf("query current tickets = %d awaiting=%v", len(query.CurrentTickets), query.AwaitingDraw)
	}
}

func TestLottoServiceQueryLatestResultsAndSummary(t *testing.T) {
	t.Parallel()

	kst := time.FixedZone("KST", 9*60*60)
	repo, err := store.NewSQLiteLottoStore(filepath.Join(t.TempDir(), "lotto.db"))
	if err != nil {
		t.Fatalf("NewSQLiteLottoStore() error = %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	service := lotto.NewService(repo, stubLottoProvider{
		latestDraw: &providers.DHLotteryDraw{
			Round:        1218,
			DrawDate:     time.Date(2026, 4, 4, 0, 0, 0, 0, kst),
			Numbers:      [6]int{3, 28, 31, 32, 42, 45},
			BonusNumber:  25,
			Rank1Winners: 18,
			Rank1Prize:   1714482042,
			Rank2Winners: 80,
			Rank2Prize:   64293077,
			Rank3Winners: 2889,
			Rank3Prize:   1780356,
			Rank4Winners: 150326,
			Rank4Prize:   50000,
			Rank5Winners: 2600819,
			Rank5Prize:   5000,
		},
		shops: []providers.DHLotteryFirstPrizeShop{
			{ShopID: "11140694", ShopName: "꿈이있는 로또점", Region: "서울", District: "은평구", FullAddress: "서울 은평구", WinMethodText: "자동"},
		},
	}, lotto.ServiceConfig{SyncCooldown: time.Second}, nil)

	ctx := context.Background()
	now := time.Date(2026, 4, 6, 13, 0, 0, 0, kst)
	if err := repo.UpsertUserRoomProfile(ctx, lotto.UserRoomProfile{
		UserID:     "user-1",
		ChatID:     "room-1",
		SenderName: "홍길동",
		LastSeenAt: now,
	}); err != nil {
		t.Fatalf("UpsertUserRoomProfile() error = %v", err)
	}
	if err := repo.InsertTickets(ctx, []lotto.TicketLine{
		{
			UserID:  "user-1",
			Round:   1218,
			LineNo:  1,
			Numbers: [6]int{3, 28, 31, 32, 42, 45},
			Source:  lotto.TicketSourceManual,
			Status:  lotto.TicketStatusActive,
		},
		{
			UserID:  "user-2",
			Round:   1218,
			LineNo:  1,
			Numbers: [6]int{1, 2, 3, 4, 5, 6},
			Source:  lotto.TicketSourceManual,
			Status:  lotto.TicketStatusActive,
		},
	}); err != nil {
		t.Fatalf("InsertTickets() error = %v", err)
	}

	query, err := service.QueryMine(ctx, "user-1", "room-1", "홍길동", now)
	if err != nil {
		t.Fatalf("QueryMine() error = %v", err)
	}
	if len(query.LatestResults) != 1 || query.LatestResults[0].Rank != 1 {
		t.Fatalf("latest results = %+v", query.LatestResults)
	}

	summary, err := service.Summary(ctx, "room-1", now)
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if summary.Participants != 2 {
		t.Fatalf("participants = %d, want 2", summary.Participants)
	}
	if len(summary.Winners) != 1 || summary.Winners[0].MaskedName != "홍****" {
		t.Fatalf("winners = %+v", summary.Winners)
	}
	if summary.WaitingRound != 1219 {
		t.Fatalf("waiting round = %d, want 1219", summary.WaitingRound)
	}
	if len(summary.FirstPrizeRegions) != 1 || summary.FirstPrizeRegions[0].Label != "서울 은평구" {
		t.Fatalf("regions = %+v", summary.FirstPrizeRegions)
	}
}
