package command

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/commandmeta"
	"github.com/shift-click/masterbot/internal/store"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/pkg/formatter"
)

type AdminHandler struct {
	descriptorSupport
	access       *bot.AccessController
	manager      *bot.AccessManager
	autoQueries  *bot.AutoQueryManager
	knownIntents func() []string
	logger       *slog.Logger
}

const adminRoomNotRegisteredWithGuide = "등록되어 있지 않은 방입니다.\n\n💡 \"관리 등록\" 으로 먼저 등록하세요"
const (
	adminPolicyUsage         = "사용법: 관리 정책 보기 <chat_id> | 관리 정책 설정 <chat_id> <explicit-only|local-auto>"
	adminPolicyViewUsage     = "사용법: 관리 정책 보기 <chat_id>"
	adminPolicySetUsage      = "사용법: 관리 정책 설정 <chat_id> <explicit-only|local-auto>"
	adminPrincipalUsage      = "사용법: 관리 관리자 목록 | 관리 관리자 방 목록 | 관리 관리자 방 추가 <chat_id> | 관리 관리자 방 제거 <chat_id> | 관리 관리자 사용자 목록 | 관리 관리자 사용자 추가 <user_id> | 관리 관리자 사용자 제거 <user_id>"
	adminPrincipalRoomUsage  = "사용법: 관리 관리자 방 목록 | 관리 관리자 방 추가 <chat_id> | 관리 관리자 방 제거 <chat_id>"
	adminPrincipalUserUsage  = "사용법: 관리 관리자 사용자 목록 | 관리 관리자 사용자 추가 <user_id> | 관리 관리자 사용자 제거 <user_id>"
	adminPrincipalShortUsage = "사용법: 관리 관리자 방 목록 | 관리 관리자 방 추가 <chat_id> | 관리 관리자 방 제거 <chat_id> | 관리 관리자 사용자 목록 | 관리 관리자 사용자 추가 <user_id> | 관리 관리자 사용자 제거 <user_id>"
)

func NewAdminHandler(
	access *bot.AccessController,
	manager *bot.AccessManager,
	autoQueries *bot.AutoQueryManager,
	knownIntents func() []string,
	logger *slog.Logger,
) *AdminHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &AdminHandler{
		descriptorSupport: newDescriptorSupport("admin"),
		access:            access,
		manager:           manager,
		autoQueries:       autoQueries,
		knownIntents:      knownIntents,
		logger:            logger.With("component", "admin_handler"),
	}
}

func (h *AdminHandler) Execute(ctx context.Context, cmd bot.CommandContext) error {
	if handled, err := h.validateCommandAccess(ctx, cmd); handled {
		return err
	}

	fields := cmd.Args
	if len(fields) == 0 {
		return h.replyText(ctx, cmd, adminUsageText())
	}

	chatID := strings.TrimSpace(cmd.Message.Raw.ChatID)
	if handled, err := h.executePrimaryCommand(ctx, cmd, chatID, fields); handled {
		return err
	}
	return h.executeAliasCommand(ctx, cmd, fields)
}

func (h *AdminHandler) validateCommandAccess(ctx context.Context, cmd bot.CommandContext) (bool, error) {
	if h.access == nil || h.manager == nil || !h.access.RuntimeEnabled() {
		return true, h.replyText(ctx, cmd, "채팅 기반 ACL 관리가 비활성화되어 있습니다.")
	}
	if !h.access.IsAuthorizedAdmin(cmd.Message) {
		return true, h.replyText(ctx, cmd, "관리자 권한이 없습니다.")
	}
	return false, nil
}

