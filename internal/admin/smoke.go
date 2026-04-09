package admin

import "context"

type CommandSmokeProbe struct {
	ID          string   `json:"id"`
	Message     string   `json:"message"`
	ExpectTexts []string `json:"expect_texts,omitempty"`
	ExpectType  string   `json:"expect_type,omitempty"`
	// AcceptACLDenied makes the probe treat the canonical router ACL deny
	// reply as a successful baseline outcome. It is intended for synthetic
	// chat id mode where the smoke runner is intentionally not on the ACL
	// allowlist; the probe still validates that routing and ACL evaluation
	// are alive end-to-end.
	AcceptACLDenied bool `json:"accept_acl_denied,omitempty"`
}

type CommandSmokeResult struct {
	ID         string   `json:"id"`
	Message    string   `json:"message"`
	OK         bool     `json:"ok"`
	ReplyCount int      `json:"reply_count"`
	Replies    []string `json:"replies,omitempty"`
	Error      string   `json:"error,omitempty"`
	// ACLDenied indicates the probe was satisfied via the ACL deny path
	// rather than a normal success match. Operators reading the result page
	// can distinguish the two outcomes without inspecting reply text.
	ACLDenied bool `json:"acl_denied,omitempty"`
}

type CommandSmokeRunner interface {
	Run(context.Context, []CommandSmokeProbe) ([]CommandSmokeResult, error)
}
