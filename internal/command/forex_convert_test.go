package command

import (
	"context"
	"strings"
	"testing"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/scraper/providers"
	"github.com/shift-click/masterbot/internal/transport"
)

func TestParseCurrencyExpressions(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []currencyMatch
	}{
		{
			name:  "simple USD",
			input: "100달러",
			want:  []currencyMatch{{Amount: 100, Code: "USD"}},
		},
		{
			name:  "USD with 불",
			input: "50불",
			want:  []currencyMatch{{Amount: 50, Code: "USD"}},
		},
		{
			name:  "VND with 만",
			input: "10만동",
			want:  []currencyMatch{{Amount: 100_000, Code: "VND"}},
		},
		{
			name:  "JPY with decimal and 만",
			input: "1.5만엔",
			want:  []currencyMatch{{Amount: 15_000, Code: "JPY"}},
		},
		{
			name:  "CNY simple",
			input: "20위안",
			want:  []currencyMatch{{Amount: 20, Code: "CNY"}},
		},
		{
			name:  "CNY 원안 alias",
			input: "20원안",
			want:  []currencyMatch{{Amount: 20, Code: "CNY"}},
		},
		{
			name:  "EUR",
			input: "50유로",
			want:  []currencyMatch{{Amount: 50, Code: "EUR"}},
		},
		{
			name:  "THB 바트",
			input: "200바트",
			want:  []currencyMatch{{Amount: 200, Code: "THB"}},
		},
		{
			name:  "THB 밧",
			input: "200밧",
			want:  []currencyMatch{{Amount: 200, Code: "THB"}},
		},
		{
			name:  "TWD 대만달러",
			input: "100대만달러",
			want:  []currencyMatch{{Amount: 100, Code: "TWD"}},
		},
		{
			name:  "HKD 홍콩달러",
			input: "100홍콩달러",
			want:  []currencyMatch{{Amount: 100, Code: "HKD"}},
		},
		{
			name:  "multiple currencies",
			input: "100달러랑 200위안",
			want: []currencyMatch{
				{Amount: 100, Code: "USD"},
				{Amount: 200, Code: "CNY"},
			},
		},
		{
			name:  "currency in sentence",
			input: "100달러짜리 선물 뭐가 좋을까?",
			want:  []currencyMatch{{Amount: 100, Code: "USD"}},
		},
		{
			name:  "천 unit",
			input: "5천엔",
			want:  []currencyMatch{{Amount: 5_000, Code: "JPY"}},
		},
		{
			name:  "억 unit",
			input: "1억동",
			want:  []currencyMatch{{Amount: 100_000_000, Code: "VND"}},
		},
		{
			name:  "no number - no match",
			input: "달러가 떨어졌다",
			want:  nil,
		},
		{
			name:  "KRW ignored - 원 not in alias",
			input: "100만원",
			want:  nil,
		},
		{
			name:  "empty string",
			input: "",
			want:  nil,
		},
		{
			name:  "number with space before alias",
			input: "100 달러",
			want:  []currencyMatch{{Amount: 100, Code: "USD"}},
		},
		{
			name:  "long alias priority - 홍콩달러 over 달러",
			input: "100홍콩달러",
			want:  []currencyMatch{{Amount: 100, Code: "HKD"}},
		},
		{
			name:  "long alias priority - 대만불 over 불",
			input: "50대만불",
			want:  []currencyMatch{{Amount: 50, Code: "TWD"}},
		},
		{
			name:  "decimal without multiplier",
			input: "99.5달러",
			want:  []currencyMatch{{Amount: 99.5, Code: "USD"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCurrencyExpressions(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d matches, want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i].Amount != tt.want[i].Amount {
					t.Errorf("match[%d].Amount = %f, want %f", i, got[i].Amount, tt.want[i].Amount)
				}
				if got[i].Code != tt.want[i].Code {
					t.Errorf("match[%d].Code = %s, want %s", i, got[i].Code, tt.want[i].Code)
				}
			}
		})
	}
}

func TestForexConvertHandler_HandleFallback(t *testing.T) {
	forex := providers.NewDunamuForex(nil)
	// Seed rates into the provider.
	forex.SetRatesForTest(providers.MultiForexRates{
		Rates: map[string]providers.CurrencyRate{
			"USD": {BasePrice: 1485.80, CurrencyUnit: 1, CurrencyCode: "USD"},
			"JPY": {BasePrice: 935.11, CurrencyUnit: 100, CurrencyCode: "JPY"},
			"CNY": {BasePrice: 215.93, CurrencyUnit: 1, CurrencyCode: "CNY"},
			"VND": {BasePrice: 5.65, CurrencyUnit: 100, CurrencyCode: "VND"},
		},
	})

	handler := NewForexConvertHandler(forex)

	t.Run("match returns ErrHandled", func(t *testing.T) {
		var replied string
		cmd := bot.CommandContext{
			Message: transport.Message{Msg: "100달러"},
			Reply: func(_ context.Context, r bot.Reply) error {
				replied = r.Text
				return nil
			},
		}

		err := handler.HandleFallback(context.Background(), cmd)
		if err != bot.ErrHandled {
			t.Fatalf("expected ErrHandled, got %v", err)
		}
		if replied == "" {
			t.Fatal("expected reply text, got empty")
		}
		// Should contain the converted amount
		if !strings.Contains(replied, "148,580") {
			t.Errorf("reply should contain 148,580원, got: %s", replied)
		}
	})

	t.Run("no match returns nil", func(t *testing.T) {
		cmd := bot.CommandContext{
			Message: transport.Message{Msg: "안녕하세요"},
			Reply: func(_ context.Context, r bot.Reply) error {
				t.Fatal("should not reply")
				return nil
			},
		}

		err := handler.HandleFallback(context.Background(), cmd)
		if err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("empty rates returns nil", func(t *testing.T) {
		emptyForex := providers.NewDunamuForex(nil)
		h := NewForexConvertHandler(emptyForex)

		cmd := bot.CommandContext{
			Message: transport.Message{Msg: "100달러"},
			Reply: func(_ context.Context, r bot.Reply) error {
				t.Fatal("should not reply when no rates")
				return nil
			},
		}

		err := h.HandleFallback(context.Background(), cmd)
		if err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("multiple currencies in one message", func(t *testing.T) {
		var replied string
		cmd := bot.CommandContext{
			Message: transport.Message{Msg: "100달러랑 20위안"},
			Reply: func(_ context.Context, r bot.Reply) error {
				replied = r.Text
				return nil
			},
		}

		err := handler.HandleFallback(context.Background(), cmd)
		if err != bot.ErrHandled {
			t.Fatalf("expected ErrHandled, got %v", err)
		}
		if !strings.Contains(replied, "148,580") || !strings.Contains(replied, "4,319") {
			t.Errorf("reply should contain both conversions, got: %s", replied)
		}
	})
}

