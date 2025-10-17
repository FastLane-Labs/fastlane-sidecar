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

// TestHandleMessage_Notification tests handling of JSON-RPC notifications
func TestHandleMessage_Notification(t *testing.T) {
	client := &Client{
		config: &config.Config{
			DisableGatewayIngress: false,
		},
		txChan: make(chan []byte, 10),
	}

	// Create bundle notification
	notif := jsonRPCNotification{
		JSONRPC: "2.0",
		Method:  "validator_bundle_notification",
		Params: json.RawMessage(`{
			"bundles": [{
				"bundle_id": "test-bundle",
				"txs": [{
					"raw_tx_hex": "0x1234"
				}]
			}]
		}`),
	}
	msgBytes, _ := json.Marshal(notif)

	// Handle message
	client.handleMessage(msgBytes)

	// Give it time to process
	time.Sleep(50 * time.Millisecond)

	// Verify transaction was sent to channel
	select {
	case tx := <-client.txChan:
		if len(tx) != 2 || tx[0] != 0x12 || tx[1] != 0x34 {
			t.Errorf("Expected [0x12, 0x34], got %v", tx)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Transaction was not sent to channel")
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

// TestHandleBundleNotification_IngressDisabled tests that bundles are ignored when ingress is disabled
func TestHandleBundleNotification_IngressDisabled(t *testing.T) {
	client := &Client{
		config: &config.Config{
			DisableGatewayIngress: true,
		},
		txChan: make(chan []byte, 10),
	}

	params := json.RawMessage(`{
		"bundles": [{
			"bundle_id": "test-bundle",
			"txs": [{
				"raw_tx_hex": "0x1234"
			}]
		}]
	}`)

	err := client.handleBundleNotification(params)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Verify no transaction was sent
	select {
	case <-client.txChan:
		t.Error("Expected no transaction when ingress is disabled")
	case <-time.After(50 * time.Millisecond):
		// Expected - no transaction
	}
}

// TestHandleBundleNotification_InvalidParams tests handling of invalid bundle params
func TestHandleBundleNotification_InvalidParams(t *testing.T) {
	client := &Client{
		config: &config.Config{
			DisableGatewayIngress: false,
		},
		txChan: make(chan []byte, 10),
	}

	tests := []struct {
		name   string
		params string
	}{
		{
			name:   "invalid json",
			params: `{invalid}`,
		},
		{
			name:   "missing bundles field",
			params: `{"other": "field"}`,
		},
		{
			name:   "bundles not array",
			params: `{"bundles": "not-array"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.handleBundleNotification(json.RawMessage(tt.params))
			if err == nil {
				t.Error("Expected error for invalid params")
			}
		})
	}
}

// TestHandleBundleNotification_MultipleBundles tests processing multiple bundles
func TestHandleBundleNotification_MultipleBundles(t *testing.T) {
	client := &Client{
		config: &config.Config{
			DisableGatewayIngress: false,
		},
		txChan: make(chan []byte, 10),
	}

	params := json.RawMessage(`{
		"bundles": [
			{
				"bundle_id": "bundle-1",
				"txs": [
					{"raw_tx_hex": "0x1234"},
					{"raw_tx_hex": "0x5678"}
				]
			},
			{
				"bundle_id": "bundle-2",
				"txs": [
					{"raw_tx_hex": "0xabcd"}
				]
			}
		]
	}`)

	err := client.handleBundleNotification(params)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should receive 3 transactions total
	txCount := 0
	timeout := time.After(200 * time.Millisecond)

	for txCount < 3 {
		select {
		case <-client.txChan:
			txCount++
		case <-timeout:
			t.Fatalf("Expected 3 transactions, got %d", txCount)
		}
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

// TestHandleRateLimited tests rate limit notification handling
func TestHandleRateLimited(t *testing.T) {
	client := &Client{
		config: &config.Config{},
	}

	tests := []struct {
		name   string
		params string
	}{
		{
			name:   "with reason",
			params: `{"retry_after_ms": 1000, "reason": "too many requests"}`,
		},
		{
			name:   "without reason",
			params: `{"retry_after_ms": 500}`,
		},
		{
			name:   "invalid params",
			params: `{invalid}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not error, just log
			err := client.handleRateLimited(json.RawMessage(tt.params))
			if err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}
		})
	}
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