func (h *AdminHandler) executePrimaryCommand(ctx context.Context, cmd bot.CommandContext, chatID string, fields []string) (bool, error) {
	switch fields[0] {
	case "등록":
		return true, h.handleRegister(ctx, cmd, chatID, fields[1:])
	case "해제":
		return true, h.handleUnregister(ctx, cmd, chatID)
	case "상태":
		return true, h.handleStatus(ctx, cmd, chatID)
	case "현황":
		return true, h.handleOverview(ctx, cmd)
	case "전체":
		if len(fields) < 2 {
			return true, h.replyText(ctx, cmd, adminUsageText())
		}
		return true, h.handleToggleAll(ctx, cmd, chatID, fields[1])
	case "정책":
		return true, h.handlePolicy(ctx, cmd, fields[1:])
	case "관리자":
		return true, h.handleAdminPrincipal(ctx, cmd, fields[1:])
	}

	intentID, ok := commandmeta.NormalizeIntentID(fields[0])
	if !ok {
		return false, nil
	}
	if len(fields) < 2 {
		return true, h.replyText(ctx, cmd, adminUsageText())
	}
	return true, h.handleToggleIntent(ctx, cmd, chatID, intentID, fields[1])
}

func (h *AdminHandler) executeAliasCommand(ctx context.Context, cmd bot.CommandContext, fields []string) error {
	room, ok := h.manager.FindRoomByAlias(fields[0])
	if !ok {
		return h.replyText(ctx, cmd, fmt.Sprintf("알 수 없는 명령 또는 기능입니다: %s\n\n%s", fields[0], adminUsageText()))
	}
	if len(fields) < 2 {
		return h.replyText(ctx, cmd, adminUsageText())
	}
	return h.executeAliasAction(ctx, cmd, room.ChatID, fields[1:])
}

func (h *AdminHandler) executeAliasAction(ctx context.Context, cmd bot.CommandContext, remoteChatID string, args []string) error {
	switch args[0] {
	case "해제":
		return h.handleUnregister(ctx, cmd, remoteChatID)
	case "상태":
		return h.handleStatus(ctx, cmd, remoteChatID)
	case "전체":
		if len(args) < 2 {
			return h.replyText(ctx, cmd, adminUsageText())
		}
		return h.handleToggleAll(ctx, cmd, remoteChatID, args[1])
	}

	intentID, ok := commandmeta.NormalizeIntentID(args[0])
	if !ok {
		return h.replyText(ctx, cmd, fmt.Sprintf("알 수 없는 기능입니다: %s\n\n%s", args[0], adminUsageText()))
	}
	if len(args) < 2 {
		return h.replyText(ctx, cmd, adminUsageText())
	}
	return h.handleToggleIntent(ctx, cmd, remoteChatID, intentID, args[1])
}

// ── Room registration ────────────────────────────────────────────────

func (h *AdminHandler) handleRegister(ctx context.Context, cmd bot.CommandContext, chatID string, args []string) error {
	alias := ""
	if len(args) > 0 {
		alias = strings.TrimSpace(strings.Join(args, " "))
	}
	if room, ok := findRoom(h.manager.Snapshot(), chatID); ok && room.Alias == alias {
		label := roomLabel(alias)
		return h.replyText(ctx, cmd, fmt.Sprintf("%s은(는) 이미 등록되어 있습니다.", label))
	}
	if _, err := h.manager.UpsertRoom(ctx, actorFrom(cmd), store.ACLRoom{
		ChatID: chatID,
		Alias:  alias,
	}); err != nil {
		return err
	}
	label := roomLabel(alias)
	return h.replyText(ctx, cmd, fmt.Sprintf("✅ %s을(를) 등록했습니다.\n\n💡 \"관리 코인 켜기\" 로 기능을 추가하세요", label))
}

func (h *AdminHandler) handleUnregister(ctx context.Context, cmd bot.CommandContext, chatID string) error {
	room, ok := findRoom(h.manager.Snapshot(), chatID)
	if !ok {
		return h.replyText(ctx, cmd, "등록되어 있지 않은 방입니다.")
	}
	if _, err := h.manager.DeleteRoom(ctx, actorFrom(cmd), chatID); err != nil {
		return err
	}
	label := roomLabel(room.Alias)
	return h.replyText(ctx, cmd, fmt.Sprintf("🗑️ %s을(를) 해제했습니다.", label))
}

// ── Intent toggle ────────────────────────────────────────────────────

