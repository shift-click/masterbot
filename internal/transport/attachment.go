package transport

import (
	"encoding/json"
	"strings"
)

type AttachmentKind string

const (
	AttachmentKindUnknown     AttachmentKind = "unknown"
	AttachmentKindMedia       AttachmentKind = "media"
	AttachmentKindLinkPreview AttachmentKind = "link_preview"
)

type AttachmentInfo struct {
	Kind        AttachmentKind
	URL         string
	Title       string
	Description string
	URLSource   string
}

// ParseAttachmentInfo classifies a KakaoTalk attachment and extracts link
// preview metadata only for known link preview shapes.
func ParseAttachmentInfo(attachment string) AttachmentInfo {
	attachment = strings.TrimSpace(attachment)
	if attachment == "" || attachment == "{}" {
		return AttachmentInfo{Kind: AttachmentKindUnknown}
	}

	var root map[string]any
	if err := json.Unmarshal([]byte(attachment), &root); err != nil {
		return AttachmentInfo{Kind: AttachmentKindUnknown}
	}

	if isMediaAttachment(root) {
		return AttachmentInfo{Kind: AttachmentKindMedia}
	}

	if info, ok := extractModernLinkPreview(root); ok {
		info.Kind = AttachmentKindLinkPreview
		return finalizeAttachmentInfo(info)
	}

	if info, ok := extractLegacyLinkPreview(root); ok {
		info.Kind = AttachmentKindLinkPreview
		return finalizeAttachmentInfo(info)
	}

	return AttachmentInfo{Kind: AttachmentKindUnknown}
}

// ParseLinkPreviewTitle extracts a link preview title from a KakaoTalk
// attachment JSON string.
func ParseLinkPreviewTitle(attachment string) string {
	info := ParseAttachmentInfo(attachment)
	if info.Kind != AttachmentKindLinkPreview {
		return ""
	}
	return info.Title
}

// ParseLinkPreviewURL extracts a link preview URL from a KakaoTalk attachment
// JSON string.
func ParseLinkPreviewURL(attachment string) string {
	info := ParseAttachmentInfo(attachment)
	if info.Kind != AttachmentKindLinkPreview {
		return ""
	}
	return info.URL
}

func finalizeAttachmentInfo(info AttachmentInfo) AttachmentInfo {
	info.URL = strings.TrimSpace(info.URL)
	info.Title = strings.TrimSpace(info.Title)
	info.Description = strings.TrimSpace(info.Description)
	if info.Title == "" {
		info.Title = info.Description
	}
	if info.Kind == AttachmentKindLinkPreview && info.URL == "" && info.Title == "" && info.Description == "" {
		return AttachmentInfo{Kind: AttachmentKindUnknown}
	}
	return info
}

func isMediaAttachment(root map[string]any) bool {
	if mt := getString(root, "mt"); strings.HasPrefix(strings.ToLower(mt), "image/") || strings.HasPrefix(strings.ToLower(mt), "video/") {
		return true
	}
	switch strings.ToLower(getString(root, "type")) {
	case "photo", "video":
		return true
	}
	if getString(root, "thumbnailUrl") != "" || getString(root, "thumbnail") != "" {
		return true
	}
	return false
}

func extractModernLinkPreview(root map[string]any) (AttachmentInfo, bool) {
	universal := parseUniversalScrapData(root["universalScrapData"])
	urls := parseURLsField(root["urls"])
	if universal == nil && len(urls) == 0 {
		return AttachmentInfo{}, false
	}

	info := AttachmentInfo{}
	if universal != nil {
		if canonical := getString(universal, "canonical_url"); canonical != "" {
			info.URL = canonical
			info.URLSource = "universal_canonical"
		} else if requested := getString(universal, "requested_url"); requested != "" {
			info.URL = requested
			info.URLSource = "universal_requested"
		}
		info.Title = getString(universal, "title")
		info.Description = getString(universal, "description")
	}
	if info.URL == "" && len(urls) > 0 {
		info.URL = urls[0].URL
		info.URLSource = "urls"
	}
	if info.Title == "" && len(urls) > 0 {
		info.Title = urls[0].Title
	}
	if info.Description == "" && len(urls) > 0 {
		info.Description = urls[0].Description
	}
	return info, info.URL != "" || info.Title != "" || info.Description != ""
}

func extractLegacyLinkPreview(root map[string]any) (AttachmentInfo, bool) {
	if info := extractFlatLinkPreview(root); info.URL != "" || info.Title != "" || info.Description != "" {
		return info, true
	}
	for _, key := range []string{"url", "shout", "SL"} {
		info, ok := extractNamedNestedLinkPreview(root, key)
		if ok {
			return info, true
		}
	}
	for _, entry := range parseURLsField(root["urls"]) {
		if entry.URL != "" || entry.Title != "" || entry.Description != "" {
			return AttachmentInfo{
				URL:         entry.URL,
				Title:       entry.Title,
				Description: entry.Description,
				URLSource:   "urls",
			}, true
		}
	}
	return AttachmentInfo{}, false
}

func extractFlatLinkPreview(root map[string]any) AttachmentInfo {
	url := getString(root, "url")
	if _, ok := root["url"].(map[string]any); ok {
		url = ""
	}
	return AttachmentInfo{
		URL:         url,
		Title:       getString(root, "title"),
		Description: getString(root, "description"),
		URLSource:   "flat",
	}
}

func extractNamedNestedLinkPreview(root map[string]any, key string) (AttachmentInfo, bool) {
	nested, ok := root[key].(map[string]any)
	if !ok {
		return AttachmentInfo{}, false
	}
	info := AttachmentInfo{
		URL:         getString(nested, "url"),
		Title:       getString(nested, "title"),
		Description: getString(nested, "description"),
		URLSource:   key,
	}
	return info, info.URL != "" || info.Title != "" || info.Description != ""
}

type attachmentURLEntry struct {
	URL         string
	Title       string
	Description string
	URLSource   string
}

func parseURLsField(value any) []attachmentURLEntry {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	entries := make([]attachmentURLEntry, 0, len(items))
	for _, item := range items {
		switch typed := item.(type) {
		case string:
			url := strings.TrimSpace(typed)
			if url != "" {
				entries = append(entries, attachmentURLEntry{URL: url, URLSource: "urls"})
			}
		case map[string]any:
			entry := attachmentURLEntry{
				URL:         getString(typed, "url"),
				Title:       getString(typed, "title"),
				Description: getString(typed, "description"),
				URLSource:   "urls",
			}
			if entry.URL != "" || entry.Title != "" || entry.Description != "" {
				entries = append(entries, entry)
			}
		}
	}
	return entries
}

func parseUniversalScrapData(value any) map[string]any {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(typed), &data); err != nil {
			return nil
		}
		return data
	case map[string]any:
		return typed
	default:
		return nil
	}
}

func getString(root map[string]any, key string) string {
	value, ok := root[key]
	if !ok {
		return ""
	}
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}
