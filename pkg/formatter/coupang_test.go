package formatter

import (
	"strings"
	"testing"
	"time"
)

func TestFormatCoupangPriceUsesComparativeMinForSparseHistory(t *testing.T) {
	t.Parallel()

	got := FormatCoupangPrice(CoupangPriceData{
		Name:                "오뚜기콕콕콕 라면볶이 용기 120 g, 12개",
		CurrentPrice:        8740,
		MinPrice:            7820,
		MaxPrice:            9380,
		AvgPrice:            8740,
		Prices:              []int{8740},
		HistorySpanDays:     1,
		SampleCount:         1,
		ComparativeMinPrice: 7820,
	})

	if !strings.Contains(got, "현재가 8,740원") {
		t.Fatalf("missing current price: %q", got)
	}
	if !strings.Contains(got, "최저가 7,820원") {
		t.Fatalf("missing lowest price: %q", got)
	}
	if !strings.Contains(got, "+11.8%") {
		t.Fatalf("missing lowest price delta: %q", got)
	}
	assertNoLegacyCoupangCopy(t, got)
}

func TestFormatCoupangPriceShowsMinimalStaleLine(t *testing.T) {
	t.Parallel()

	got := FormatCoupangPrice(CoupangPriceData{
		Name:                "테스트 상품",
		CurrentPrice:        9800,
		SampleCount:         1,
		ComparativeMinPrice: 9200,
		IsStale:             true,
		LastObservedAt:      time.Now().Add(-2 * time.Hour),
		RefreshStatus:       "timed_out",
		RefreshRequested:    true,
	})

	if !strings.Contains(got, "최저가 9,200원") {
		t.Fatalf("missing lowest price: %q", got)
	}
	if !strings.Contains(got, "+6.5%") {
		t.Fatalf("missing delta: %q", got)
	}
	if !strings.Contains(got, "최근 확인 2시간 전") {
		t.Fatalf("missing stale observed label: %q", got)
	}
	assertNoLegacyCoupangCopy(t, got)
}

func TestFormatCoupangPriceUsesLocalMinWhenHistoryIsSufficient(t *testing.T) {
	t.Parallel()

	got := FormatCoupangPrice(CoupangPriceData{
		Name:                "테스트 상품",
		CurrentPrice:        21900,
		MinPrice:            21900,
		MaxPrice:            25000,
		AvgPrice:            23300,
		Prices:              []int{25000, 23000, 21900},
		HistorySpanDays:     3,
		SampleCount:         3,
		ComparativeMinPrice: 18240,
		StatsEligible:       true,
	})

	if !strings.Contains(got, "현재가 21,900원") {
		t.Fatalf("missing current price: %q", got)
	}
	if !strings.Contains(got, "최저가 21,900원") {
		t.Fatalf("missing local lowest price: %q", got)
	}
	if !strings.Contains(got, "0.0%") {
		t.Fatalf("missing zero delta: %q", got)
	}
	if strings.Contains(got, "최저가 18,240원") {
		t.Fatalf("should not use comparative minimum when stats are eligible: %q", got)
	}
	assertNoLegacyCoupangCopy(t, got)
}

func TestFormatCoupangPriceSupportsNegativeLowestPriceDelta(t *testing.T) {
	t.Parallel()

	got := FormatCoupangPrice(CoupangPriceData{
		Name:                "테스트 상품",
		CurrentPrice:        9700,
		ComparativeMinPrice: 10000,
	})

	if !strings.Contains(got, "-3.0%") {
		t.Fatalf("missing negative delta: %q", got)
	}
}

func TestFormatCoupangPriceReturnsCurrentPriceOnlyWithoutLowestReference(t *testing.T) {
	t.Parallel()

	got := FormatCoupangPrice(CoupangPriceData{
		Name:          "테스트 상품",
		CurrentPrice:  12345,
		MinPrice:      12000,
		StatsEligible: false,
	})

	if !strings.Contains(got, "현재가 12,345원") {
		t.Fatalf("missing current price: %q", got)
	}
	if strings.Contains(got, "최저가 ") {
		t.Fatalf("unexpected lowest price line: %q", got)
	}
}

func TestFormatCoupangPriceAppendsDeferredNotice(t *testing.T) {
	t.Parallel()

	got := FormatCoupangPrice(CoupangPriceData{
		Name:                "테스트 상품",
		CurrentPrice:        12345,
		ComparativeMinPrice: 12000,
		DeferredNotice:      true,
	})

	if !strings.Contains(got, "⏳ 가격 이력 보강 중") {
		t.Fatalf("missing deferred notice: %q", got)
	}
}

func assertNoLegacyCoupangCopy(t *testing.T, got string) {
	t.Helper()

	unexpected := []string{
		"폴센트",
		"로컬 표본",
		"최근 관측 기준 최신 응답입니다",
		"가격 추적을 시작했습니다",
		"추이를 함께 보여드릴게요",
		"백그라운드에서 다시 확인 중입니다",
	}
	for _, s := range unexpected {
		if strings.Contains(got, s) {
			t.Fatalf("unexpected legacy copy %q in %q", s, got)
		}
	}
}
