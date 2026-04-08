package admin

import "context"

type CommandSmokeProbe struct {
	ID          string   `json:"id"`
	Message     string   `json:"message"`
	ExpectTexts []string `json:"expect_texts,omitempty"`
	ExpectType  string   `json:"expect_type,omitempty"`
}

type CommandSmokeResult struct {
	ID         string   `json:"id"`
	Message    string   `json:"message"`
	OK         bool     `json:"ok"`
	ReplyCount int      `json:"reply_count"`
	Replies    []string `json:"replies,omitempty"`
	Error      string   `json:"error,omitempty"`
}

type CommandSmokeRunner interface {
	Run(context.Context, []CommandSmokeProbe) ([]CommandSmokeResult, error)
}
