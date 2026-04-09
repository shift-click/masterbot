package metrics

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// BuildStoredEvent converts a logical metrics event into its persisted form.
// It applies the same pseudonymization and aliasing rules as the async recorder.
func BuildStoredEvent(event Event, secret string, roomAliases map[string]string) StoredEvent {
	return buildStoredEvent(event, []byte(secret), roomAliases)
}

func buildStoredEvent(event Event, secret []byte, roomAliases map[string]string) StoredEvent {
	metadataJSON := "{}"
	if len(event.Metadata) > 0 {
		if encoded, err := json.Marshal(event.Metadata); err == nil {
			metadataJSON = string(encoded)
		}
	}
	rawScopeRoomID := strings.TrimSpace(event.RawScopeRoomID)
	if rawScopeRoomID == "" {
		rawScopeRoomID = strings.TrimSpace(event.RawRoomID)
	}
	roomLabel := strings.TrimSpace(roomAliases[event.RawRoomID])
	if roomLabel == "" {
		roomLabel = strings.TrimSpace(event.RoomName)
	}
	return StoredEvent{
		OccurredAt:       event.OccurredAt.UTC(),
		RequestID:        strings.TrimSpace(event.RequestID),
		EventName:        string(event.EventName),
		RoomIDHash:       hashIdentifier(secret, event.RawRoomID),
		TenantIDHash:     hashIdentifier(secret, event.RawTenantID),
		RoomScopeHash:    hashIdentifier(secret, rawScopeRoomID),
		RoomLabel:        roomLabel,
		RoomNameSnapshot: strings.TrimSpace(event.RoomName),
		UserIDHash:       hashIdentifier(secret, event.RawUserID),
		CommandID:        strings.TrimSpace(event.CommandID),
		CommandSource:    string(event.CommandSource),
		Audience:         strings.TrimSpace(event.Audience),
		FeatureKey:       strings.TrimSpace(event.FeatureKey),
		Attribution:      strings.TrimSpace(event.Attribution),
		Success:          event.Success,
		ErrorClass:       strings.TrimSpace(event.ErrorClass),
		LatencyMS:        event.Latency.Milliseconds(),
		Denied:           event.Denied,
		RateLimited:      event.RateLimited,
		ReplyType:        strings.TrimSpace(event.ReplyType),
		MetadataJSON:     metadataJSON,
	}
}

func hashIdentifier(secret []byte, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

// RoomShortHash returns a short pseudonymized prefix derived from the same
// HMAC pipeline used for room_id_hash. It is intended for display-side fallback
// labels (e.g., distinguishing rooms whose snapshot name is missing) and never
// exposes the raw chat id. Returns an empty string when the chat id is empty
// or length is non-positive. The prefix is deterministic for a given secret
// and chat id.
func RoomShortHash(secret, chatID string, length int) string {
	if length <= 0 {
		return ""
	}
	full := hashIdentifier([]byte(secret), chatID)
	if full == "" {
		return ""
	}
	if length > len(full) {
		length = len(full)
	}
	return full[:length]
}
