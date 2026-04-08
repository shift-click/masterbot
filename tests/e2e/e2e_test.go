package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shift-click/masterbot/internal/app"
	"github.com/shift-click/masterbot/internal/config"
	"github.com/shift-click/masterbot/internal/transport/httptest"
)

var testBaseURL string

func TestMain(m *testing.M) {
	cfg := config.Default()
	cfg.Iris.Enabled = false
	cfg.HTTPTest.Enabled = true
	cfg.HTTPTest.Addr = "127.0.0.1:0"
	cfg.HTTPTest.ReplyTimeout = 15 * time.Second
	cfg.Admin.Enabled = false
	cfg.Admin.MetricsEnabled = false

	tmpDir, err := os.MkdirTemp("", "jucobot-e2e-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	cfg.Coupang.DBPath = filepath.Join(tmpDir, "coupang.db")
	cfg.Access.RuntimeDBPath = filepath.Join(tmpDir, "access.db")
	cfg.Access.BootstrapAdminRoomChatID = "e2e-admin-room"
	cfg.Access.BootstrapAdminUserID = "e2e-admin-user"

	application, err := app.Build(cfg, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "app.Build: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- application.Run(ctx)
	}()

	// Wait for the HTTP test transport to be ready.
	addr := ""
	deadline := time.After(5 * time.Second)
	for {
		addr = application.TransportAddr()
		if addr != "" {
			break
		}
		select {
		case <-deadline:
			cancel()
			fmt.Fprintf(os.Stderr, "httptest transport did not start in time\n")
			os.Exit(1)
		case err := <-errCh:
			fmt.Fprintf(os.Stderr, "app exited early: %v\n", err)
			os.Exit(1)
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}

	testBaseURL = fmt.Sprintf("http://%s", addr)

	code := m.Run()

	cancel()
	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
	}

	os.Exit(code)
}

// sendMessage sends a message to the bot and returns the response.
func sendMessage(t *testing.T, msg string) httptest.MessageResponse {
	t.Helper()

	body, _ := json.Marshal(httptest.MessageRequest{Msg: msg})
	resp, err := http.Post(testBaseURL+"/message", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /message: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	var result httptest.MessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return result
}

func sendMessageRequest(t *testing.T, req httptest.MessageRequest) httptest.MessageResponse {
	t.Helper()

	body, _ := json.Marshal(req)
	resp, err := http.Post(testBaseURL+"/message", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /message: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	var result httptest.MessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return result
}
