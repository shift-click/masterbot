package main

import (
	"regexp"
	"testing"

	"github.com/shift-click/masterbot/internal/metrics"
)

const testSecret = "fragmentation-test-secret"

var fallbackLabelPattern = regexp.MustCompile(`^KAKAO-(직접|그룹) #[0-9a-f]{4}$`)

func TestExtractRoomNameUsesMetaTitle(t *testing.T) {
	t.Parallel()

	got := extractRoomName(
		"MultiChat",
		`[{"type":1,"content":"ignored"},{"type":3,"content":"무나짱"}]`,
		"chat-1",
		testSecret,
	)
	if got != "무나짱" {
		t.Fatalf("extractRoomName = %q, want %q", got, "무나짱")
	}
}

func TestExtractRoomNameFallbackHasShortHashSuffix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		roomType string
		chatID   string
		base     string
	}{
		{name: "direct", roomType: "DirectChat", chatID: "chat-direct-1", base: "KAKAO-직접"},
		{name: "group", roomType: "MultiChat", chatID: "chat-group-1", base: "KAKAO-그룹"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := extractRoomName(tc.roomType, ``, tc.chatID, testSecret)
			if !fallbackLabelPattern.MatchString(got) {
				t.Fatalf("fallback label %q does not match pattern", got)
			}
			if got[:len(tc.base)] != tc.base {
				t.Fatalf("fallback label %q missing base %q", got, tc.base)
			}
			if got == tc.base {
				t.Fatalf("fallback label %q is missing hash suffix", got)
			}
		})
	}
}

func TestExtractRoomNameFallbackIsDeterministic(t *testing.T) {
	t.Parallel()

	first := extractRoomName("DirectChat", ``, "stable-chat-id", testSecret)
	second := extractRoomName("DirectChat", ``, "stable-chat-id", testSecret)
	if first != second {
		t.Fatalf("fallback label not deterministic: %q vs %q", first, second)
	}
}

func TestExtractRoomNameDistinctChatIDsGetDistinctSuffixes(t *testing.T) {
	t.Parallel()

	a := extractRoomName("DirectChat", ``, "chat-A", testSecret)
	b := extractRoomName("DirectChat", ``, "chat-B", testSecret)
	if a == b {
		t.Fatalf("expected distinct labels for distinct chat ids, got %q == %q", a, b)
	}
}

func TestExtractRoomNameFallbackWithoutChatIDOmitsSuffix(t *testing.T) {
	t.Parallel()

	got := extractRoomName("DirectChat", ``, "", testSecret)
	if got != "KAKAO-직접" {
		t.Fatalf("expected base label without suffix when chat id is empty, got %q", got)
	}
}

func TestExtractRoomNameFallbackDoesNotLeakRawChatID(t *testing.T) {
	t.Parallel()

	chatID := "9876543210"
	got := extractRoomName("MultiChat", ``, chatID, testSecret)
	for i := 0; i+4 <= len(chatID); i++ {
		fragment := chatID[i : i+4]
		if containsString(got, fragment) {
			t.Fatalf("fallback label %q must not embed raw chat id fragment %q", got, fragment)
		}
	}
}

func TestExtractRoomNameUnknownTypeReturnsEmpty(t *testing.T) {
	t.Parallel()

	if got := extractRoomName("OpenChat", ``, "chat-x", testSecret); got != "" {
		t.Fatalf("unknown type fallback = %q, want empty", got)
	}
}

func TestRoomShortHashHelperBoundaryCases(t *testing.T) {
	t.Parallel()

	if got := metrics.RoomShortHash(testSecret, "", 4); got != "" {
		t.Fatalf("empty chat id should yield empty short hash, got %q", got)
	}
	if got := metrics.RoomShortHash(testSecret, "chat-1", 0); got != "" {
		t.Fatalf("zero length should yield empty short hash, got %q", got)
	}
	full := metrics.RoomShortHash(testSecret, "chat-1", 1024)
	if len(full) == 0 || len(full) > 64 {
		t.Fatalf("oversized length should clamp to full hash length, got len=%d", len(full))
	}
}

func containsString(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
