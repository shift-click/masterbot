package providers

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/sony/gobreaker/v2"
)

// BreakerHTTPClient wraps an *http.Client with a circuit breaker.
// The breaker protects the Do method; callers use Do exactly like http.Client.Do.
type BreakerHTTPClient struct {
	inner   *http.Client
	breaker *gobreaker.CircuitBreaker[*http.Response]
}

// DefaultBreakerSettings returns circuit breaker settings tuned for external API providers.
//
//   - Interval (60s): rolling window for counting failures.
//   - Timeout (30s): how long the breaker stays open before transitioning to half-open.
//   - ReadyToTrip: opens when >= 5 requests observed AND failure ratio >= 60%.
//   - MaxRequests (2): probe requests allowed in half-open state.
func DefaultBreakerSettings(name string, logger *slog.Logger) gobreaker.Settings {
	if logger == nil {
		logger = slog.Default()
	}
	return gobreaker.Settings{
		Name:        name,
		Interval:    60 * time.Second,
		Timeout:     30 * time.Second,
		MaxRequests: 2,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.Requests >= 5 &&
				float64(counts.TotalFailures)/float64(counts.Requests) >= 0.6
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			logger.Warn("circuit breaker state change",
				"breaker", name,
				"from", from.String(),
				"to", to.String(),
			)
		},
	}
}

// NewBreakerHTTPClient wraps an http.Client with a circuit breaker.
func NewBreakerHTTPClient(client *http.Client, name string, logger *slog.Logger) *BreakerHTTPClient {
	return &BreakerHTTPClient{
		inner:   client,
		breaker: gobreaker.NewCircuitBreaker[*http.Response](DefaultBreakerSettings(name, logger)),
	}
}

// Do executes the request through the circuit breaker.
// Network errors, server errors (5xx), and rate limits (429) are counted as
// failures. Other non-2xx status codes (e.g. 404, 400) pass through without
// tripping the breaker, since those are typically client-side issues.
func (b *BreakerHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return b.breaker.Execute(func() (*http.Response, error) {
		resp, err := b.inner.Do(req)
		if err != nil {
			return nil, err
		}
		// Count server errors (5xx) and rate limits (429) as breaker failures.
		// We drain and close the body before returning so the caller's
		// `if err != nil { return }` path does not leak the connection.
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			return nil, fmt.Errorf("breaker: server error HTTP %d", resp.StatusCode)
		}
		return resp, nil
	})
}

// Unwrap returns the underlying http.Client (useful for tests or special cases).
func (b *BreakerHTTPClient) Unwrap() *http.Client {
	return b.inner
}

// State returns the current state of the circuit breaker.
func (b *BreakerHTTPClient) State() gobreaker.State {
	return b.breaker.State()
}

// DefaultBreakerClient creates a DefaultClient wrapped with a circuit breaker.
// This is a convenience for the common case: HTTP client + breaker in one call.
func DefaultBreakerClient(timeout time.Duration, name string, logger *slog.Logger) *BreakerHTTPClient {
	return NewBreakerHTTPClient(DefaultClient(timeout), name, logger)
}

// SetTransport replaces the Transport on the underlying http.Client.
// This is intended for testing (mock round-trippers).
func (b *BreakerHTTPClient) SetTransport(rt http.RoundTripper) {
	b.inner.Transport = rt
}

