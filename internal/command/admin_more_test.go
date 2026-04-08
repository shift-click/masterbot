package command

import (
	"context"
	"strings"
	"testing"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/pkg/formatter"
)

func TestAdminHandlerPolicyViewAndModeParsing(t *testing.T) {
	t.Parallel()

	handler, _, autoQueries, cleanup := newAdminHarnessWithManager(t)
	defer cleanup()

	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 정책 보기",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "사용법: 관리 정책 보기") {
		t.Fatalf("usage reply = %q", reply.Text)
	}

	reply = executeAdmin(t, handler, transport.Message{
		Msg: "관리 정책 보기 room-1",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "source: default") {
		t.Fatalf("default policy view reply = %q", reply.Text)
	}

	_, err := autoQueries.UpdateRoomPolicy(context.Background(), bot.AutoQueryActor{ChatID: "admin-room", UserID: "bootstrap-user"}, "room-1", bot.AutoQueryPolicy{
		Mode:              bot.AutoQueryModeLocalAuto,
		AllowedHandlers:   []string{"coin"},
		BudgetPerHour:     30,
		CooldownWindow:    30,
		DegradationTarget: bot.AutoQueryModeExplicitOnly,
	})
	if err != nil {
		t.Fatalf("update room policy: %v", err)
	}
	reply = executeAdmin(t, handler, transport.Message{
		Msg: "관리 정책 보기 room-1",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "source: runtime") || !strings.Contains(reply.Text, "mode: local-auto") {
		t.Fatalf("runtime policy view reply = %q", reply.Text)
	}

	if mode, ok := parseAutoQueryMode(" explicit-only "); !ok || mode != bot.AutoQueryModeExplicitOnly {
		t.Fatalf("parseAutoQueryMode explicit-only = (%s,%v)", mode, ok)
	}
	if mode, ok := parseAutoQueryMode("local-auto"); !ok || mode != bot.AutoQueryModeLocalAuto {
		t.Fatalf("parseAutoQueryMode local-auto = (%s,%v)", mode, ok)
	}
	if _, ok := parseAutoQueryMode("off"); ok {
		t.Fatal("parseAutoQueryMode(off) should fail")
	}
}

func TestAdminHandlerPrincipalMutationsAndHelpers(t *testing.T) {
	t.Parallel()

	handler, manager, _, cleanup := newAdminHarnessWithManager(t)
	defer cleanup()

	reply := executeAdmin(t, handler, transport.Message{
		Msg: "관리 관리자 목록",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "관리자 목록") {
		t.Fatalf("principal list reply = %q", reply.Text)
	}

	reply = executeAdmin(t, handler, transport.Message{
		Msg: "관리 관리자 방 목록",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "관리자 방") {
		t.Fatalf("admin room list reply = %q", reply.Text)
	}

	reply = executeAdmin(t, handler, transport.Message{
		Msg: "관리 관리자 방 추가",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "사용법: 관리 관리자 방 추가") {
		t.Fatalf("admin room usage reply = %q", reply.Text)
	}

	reply = executeAdmin(t, handler, transport.Message{
		Msg: "관리 관리자 방 추가 room-extra",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "추가했습니다") {
		t.Fatalf("admin room add reply = %q", reply.Text)
	}

	reply = executeAdmin(t, handler, transport.Message{
		Msg: "관리 관리자 방 추가 room-extra",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "이미 등록") {
		t.Fatalf("admin room duplicate reply = %q", reply.Text)
	}

	reply = executeAdmin(t, handler, transport.Message{
		Msg: "관리 관리자 방 제거 room-missing",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "찾을 수 없습니다") {
		t.Fatalf("admin room missing remove reply = %q", reply.Text)
	}

	reply = executeAdmin(t, handler, transport.Message{
		Msg: "관리 관리자 방 제거 room-extra",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "제거했습니다") {
		t.Fatalf("admin room remove reply = %q", reply.Text)
	}

	reply = executeAdmin(t, handler, transport.Message{
		Msg: "관리 관리자 사용자 목록",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "관리자 사용자") {
		t.Fatalf("admin user list reply = %q", reply.Text)
	}

	reply = executeAdmin(t, handler, transport.Message{
		Msg: "관리 관리자 사용자 추가",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "사용법: 관리 관리자 사용자 추가") {
		t.Fatalf("admin user usage reply = %q", reply.Text)
	}

	reply = executeAdmin(t, handler, transport.Message{
		Msg: "관리 관리자 사용자 추가 user-extra",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "추가했습니다") {
		t.Fatalf("admin user add reply = %q", reply.Text)
	}

	reply = executeAdmin(t, handler, transport.Message{
		Msg: "관리 관리자 사용자 제거 user-extra",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "제거했습니다") {
		t.Fatalf("admin user remove reply = %q", reply.Text)
	}

	reply = executeAdmin(t, handler, transport.Message{
		Msg: "관리 관리자 방 알수없음",
		Raw: transport.RawChatLog{
			ChatID: "admin-room",
			UserID: "bootstrap-user",
		},
	})
	if !strings.Contains(reply.Text, "관리자 방 목록") {
		t.Fatalf("admin room short usage reply = %q", reply.Text)
	}

	snapshot := manager.Snapshot()
	got := handler.adminListText(snapshot)
	if !strings.Contains(got, "관리자 목록") {
		t.Fatalf("adminListText = %q", got)
	}
	if !strings.HasPrefix(got, formatter.Prefix("🛡️", "관리자 목록")) {
		t.Fatalf("adminListText prefix mismatch = %q", got)
	}
	if !contains([]string{"a", "b"}, " b ") {
		t.Fatal("contains should trim target")
	}
	if contains([]string{"a", "b"}, "c") {
		t.Fatal("contains should return false for missing target")
	}

	// Directly exercise low-level helpers to avoid accidental regressions.
	if got := canonicalIntentID(" 코인 "); got != "coin" {
		t.Fatalf("canonicalIntentID = %q", got)
	}
	if room, ok := findRoom(bot.AccessSnapshot{
		Rooms: []config.AccessRoomConfig{{ChatID: "room-1", Alias: "기본", AllowIntents: []string{"coin"}}},
	}, "room-1"); !ok || room.ChatID != "room-1" {
		t.Fatalf("findRoom = (%+v,%v)", room, ok)
	}
}
