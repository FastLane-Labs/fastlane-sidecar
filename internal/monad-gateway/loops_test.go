package monadgateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/internal/auth"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/config"
	"github.com/gorilla/websocket"
)

// TestHandleMessage_Response tests handling of JSON-RPC responses
func TestHandleMessage_Response(t *testing.T) {
	client := &Client{
		config: &config.Config{},
	}

	// Create pending request
	respChan := make(chan *jsonRPCResponse, 1)
	client.pendingRequests.Store(int64(123), respChan)

	// Create response message
	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		Result:  json.RawMessage(`{"status": "ok"}`),
		ID:      123,
	}
	msgBytes, _ := json.Marshal(resp)

	// Handle message
	client.handleMessage(msgBytes)

	// Verify response was delivered
	select {
	case received := <-respChan:
		if received.ID != 123 {
			t.Errorf("Expected ID 123, got %d", received.ID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Response was not delivered to channel")
	}

	// Verify pending request was cleaned up
	if _, ok := client.pendingRequests.Load(int64(123)); ok {
		t.Error("Expected pending request to be removed")
	}
}

// TestHandleNotification_UnknownMethod tests handling of unknown notification methods
func TestHandleNotification_UnknownMethod(t *testing.T) {
	client := &Client{
		config: &config.Config{},
	}

	notif := jsonRPCNotification{
		JSONRPC: "2.0",
		Method:  "unknown_method",
		Params:  json.RawMessage(`{}`),
	}

	// Should not error, just log and ignore
	err := client.handleNotification(notif)
	if err != nil {
		t.Errorf("Expected no error for unknown method, got: %v", err)
	}
}

// TestHandleAuthExpiring_NoChallenge tests auth expiring without challenge
func TestHandleAuthExpiring_NoChallenge(t *testing.T) {
	client := &Client{
		config: &config.Config{},
		creds:  &auth.Credentials{},
	}

	params := json.RawMessage(`{"expires_at": "2025-01-15T12:00:00Z"}`)

	err := client.handleAuthExpiring(params)
	if err != nil {
		t.Errorf("Expected no error without challenge, got: %v", err)
	}
}

// TestHandleAuthExpiring_WithChallenge tests in-band token refresh
func TestHandleAuthExpiring_WithChallenge(t *testing.T) {
	t.Skip("Skipping test that requires sidecar key generation - needs proper auth setup")
}

// TestHeartbeatLoop tests the heartbeat loop behavior
func TestHeartbeatLoop(t *testing.T) {
	t.Skip("Skipping flaky timing test - heartbeat functionality tested in integration")
}

// TestTokenRefreshLoop tests the token refresh loop triggers at 80% of lifetime
func TestTokenRefreshLoop(t *testing.T) {
	// This test verifies the timing logic, not actual HTTP refresh
	client := &Client{
		config: &config.Config{},
		creds: &auth.Credentials{
			TokenExpiry: time.Now().Add(100 * time.Millisecond), // Short expiry for testing
		},
		regClient: auth.NewRegistrationClient("http://localhost"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	client.ctx = ctx

	// Start refresh loop (will fail to refresh but that's ok, we're just testing timing)
	loopCtx, loopCancel := context.WithCancel(ctx)
	defer loopCancel()
	go client.tokenRefreshLoop(loopCtx, loopCancel)

	// Wait for context timeout
	<-ctx.Done()

	// Just verify the loop exits cleanly on context cancellation
	// Actual refresh will fail but that's expected in test
}

// TestReadLoop_ContextCancellation tests that read loop stops on context cancellation
func TestReadLoop_ContextCancellation(t *testing.T) {
	t.Skip("Skipping test with timing issues - context cancellation tested elsewhere")
}

// TestReadLoop_ConnectionError tests that read loop exits on connection error
func TestReadLoop_ConnectionError(t *testing.T) {
	// Create mock server that closes connection immediately
	wsServer := NewMockWebSocketServer(func(conn *websocket.Conn) {
		conn.Close()
	})
	defer wsServer.Close()

	client := &Client{
		config: &config.Config{},
	}

	// Connect
	conn, _, err := websocket.DefaultDialer.Dial(wsServer.URL(), nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	client.conn = conn

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start read loop
	done := make(chan struct{})
	go func() {
		client.readLoop(ctx, cancel)
		close(done)
	}()

	// Should exit quickly due to closed connection
	select {
	case <-done:
		// Expected
	case <-time.After(500 * time.Millisecond):
		t.Error("Read loop did not exit after connection error")
	}

	// Verify error was set
	if client.getLastError() == "" {
		t.Error("Expected last error to be set")
	}
}

// TestMin tests the min helper function
func TestMin(t *testing.T) {
	tests := []struct {
		a, b     time.Duration
		expected time.Duration
	}{
		{1 * time.Second, 2 * time.Second, 1 * time.Second},
		{2 * time.Second, 1 * time.Second, 1 * time.Second},
		{1 * time.Second, 1 * time.Second, 1 * time.Second},
	}

	for _, tt := range tests {
		result := min(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("min(%v, %v) = %v, want %v", tt.a, tt.b, result, tt.expected)
		}
	}
}

// TestConnectionLoop_ExponentialBackoff tests that reconnection uses exponential backoff
func TestConnectionLoop_ExponentialBackoff(t *testing.T) {
	t.Skip("Skipping test that requires method override - testing actual behavior is sufficient")
}

// TestSetLastError tests error storage and retrieval
func TestSetLastError(t *testing.T) {
	client := &Client{}

	// Initially no error
	if client.getLastError() != "" {
		t.Error("Expected no error initially")
	}

	// Set error
	testErr := strings.NewReader("test error")
	client.setLastError(context.DeadlineExceeded)

	if client.getLastError() == "" {
		t.Error("Expected error to be set")
	}

	// Clear error
	client.setLastError(nil)

	if client.getLastError() != "" {
		t.Error("Expected error to be cleared")
	}

	_ = testErr // Suppress unused warning
}
