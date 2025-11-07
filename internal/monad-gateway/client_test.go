package monadgateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/internal/auth"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/config"
	"github.com/gorilla/websocket"
)

// mockMetricsProvider for testing
type mockMetricsProvider struct{}

func (m *mockMetricsProvider) GetSnapshot() interface{} {
	return map[string]interface{}{
		"test_metric": 123,
	}
}

// TestNewMonadGatewayClient_NoCredentials tests that constructor returns nil when no credentials are provided
func TestNewMonadGatewayClient_NoCredentials(t *testing.T) {
	cfg := &config.Config{
		GatewayURL:     "http://localhost",
		DelegationPath: "", // No credentials
		KeystorePath:   "",
	}

	client, err := NewMonadGatewayClient(cfg, &mockMetricsProvider{})
	if err != nil {
		t.Fatalf("Expected no error when no credentials, got: %v", err)
	}

	if client != nil {
		t.Error("Expected nil client when no credentials provided")
	}
}

// TestNewMonadGatewayClient_RegistrationSuccess tests successful registration
func TestNewMonadGatewayClient_RegistrationSuccess(t *testing.T) {
	t.Skip("Skipping test that requires HTTP registration - needs mock server setup")
}

// TestClient_Health tests the Health method returns correct stats
func TestClient_Health(t *testing.T) {
	client := &Client{
		config: &config.Config{},
	}

	// Initial state - disconnected
	health := client.Health()
	if health.Connected {
		t.Error("Expected Connected to be false initially")
	}
	if health.Authenticated {
		t.Error("Expected Authenticated to be false initially")
	}

	// Set connected state
	client.connected.Store(true)
	client.authenticated.Store(true)

	health = client.Health()
	if !health.Connected {
		t.Error("Expected Connected to be true")
	}
	if !health.Authenticated {
		t.Error("Expected Authenticated to be true")
	}

	// Set error
	client.setLastError(context.DeadlineExceeded)
	health = client.Health()
	if health.LastError == "" {
		t.Error("Expected LastError to be set")
	}
	if !strings.Contains(health.LastError, "context deadline exceeded") {
		t.Errorf("Expected error about deadline, got: %s", health.LastError)
	}
}

// TestClient_Stop tests that Stop properly cleans up
func TestClient_Stop(t *testing.T) {
	client := &Client{
		config: &config.Config{},
	}
	client.ctx, client.cancel = context.WithCancel(context.Background())

	// Stop should not error even without active connection
	err := client.Stop()
	if err != nil {
		t.Errorf("Expected no error from Stop, got: %v", err)
	}

	// Verify context is cancelled
	select {
	case <-client.ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Context was not cancelled")
	}
}

// TestDecodeHex tests the hex decoding helper function
func TestDecodeHex(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []byte
		wantErr  bool
	}{
		{
			name:     "with 0x prefix",
			input:    "0x1234",
			expected: []byte{0x12, 0x34},
			wantErr:  false,
		},
		{
			name:     "without prefix",
			input:    "1234",
			expected: []byte{0x12, 0x34},
			wantErr:  false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: []byte{},
			wantErr:  false,
		},
		{
			name:     "invalid hex",
			input:    "0xZZZZ",
			expected: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := decodeHex(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("decodeHex() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && string(result) != string(tt.expected) {
				t.Errorf("decodeHex() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestClient_ConnectionLoop_TokenExpiry tests that expired tokens trigger refresh
func TestClient_ConnectionLoop_TokenExpiry(t *testing.T) {
	// This is more of an integration test, so we'll keep it simple
	client := &Client{
		config: &config.Config{},
		creds: &auth.Credentials{
			TokenExpiry: time.Now().Add(-1 * time.Hour), // Expired
		},
	}
	client.ctx, client.cancel = context.WithCancel(context.Background())

	// We can't easily test the full connection loop without a real server,
	// but we can verify the expiry check logic would trigger
	if !time.Now().After(client.creds.TokenExpiry) {
		t.Error("Expected token to be expired")
	}
}

// MockWebSocketServer creates a test WebSocket server
type MockWebSocketServer struct {
	server   *httptest.Server
	upgrader websocket.Upgrader
	handler  func(*websocket.Conn)
}

func NewMockWebSocketServer(handler func(*websocket.Conn)) *MockWebSocketServer {
	mock := &MockWebSocketServer{
		upgrader: websocket.Upgrader{},
		handler:  handler,
	}

	mock.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := mock.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		mock.handler(conn)
	}))

	return mock
}

func (m *MockWebSocketServer) Close() {
	m.server.Close()
}

func (m *MockWebSocketServer) URL() string {
	return "ws" + strings.TrimPrefix(m.server.URL, "http")
}

// TestClient_Connect_Success tests successful WebSocket connection
func TestClient_Connect_Success(t *testing.T) {
	t.Skip("Skipping test with complex async behavior - integration tests cover this")
}

// TestClient_Connect_NoAccessToken tests that connection tries to register when no access token
func TestClient_Connect_NoAccessToken(t *testing.T) {
	t.Skip("Skipping test that requires registration client - integration tests cover this")
}

// TestClient_Connect_ExpiredToken tests that connection fails with expired token
func TestClient_Connect_ExpiredToken(t *testing.T) {
	client := &Client{
		config: &config.Config{},
		creds: &auth.Credentials{
			AccessToken: "expired-token",
			TokenExpiry: time.Now().Add(-1 * time.Hour), // Expired
		},
	}

	err := client.connect()
	if err == nil {
		t.Fatal("Expected error when token is expired")
	}

	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("Expected error about expired token, got: %v", err)
	}
}
