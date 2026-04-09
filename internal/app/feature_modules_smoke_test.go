package app

import (
	"strings"
	"testing"

	"github.com/shift-click/masterbot/internal/admin"
	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/transport"
)

func TestResolveSmokeRoomChatIDPrefersExplicitConfig(t *testing.T) {
	t.Parallel()

	cfg := config.Config{}
	cfg.Admin.SmokeRoomChatID = "explicit-smoke-room"
	cfg.Access.BootstrapAdminRoomChatID = "bootstrap-room"
	cfg.Access.Rooms = []config.AccessRoomConfig{{ChatID: "operational-room"}}

	if got := resolveSmokeRoomChatID(cfg); got != "explicit-smoke-room" {
		t.Fatalf("explicit smoke chat id ignored: got %q", got)
	}
}

func TestResolveSmokeRoomChatIDTrimsExplicitConfig(t *testing.T) {
	t.Parallel()

	cfg := config.Config{}
	cfg.Admin.SmokeRoomChatID = "  padded-room  "

	if got := resolveSmokeRoomChatID(cfg); got != "padded-room" {
		t.Fatalf("explicit smoke chat id should be trimmed: got %q", got)
	}
}

func TestResolveSmokeRoomChatIDFallsBackToSyntheticWhenUnset(t *testing.T) {
	t.Parallel()

	cfg := config.Config{}

	got := resolveSmokeRoomChatID(cfg)
	if got != adminSmokeSyntheticChatID {
		t.Fatalf("expected synthetic id when unset, got %q", got)
	}
	if !strings.HasPrefix(got, "admin-smoke://") {
		t.Fatalf("synthetic id must use admin-smoke:// scheme, got %q", got)
	}
}

func TestResolveSmokeRoomChatIDDoesNotBorrowOperationalRoom(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		cfg  config.Config
	}{
		{
			name: "with bootstrap room only",
			cfg: func() config.Config {
				c := config.Config{}
				c.Access.BootstrapAdminRoomChatID = "bootstrap-room"
				return c
			}(),
		},
		{
			name: "with access list rooms",
			cfg: func() config.Config {
				c := config.Config{}
				c.Access.Rooms = []config.AccessRoomConfig{{ChatID: "operational-room"}}
				return c
			}(),
		},
		{
			name: "with both",
			cfg: func() config.Config {
				c := config.Config{}
				c.Access.BootstrapAdminRoomChatID = "bootstrap-room"
				c.Access.Rooms = []config.AccessRoomConfig{{ChatID: "operational-room"}}
				return c
			}(),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := resolveSmokeRoomChatID(tc.cfg)
			if got != adminSmokeSyntheticChatID {
				t.Fatalf("smoke runner must not borrow operational rooms; got %q", got)
			}
		})
	}
}

func TestAdminSmokeSyntheticChatIDIsSchemed(t *testing.T) {
	t.Parallel()

	if !strings.Contains(adminSmokeSyntheticChatID, "://") {
		t.Fatalf("synthetic chat id should be URI-shaped, got %q", adminSmokeSyntheticChatID)
	}
}

func TestCommandSmokeMatchesExpectationDenyPath(t *testing.T) {
	t.Parallel()

	denyReply := bot.Reply{Type: transport.ReplyTypeText, Text: bot.DeniedIntentMessage}
	successReply := bot.Reply{Type: transport.ReplyTypeText, Text: "사용 가능한 명령어: 도움"}

	cases := []struct {
		name        string
		probe       admin.CommandSmokeProbe
		replies     []bot.Reply
		wantMatched bool
		wantDenied  bool
	}{
		{
			name: "deny success when AcceptACLDenied true",
			probe: admin.CommandSmokeProbe{
				ID:              "help",
				ExpectTexts:     []string{"사용 가능한 명령어"},
				ExpectType:      string(transport.ReplyTypeText),
				AcceptACLDenied: true,
			},
			replies:     []bot.Reply{denyReply},
			wantMatched: true,
			wantDenied:  true,
		},
		{
			name: "deny mismatch when AcceptACLDenied false",
			probe: admin.CommandSmokeProbe{
				ID:          "help",
				ExpectTexts: []string{"사용 가능한 명령어"},
				ExpectType:  string(transport.ReplyTypeText),
			},
			replies:     []bot.Reply{denyReply},
			wantMatched: false,
			wantDenied:  false,
		},
		{
			name: "normal success keeps deny flag false",
			probe: admin.CommandSmokeProbe{
				ID:              "help",
				ExpectTexts:     []string{"사용 가능한 명령어"},
				ExpectType:      string(transport.ReplyTypeText),
				AcceptACLDenied: true,
			},
			replies:     []bot.Reply{successReply},
			wantMatched: true,
			wantDenied:  false,
		},
		{
			name: "type mismatch fails",
			probe: admin.CommandSmokeProbe{
				ID:              "help",
				ExpectType:      string(transport.ReplyTypeImage),
				AcceptACLDenied: true,
			},
			replies:     []bot.Reply{successReply},
			wantMatched: false,
			wantDenied:  false,
		},
		{
			name: "no replies fails",
			probe: admin.CommandSmokeProbe{
				ID:              "help",
				AcceptACLDenied: true,
			},
			replies:     nil,
			wantMatched: false,
			wantDenied:  false,
		},
		{
			name: "deny detection rejects multi-reply",
			probe: admin.CommandSmokeProbe{
				ID:              "help",
				ExpectTexts:     []string{"사용 가능한 명령어"},
				ExpectType:      string(transport.ReplyTypeText),
				AcceptACLDenied: true,
			},
			replies:     []bot.Reply{denyReply, successReply},
			wantMatched: true,
			wantDenied:  false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			matched, denied := commandSmokeMatchesExpectation(tc.replies, tc.probe)
			if matched != tc.wantMatched {
				t.Fatalf("matched = %v, want %v", matched, tc.wantMatched)
			}
			if denied != tc.wantDenied {
				t.Fatalf("aclDenied = %v, want %v", denied, tc.wantDenied)
			}
		})
	}
}

func TestCommandSmokeMatchesExpectationDenyTrimsWhitespace(t *testing.T) {
	t.Parallel()

	probe := admin.CommandSmokeProbe{ID: "help", AcceptACLDenied: true}
	replies := []bot.Reply{{Type: transport.ReplyTypeText, Text: "  " + bot.DeniedIntentMessage + "\n"}}

	matched, denied := commandSmokeMatchesExpectation(replies, probe)
	if !matched || !denied {
		t.Fatalf("expected deny path success even with surrounding whitespace; matched=%v denied=%v", matched, denied)
	}
}

func TestDeniedIntentMessageIsExported(t *testing.T) {
	t.Parallel()

	if bot.DeniedIntentMessage == "" {
		t.Fatal("bot.DeniedIntentMessage must be a non-empty exported constant")
	}
}
