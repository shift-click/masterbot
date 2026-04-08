package bot

import (
	"context"
	"errors"
	"time"

	"github.com/shift-click/masterbot/internal/commandmeta"
	"github.com/shift-click/masterbot/internal/transport"
)

// ErrHandled is returned by FallbackHandler.HandleFallback to indicate
// that the message was successfully handled. The router uses this to stop
// the fallback chain. It is NOT a real error — the router will swallow it.
var ErrHandled = errors.New("handled")

// ErrHandledWithFailure is returned by handlers when they have already sent
// an error message to the user but want the failure to be recorded in metrics.
// errorMiddleware will NOT send a duplicate generic message for this error.
// loggingMiddleware will record it as EventCommandFailed.
// In the fallback path, the router will stop the chain and record failure.
var ErrHandledWithFailure = errors.New("handled with failure")

type classifiedError interface {
	error
	ErrorClass() string
	ShouldAlert() bool
}

type handledFailure struct {
	class string
	alert bool
	msg   string
	cause error
}

func (e *handledFailure) Error() string {
	if e.msg != "" {
		return e.msg
	}
	if e.cause != nil {
		return e.cause.Error()
	}
	return ErrHandledWithFailure.Error()
}

func (e *handledFailure) Unwrap() error {
	return e.cause
}

func (e *handledFailure) Is(target error) bool {
	if target == ErrHandledWithFailure {
		return true
	}
	return e.cause != nil && errors.Is(e.cause, target)
}

func (e *handledFailure) ErrorClass() string {
	return e.class
}

func (e *handledFailure) ShouldAlert() bool {
	return e.alert
}

func NewHandledFailure(class string, alert bool, msg string, cause error) error {
	return &handledFailure{
		class: class,
		alert: alert,
		msg:   msg,
		cause: cause,
	}
}

func handledFailureClass(err error) (string, bool) {
	var classified classifiedError
	if !errors.As(err, &classified) {
		return "", false
	}
	return classified.ErrorClass(), true
}

func handledFailureShouldAlert(err error) bool {
	var classified classifiedError
	if !errors.As(err, &classified) {
		return true
	}
	return classified.ShouldAlert()
}

type Reply struct {
	Type        transport.ReplyType
	Text        string
	ImageBase64 string   // single image as base64
	Images      []string // multiple images as base64 (for image_multiple)
	Metadata    map[string]any
}

type ReplyFunc func(context.Context, Reply) error

type CommandContext struct {
	Message transport.Message
	Command string
	Source  string
	Args    []string
	Reply   ReplyFunc
	Now     func() time.Time
}

type Handler interface {
	Name() string
	Aliases() []string
	Description() string
	Execute(context.Context, CommandContext) error
}

type DescribedHandler interface {
	Handler
	Descriptor() commandmeta.Descriptor
}

type BareQueryMatcher interface {
	MatchBareQuery(context.Context, string) ([]string, bool)
}

type AutoQueryCandidateMatcher interface {
	MatchAutoQueryCandidate(context.Context, string) bool
}

type SlashCommandMode interface {
	SupportsSlashCommands() bool
}

type HandlerFunc func(context.Context, CommandContext) error

type Middleware interface {
	Wrap(HandlerFunc) HandlerFunc
}
