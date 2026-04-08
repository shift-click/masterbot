package providers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sony/gobreaker/v2"
)

func TestBreakerHTTPClient_SuccessPassesThrough(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)

	bc := NewBreakerHTTPClient(srv.Client(), "test-success", nil)

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := bc.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("Body = %q, want %q", body, "ok")
	}

	if bc.State() != gobreaker.StateClosed {
		t.Errorf("State = %v, want Closed", bc.State())
	}
}

func TestBreakerHTTPClient_ServerErrorCountsAsFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	}))
	t.Cleanup(srv.Close)

	bc := NewBreakerHTTPClient(srv.Client(), "test-5xx", nil)

	for i := 0; i < 6; i++ {
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		_, err := bc.Do(req)
		if err == nil {
			t.Fatal("expected error for 5xx response")
		}
		if !strings.Contains(err.Error(), "server error HTTP 500") {
			// After breaker opens, error will be gobreaker's ErrOpenState
			if bc.State() == gobreaker.StateOpen {
				break
			}
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if bc.State() != gobreaker.StateOpen {
		t.Errorf("State = %v, want Open after repeated 5xx", bc.State())
	}
}

func TestBreakerHTTPClient_ClientErrorDoesNotTrip(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	bc := NewBreakerHTTPClient(srv.Client(), "test-4xx", nil)

	for i := 0; i < 10; i++ {
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		resp, err := bc.Do(req)
		if err != nil {
			t.Fatalf("Do() error = %v, want nil for 404", err)
		}
		resp.Body.Close()
	}

	if bc.State() != gobreaker.StateClosed {
		t.Errorf("State = %v, want Closed (4xx should not trip breaker)", bc.State())
	}
}

func TestBreakerHTTPClient_429CountsAsFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	bc := NewBreakerHTTPClient(srv.Client(), "test-429", nil)

	for i := 0; i < 6; i++ {
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		_, _ = bc.Do(req)
	}

	if bc.State() != gobreaker.StateOpen {
		t.Errorf("State = %v, want Open after repeated 429", bc.State())
	}
}

func TestBreakerHTTPClient_Unwrap(t *testing.T) {
	t.Parallel()

	inner := &http.Client{}
	bc := NewBreakerHTTPClient(inner, "test-unwrap", nil)

	if bc.Unwrap() != inner {
		t.Error("Unwrap() did not return the inner client")
	}
}

func TestDefaultBreakerClient(t *testing.T) {
	t.Parallel()

	bc := DefaultBreakerClient(5_000_000_000, "test-default", nil)
	if bc == nil {
		t.Fatal("DefaultBreakerClient returned nil")
	}
	if bc.Unwrap() == nil {
		t.Fatal("inner client is nil")
	}
	if bc.State() != gobreaker.StateClosed {
		t.Errorf("initial State = %v, want Closed", bc.State())
	}
}