func (h *AdminHandler) handleToggleIntent(ctx context.Context, cmd bot.CommandContext, chatID, intentID, action string) error {
	room, ok := findRoom(h.manager.Snapshot(), chatID)
	if !ok {
		return h.replyText(ctx, cmd, adminRoomNotRegisteredWithGuide)
	}

	if !containsIntent(h.knownIntents(), intentID) {
		return h.replyText(ctx, cmd, fmt.Sprintf("알 수 없는 기능입니다: %s", commandmeta.DisplayName(intentID)))
	}

	displayName := commandmeta.DisplayName(intentID)

	switch action {
	case "켜기":
		if containsIntent(room.AllowIntents, intentID) {
			return h.replyText(ctx, cmd, fmt.Sprintf("%s 기능이 이미 켜져 있습니다.", displayName))
		}
		if _, err := h.manager.AddRoomIntent(ctx, actorFrom(cmd), chatID, intentID); err != nil {
			return err
		}
		return h.replyText(ctx, cmd, fmt.Sprintf("✅ %s 기능을 켰습니다.\n\n💡 \"관리 상태\" 로 현재 설정을 확인하세요", displayName))
	case "끄기":
		if !containsIntent(room.AllowIntents, intentID) {
			return h.replyText(ctx, cmd, fmt.Sprintf("%s 기능이 이미 꺼져 있습니다.", displayName))
		}
		if _, err := h.manager.RemoveRoomIntent(ctx, actorFrom(cmd), chatID, intentID); err != nil {
			return err
		}
		return h.replyText(ctx, cmd, fmt.Sprintf("❌ %s 기능을 껐습니다.\n\n💡 \"관리 상태\" 로 현재 설정을 확인하세요", displayName))
	default:
		return h.replyText(ctx, cmd, adminUsageText())
	}
}

func (h *AdminHandler) handleToggleAll(ctx context.Context, cmd bot.CommandContext, chatID, action string) error {
	room, ok := findRoom(h.manager.Snapshot(), chatID)
	if !ok {
		return h.replyText(ctx, cmd, adminRoomNotRegisteredWithGuide)
	}

	actor := actorFrom(cmd)

	switch action {
	case "켜기":
		for _, id := range commandmeta.ToggleableIntentIDs() {
			if _, err := h.manager.AddRoomIntent(ctx, actor, chatID, id); err != nil {
				return err
			}
		}
		return h.replyText(ctx, cmd, "✅ 모든 기능을 켰습니다.\n\n💡 \"관리 상태\" 로 현재 설정을 확인하세요")
	case "끄기":
		for _, id := range room.AllowIntents {
			if _, err := h.manager.RemoveRoomIntent(ctx, actor, chatID, id); err != nil {
				return err
			}
		}
		return h.replyText(ctx, cmd, "❌ 모든 기능을 껐습니다.\n\n💡 \"관리 상태\" 로 현재 설정을 확인하세요")
	default:
		return h.replyText(ctx, cmd, adminUsageText())
	}
}

// ── Status / overview ────────────────────────────────────────────────

func (h *AdminHandler) handleStatus(ctx context.Context, cmd bot.CommandContext, chatID string) error {
	room, ok := findRoom(h.manager.Snapshot(), chatID)
	if !ok {
		return h.replyText(ctx, cmd, adminRoomNotRegisteredWithGuide)
	}

	allIntents := commandmeta.ToggleableIntentIDs()
	var onNames, offNames []string
	for _, id := range allIntents {
		name := commandmeta.DisplayName(id)
		if containsIntent(room.AllowIntents, id) {
			onNames = append(onNames, name)
		} else {
			offNames = append(offNames, name)
		}
	}

	lines := []string{fmt.Sprintf("🏠 %s", roomLabel(room.Alias)), ""}
	if len(onNames) > 0 {
		lines = append(lines, "✅ "+strings.Join(onNames, " · "))
	}
	if len(offNames) > 0 {
		lines = append(lines, "❌ "+strings.Join(offNames, " · "))
	}
	if len(offNames) > 0 {
		lines = append(lines, "", fmt.Sprintf("💡 \"관리 %s 켜기\" 로 기능 추가", offNames[0]))
	}

	return h.replyText(ctx, cmd, strings.Join(lines, "\n"))
}

