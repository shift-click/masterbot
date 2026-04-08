package transport

import "context"

// RuntimeAdapter bridges an external messaging transport into the bot runtime.
type RuntimeAdapter interface {
	Start(context.Context, func(context.Context, Message) error) error
	Reply(context.Context, ReplyRequest) error
	Close() error
}
