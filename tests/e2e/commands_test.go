package e2e

import (
	"fmt"
	"strings"
	"testing"
)

type commandTestCase struct {
	name    string
	msg     string
	wantMin int // minimum expected replies (0 = any including empty)
}

// coreCommands are the essential commands that MUST pass for deployment.
var coreCommands = []commandTestCase{
	{name: "help", msg: "도움", wantMin: 1},
	{name: "coin/bitcoin", msg: "비트코인", wantMin: 1},
	{name: "stock/samsung", msg: "삼성전자", wantMin: 1},
	{name: "forex", msg: "환율", wantMin: 1},
	{name: "gold", msg: "금시세", wantMin: 1},
	{name: "weather", msg: "날씨 서울", wantMin: 1},
	{name: "news", msg: "뉴스", wantMin: 1},
	{name: "trending", msg: "실검", wantMin: 1},
}

// extendedCommands are optional commands that may fail due to external API issues.
var extendedCommands = []commandTestCase{
	{name: "coin/ethereum", msg: "이더리움", wantMin: 1},
	{name: "football/epl", msg: "축구 EPL", wantMin: 1},
	{name: "esports/lck", msg: "롤 LCK", wantMin: 1},
	{name: "baseball/kbo", msg: "야구 KBO", wantMin: 1},
	{name: "index/kospi", msg: "코스피", wantMin: 1},
	{name: "index/kosdaq", msg: "코스닥", wantMin: 1},
	{name: "index/nasdaq", msg: "나스닥", wantMin: 1},
	{name: "index/dow", msg: "다우", wantMin: 1},
	{name: "index/explicit", msg: "지수 S&P500", wantMin: 1},
}

func TestCoreCommands(t *testing.T) {
	var failures int
	for _, tc := range coreCommands {
		t.Run(tc.name, func(t *testing.T) {
			result := sendMessage(t, tc.msg)
			if tc.wantMin > 0 && len(result.Replies) < tc.wantMin {
				failures++
				t.Errorf("expected at least %d replies, got %d", tc.wantMin, len(result.Replies))
				return
			}
			// Check that the reply doesn't look like an error.
			for _, reply := range result.Replies {
				data := fmt.Sprintf("%v", reply.Data)
				if strings.Contains(data, "알 수 없는 명령어") {
					t.Errorf("got unknown command reply for %q", tc.msg)
				}
			}
		})
	}
}

func TestExtendedCommands(t *testing.T) {
	var total, failures int
	for _, tc := range extendedCommands {
		total++
		t.Run(tc.name, func(t *testing.T) {
			result := sendMessage(t, tc.msg)
			if tc.wantMin > 0 && len(result.Replies) < tc.wantMin {
				failures++
				t.Logf("[WARN] %s: expected at least %d replies, got %d (external API may be down)",
					tc.name, tc.wantMin, len(result.Replies))
			}
		})
	}

	// Extended commands use a failure threshold — don't fail the suite
	// unless more than 50% of commands fail.
	if total > 0 {
		failureRate := float64(failures) / float64(total)
		threshold := 0.5
		if failureRate > threshold {
			t.Errorf("extended command failure rate %.0f%% exceeds threshold %.0f%%",
				failureRate*100, threshold*100)
		}
	}
}