func (h *AdminHandler) handleOverview(ctx context.Context, cmd bot.CommandContext) error {
	snapshot := h.manager.Snapshot()
	if len(snapshot.Rooms) == 0 {
		return h.replyText(ctx, cmd, "등록된 방이 없습니다.\n\n💡 \"관리 등록\" 으로 방을 등록하세요")
	}

	lines := []string{fmt.Sprintf("📋 등록된 방 (%d개)", len(snapshot.Rooms))}
	for _, room := range snapshot.Rooms {
		lines = append(lines, "", fmt.Sprintf("🏠 %s", roomLabel(room.Alias)))
		if len(room.AllowIntents) > 0 {
			names := make([]string, 0, len(room.AllowIntents))
			for _, id := range room.AllowIntents {
				names = append(names, commandmeta.DisplayName(id))
			}
			lines = append(lines, "   ✅ "+strings.Join(names, " · "))
		} else {
			lines = append(lines, "   (기능 없음)")
		}
	}

	return h.replyText(ctx, cmd, strings.Join(lines, "\n"))
}

// ── Policy (unchanged) ──────────────────────────────────────────────

func (h *AdminHandler) handlePolicy(ctx context.Context, cmd bot.CommandContext, args []string) error {
	if h.autoQueries == nil {
		return h.replyText(ctx, cmd, "auto-query 정책 관리가 비활성화되어 있습니다.")
	}
	if len(args) == 0 {
		return h.replyText(ctx, cmd, adminPolicyUsage)
	}

	switch args[0] {
	case "보기":
		return h.handlePolicyView(ctx, cmd, args[1:])
	case "설정":
		return h.handlePolicySet(ctx, cmd, args[1:])
	default:
		return h.replyText(ctx, cmd, adminPolicyUsage)
	}
}

// ── Admin principal (unchanged) ─────────────────────────────────────

func (h *AdminHandler) handleAdminPrincipal(ctx context.Context, cmd bot.CommandContext, args []string) error {
	if len(args) == 0 {
		return h.replyText(ctx, cmd, adminPrincipalUsage)
	}

	if args[0] == "목록" {
		return h.replyText(ctx, cmd, h.adminListText(h.manager.Snapshot()))
	}

	if len(args) < 2 {
		return h.replyText(ctx, cmd, adminPrincipalShortUsage)
	}
	if !h.access.IsBootstrapSuperAdmin(cmd.Message) && args[1] != "목록" {
		return h.replyText(ctx, cmd, "bootstrap super admin만 관리자 principal을 변경할 수 있습니다.")
	}

	snapshot := h.manager.Snapshot()
	switch args[0] {
	case "방":
		return h.handleAdminRoomPrincipal(ctx, cmd, snapshot.AdminRooms, args[1:])
	case "사용자":
		return h.handleAdminUserPrincipal(ctx, cmd, snapshot.AdminUsers, args[1:])
	default:
		return h.replyText(ctx, cmd, adminPrincipalUsage)
	}
}

func (h *AdminHandler) handlePolicyView(ctx context.Context, cmd bot.CommandContext, args []string) error {
	if len(args) < 1 {
		return h.replyText(ctx, cmd, adminPolicyViewUsage)
	}
	chatID := strings.TrimSpace(args[0])
	policy, stored, err := h.autoQueries.PolicyForRoom(ctx, chatID)
	if err != nil {
		return err
	}
	source := "default"
	if stored {
		source = "runtime"
	}
	text := fmt.Sprintf(
		"chat_id: %s\nsource: %s\nmode: %s\nallowed_handlers: %s\nbudget_per_hour: %d\ncooldown_window: %s\ndegradation_target: %s",
		chatID,
		source,
		policy.Mode,
		strings.Join(policy.AllowedHandlers, ", "),
		policy.BudgetPerHour,
		policy.CooldownWindow,
		policy.DegradationTarget,
	)
	return h.replyText(ctx, cmd, formatter.Prefix("🧭", text))
}

