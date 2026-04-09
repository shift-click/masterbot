package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/lotto"
)

func TestSQLiteLottoStoreDrawAndProfileLifecycle(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteLottoStore(filepath.Join(t.TempDir(), "lotto.db"))
	if err != nil {
		t.Fatalf("NewSQLiteLottoStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	draw := lotto.Draw{
		Round:        1218,
		DrawDate:     time.Date(2026, 4, 4, 0, 0, 0, 0, time.FixedZone("KST", 9*60*60)),
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
		TotalWinners: 2754132,
		TotalSales:   61667966400,
	}
	if err := store.UpsertDraw(ctx, draw); err != nil {
		t.Fatalf("UpsertDraw() error = %v", err)
	}

	latest, err := store.LatestDraw(ctx)
	if err != nil {
		t.Fatalf("LatestDraw() error = %v", err)
	}
	if latest == nil || latest.Round != 1218 {
		t.Fatalf("latest round = %+v, want 1218", latest)
	}

	if err := store.ReplaceFirstPrizeShops(ctx, 1218, []lotto.FirstPrizeShop{{
		Round:        1218,
		ShopID:       "11140694",
		ShopName:     "꿈이있는 로또점(복권판매점)",
		Region:       "서울",
		District:     "은평구",
		FullAddress:  "서울 은평구 통일로 699 1층",
		WinMethodText: "자동",
	}}); err != nil {
		t.Fatalf("ReplaceFirstPrizeShops() error = %v", err)
	}

	shops, err := store.ListFirstPrizeShops(ctx, 1218)
	if err != nil {
		t.Fatalf("ListFirstPrizeShops() error = %v", err)
	}
	if len(shops) != 1 {
		t.Fatalf("len(shops) = %d, want 1", len(shops))
	}

	if err := store.UpsertUserRoomProfile(ctx, lotto.UserRoomProfile{
		UserID:     "user-1",
		ChatID:     "room-1",
		SenderName: "홍길동",
		LastSeenAt: time.Now(),
	}); err != nil {
		t.Fatalf("UpsertUserRoomProfile() error = %v", err)
	}

	profile, err := store.GetUserRoomProfile(ctx, "user-1", "room-1")
	if err != nil {
		t.Fatalf("GetUserRoomProfile() error = %v", err)
	}
	if profile == nil || profile.SenderName != "홍길동" {
		t.Fatalf("profile = %+v, want sender 홍길동", profile)
	}
}

func TestSQLiteLottoStoreTicketLifecycle(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteLottoStore(filepath.Join(t.TempDir(), "tickets.db"))
	if err != nil {
		t.Fatalf("NewSQLiteLottoStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	if err := store.InsertTickets(ctx, []lotto.TicketLine{
		{
			UserID:  "user-1",
			Round:   1219,
			LineNo:  1,
			Numbers: [6]int{1, 2, 3, 4, 5, 6},
			Source:  lotto.TicketSourceRecommend,
			Status:  lotto.TicketStatusActive,
		},
		{
			UserID:  "user-1",
			Round:   1219,
			LineNo:  2,
			Numbers: [6]int{7, 8, 9, 10, 11, 12},
			Source:  lotto.TicketSourceManual,
			Status:  lotto.TicketStatusActive,
		},
	}); err != nil {
		t.Fatalf("InsertTickets() error = %v", err)
	}

	nextLineNo, err := store.NextLineNo(ctx, "user-1", 1219)
	if err != nil {
		t.Fatalf("NextLineNo() error = %v", err)
	}
	if nextLineNo != 3 {
		t.Fatalf("NextLineNo() = %d, want 3", nextLineNo)
	}

	deletedOne, err := store.DeactivateTicketLines(ctx, "user-1", 1219, []int{2})
	if err != nil {
		t.Fatalf("DeactivateTicketLines() error = %v", err)
	}
	if len(deletedOne) != 1 || deletedOne[0].LineNo != 2 {
		t.Fatalf("deletedOne = %+v", deletedOne)
	}

	activeTickets, err := store.ListUserTickets(ctx, "user-1", 1219, true)
	if err != nil {
		t.Fatalf("ListUserTickets(active) error = %v", err)
	}
	if len(activeTickets) != 1 || activeTickets[0].LineNo != 1 {
		t.Fatalf("active tickets = %+v", activeTickets)
	}

	deletedAll, err := store.DeactivateAllUserTickets(ctx, "user-1", 1219)
	if err != nil {
		t.Fatalf("DeactivateAllUserTickets() error = %v", err)
	}
	if len(deletedAll) != 1 || deletedAll[0].LineNo != 1 {
		t.Fatalf("deletedAll = %+v", deletedAll)
	}
}