func (h *AdminHandler) handlePolicySet(ctx context.Context, cmd bot.CommandContext, args []string) error {
	if len(args) < 2 {
		return h.replyText(ctx, cmd, adminPolicySetUsage)
	}
	chatID := strings.TrimSpace(args[0])
	mode, ok := parseAutoQueryMode(args[1])
	if !ok {
		return h.replyText(ctx, cmd, fmt.Sprintf("유효한 auto-query 모드가 아닙니다: %s", strings.TrimSpace(args[1])))
	}
	current, _, err := h.autoQueries.PolicyForRoom(ctx, chatID)
	if err != nil {
		return err
	}
	current.Mode = mode
	changed, err := h.autoQueries.UpdateRoomPolicy(ctx, autoQueryActorFrom(cmd), chatID, current)
	if err != nil {
		return err
	}
	if !changed {
		return h.replyText(ctx, cmd, fmt.Sprintf("방 %s 의 auto-query 정책은 이미 %s 입니다.", chatID, mode))
	}
	return h.replyText(ctx, cmd, fmt.Sprintf("방 %s 의 auto-query 정책을 %s 로 설정했습니다.", chatID, mode))
}

func (h *AdminHandler) handleAdminRoomPrincipal(ctx context.Context, cmd bot.CommandContext, adminRooms []string, args []string) error {
	switch args[0] {
	case "목록":
		if len(adminRooms) == 0 {
			return h.replyText(ctx, cmd, "관리자 방이 없습니다.")
		}
		return h.replyText(ctx, cmd, formatter.Prefix("🛡️", "관리자 방\n"+strings.Join(adminRooms, "\n")))
	case "추가":
		if len(args) < 2 {
			return h.replyText(ctx, cmd, "사용법: 관리 관리자 방 추가 <chat_id>")
		}
		chatID := strings.TrimSpace(args[1])
		if contains(adminRooms, chatID) {
			return h.replyText(ctx, cmd, fmt.Sprintf("관리자 방 %s 가 이미 등록되어 있습니다.", chatID))
		}
		_, err := h.manager.AddAdminRoom(ctx, actorFrom(cmd), chatID)
		if err != nil {
			return err
		}
		return h.replyText(ctx, cmd, fmt.Sprintf("관리자 방 %s 를 추가했습니다.", chatID))
	case "제거":
		if len(args) < 2 {
			return h.replyText(ctx, cmd, "사용법: 관리 관리자 방 제거 <chat_id>")
		}
		chatID := strings.TrimSpace(args[1])
		if !contains(adminRooms, chatID) {
			return h.replyText(ctx, cmd, fmt.Sprintf("관리자 방 %s 를 찾을 수 없습니다.", chatID))
		}
		_, err := h.manager.RemoveAdminRoom(ctx, actorFrom(cmd), chatID)
		if err != nil {
			return err
		}
		return h.replyText(ctx, cmd, fmt.Sprintf("관리자 방 %s 를 제거했습니다.", chatID))
	default:
		return h.replyText(ctx, cmd, adminPrincipalRoomUsage)
	}
}

func (h *AdminHandler) handleAdminUserPrincipal(ctx context.Context, cmd bot.CommandContext, adminUsers []string, args []string) error {
	switch args[0] {
	case "목록":
		if len(adminUsers) == 0 {
			return h.replyText(ctx, cmd, "관리자 사용자가 없습니다.")
		}
		return h.replyText(ctx, cmd, formatter.Prefix("🛡️", "관리자 사용자\n"+strings.Join(adminUsers, "\n")))
	case "추가":
		if len(args) < 2 {
			return h.replyText(ctx, cmd, "사용법: 관리 관리자 사용자 추가 <user_id>")
		}
		userID := strings.TrimSpace(args[1])
		if contains(adminUsers, userID) {
			return h.replyText(ctx, cmd, fmt.Sprintf("관리자 사용자 %s 가 이미 등록되어 있습니다.", userID))
		}
		_, err := h.manager.AddAdminUser(ctx, actorFrom(cmd), userID)
		if err != nil {
			return err
		}
		return h.replyText(ctx, cmd, fmt.Sprintf("관리자 사용자 %s 를 추가했습니다.", userID))
	case "제거":
		if len(args) < 2 {
			return h.replyText(ctx, cmd, "사용법: 관리 관리자 사용자 제거 <user_id>")
		}
		userID := strings.TrimSpace(args[1])
		if !contains(adminUsers, userID) {
			return h.replyText(ctx, cmd, fmt.Sprintf("관리자 사용자 %s 를 찾을 수 없습니다.", userID))
		}
		_, err := h.manager.RemoveAdminUser(ctx, actorFrom(cmd), userID)
		if err != nil {
			return err
		}
		return h.replyText(ctx, cmd, fmt.Sprintf("관리자 사용자 %s 를 제거했습니다.", userID))
	default:
		return h.replyText(ctx, cmd, adminPrincipalUserUsage)
	}
}

func (h *AdminHandler) adminListText(snapshot bot.AccessSnapshot) string {
	lines := []string{"관리자 목록"}
	if len(snapshot.AdminRooms) == 0 {
		lines = append(lines, "방: (없음)")
	} else {
		lines = append(lines, "방: "+strings.Join(snapshot.AdminRooms, ", "))
	}
	if len(snapshot.AdminUsers) == 0 {
		lines = append(lines, "사용자: (없음)")
	} else {
		lines = append(lines, "사용자: "+strings.Join(snapshot.AdminUsers, ", "))
	}
	return formatter.Prefix("🛡️", strings.Join(lines, "\n"))
}

// ── Helpers ─────────────────────────────────────────────────────────

func (h *AdminHandler) replyText(ctx context.Context, cmd bot.CommandContext, text string) error {
	return cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
	})
}

func actorFrom(cmd bot.CommandContext) bot.AccessActor {
	return bot.AccessActor{
		ChatID: strings.TrimSpace(cmd.Message.Raw.ChatID),
		UserID: strings.TrimSpace(cmd.Message.Raw.UserID),
	}
}

func autoQueryActorFrom(cmd bot.CommandContext) bot.AutoQueryActor {
	return bot.AutoQueryActor{
		ChatID: strings.TrimSpace(cmd.Message.Raw.ChatID),
		UserID: strings.TrimSpace(cmd.Message.Raw.UserID),
	}
}

func findRoom(snapshot bot.AccessSnapshot, chatID string) (store.ACLRoom, bool) {
	chatID = strings.TrimSpace(chatID)
	for _, item := range snapshot.Rooms {
		if item.ChatID != chatID {
			continue
		}
		return store.ACLRoom{
			ChatID:       item.ChatID,
			Alias:        item.Alias,
			AllowIntents: append([]string(nil), item.AllowIntents...),
		}, true
	}
	return store.ACLRoom{}, false
}

func containsIntent(intents []string, target string) bool {
	target = canonicalIntentID(target)
	for _, intentID := range intents {
		if canonicalIntentID(intentID) == target {
			return true
		}
	}
	return false
}

func contains(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func canonicalIntentID(intentID string) string {
	if normalized, ok := commandmeta.NormalizeIntentID(intentID); ok {
		return normalized
	}
	return strings.TrimSpace(strings.ToLower(intentID))
}

func roomLabel(alias string) string {
	if strings.TrimSpace(alias) == "" {
		return "이 방"
	}
	return alias
}

func adminUsageText() string {
	return strings.Join([]string{
		"📋 관리 명령",
		"",
		"방 관리:",
		"  관리 등록 [별칭]            이 방을 등록",
		"  관리 해제                   이 방을 해제",
		"  관리 <별칭> 해제            다른 방을 해제",
		"",
		"기능 관리:",
		"  관리 <기능> 켜기            이 방에 기능 추가",
		"  관리 <기능> 끄기            이 방에서 기능 제거",
		"  관리 <별칭> <기능> 켜기     다른 방에 기능 추가",
		"  관리 <별칭> <기능> 끄기     다른 방에서 기능 제거",
		"  관리 전체 켜기              모든 기능 추가",
		"  관리 전체 끄기              모든 기능 제거",
		"",
		"조회:",
		"  관리 상태                   이 방의 기능 현황",
		"  관리 <별칭> 상태            다른 방의 기능 현황",
		"  관리 현황                   전체 방 목록",
		"",
		"기능 예시: 코인, 주식, 쿠팡, 날씨, 축구, 롤",
	}, "\n")
}

func parseAutoQueryMode(value string) (bot.AutoQueryMode, bool) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case string(bot.AutoQueryModeExplicitOnly):
		return bot.AutoQueryModeExplicitOnly, true
	case string(bot.AutoQueryModeLocalAuto):
		return bot.AutoQueryModeLocalAuto, true
	default:
		return "", false
	}
}
