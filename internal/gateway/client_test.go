package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/internal/auth"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestHandleBundleNotification(t *testing.T) {
	ctx := context.Background()
	privateKey, _ := crypto.GenerateKey()
	creds := &auth.Credentials{
		SidecarKey: privateKey,
		SidecarID:  "test-sidecar",
	}

	client := NewClient("http://localhost:8080", ctx, creds, nil)

	tests := []struct {
		name        string
		params      interface{}
		wantErr     bool
		wantTxCount int
	}{
		{
			name: "valid bundle with 2 transactions",
			params: map[string]interface{}{
				"txs": []interface{}{
					"0x1234567890abcdef",
					"0xfedcba0987654321",
				},
			},
			wantErr:     false,
			wantTxCount: 2,
		},
		{
			name: "bundle with 1 transaction (warning logged)",
			params: map[string]interface{}{
				"txs": []interface{}{
					"0x1234567890abcdef",
				},
			},
			wantErr:     false,
			wantTxCount: 1,
		},
		{
			name: "bundle with 3 transactions (warning logged)",
			params: map[string]interface{}{
				"txs": []interface{}{
					"0x1234567890abcdef",
					"0xfedcba0987654321",
					"0xabcdef1234567890",
				},
			},
			wantErr:     false,
			wantTxCount: 3,
		},
		{
			name:    "missing txs field",
			params:  map[string]interface{}{},
			wantErr: true,
		},
		{
			name:    "invalid params type",
			params:  "invalid",
			wantErr: true,
		},
		{
			name: "invalid hex in transaction",
			params: map[string]interface{}{
				"txs": []interface{}{
					"0xZZZZ", // Invalid hex
				},
			},
			wantErr:     false,
			wantTxCount: 0, // Should skip invalid tx
		},
		{
			name: "transaction without 0x prefix",
			params: map[string]interface{}{
				"txs": []interface{}{
					"1234567890abcdef",
				},
			},
			wantErr:     false,
			wantTxCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear the channel
			for len(client.txChan) > 0 {
				<-client.txChan
			}

			err := client.handleBundleNotification(tt.params)
			if (err != nil) != tt.wantErr {
				t.Errorf("handleBundleNotification() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Check that the correct number of transactions were received
			receivedCount := 0
			timeout := time.After(100 * time.Millisecond)
		countLoop:
			for {
				select {
				case <-client.txChan:
					receivedCount++
				case <-timeout:
					break countLoop
				default:
					if len(client.txChan) == 0 {
						break countLoop
					}
				}
			}

			if receivedCount != tt.wantTxCount {
				t.Errorf("handleBundleNotification() received %d transactions, want %d", receivedCount, tt.wantTxCount)
			}
		})
	}
}

func TestHandleAuthExpiring(t *testing.T) {
	ctx := context.Background()
	privateKey, _ := crypto.GenerateKey()
	creds := &auth.Credentials{
		SidecarKey:    privateKey,
		SidecarID:     "test-sidecar",
		RefreshToken:  "test-refresh-token",
		SessionNonce:  "test-nonce",
		AccessToken:   "test-access-token",
		TokenExpiry:   time.Now().Add(5 * time.Minute),
		WssURL:        "",
		WsSubprotocol: "",
	}

	client := NewClient("http://localhost:8080", ctx, creds, nil)

	tests := []struct {
		name    string
		params  interface{}
		wantErr bool
	}{
		{
			name: "valid auth expiring with expires_at",
			params: map[string]interface{}{
				"expires_at": time.Now().Add(1 * time.Minute).Format(time.RFC3339),
			},
			wantErr: false,
		},
		{
			name: "auth expiring with challenge (triggers refresh attempt)",
			params: map[string]interface{}{
				"expires_at": time.Now().Add(1 * time.Minute).Format(time.RFC3339),
				"challenge":  "test-challenge-123",
			},
			wantErr: true, // Will fail because no connection, but that's expected behavior
		},
		{
			name:    "auth expiring without expires_at",
			params:  map[string]interface{}{},
			wantErr: false,
		},
		{
			name:    "invalid params type",
			params:  "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.handleAuthExpiring(tt.params)
			if (err != nil) != tt.wantErr {
				t.Errorf("handleAuthExpiring() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHandleRateLimited(t *testing.T) {
	ctx := context.Background()
	privateKey, _ := crypto.GenerateKey()
	creds := &auth.Credentials{
		SidecarKey: privateKey,
		SidecarID:  "test-sidecar",
	}

	client := NewClient("http://localhost:8080", ctx, creds, nil)

	tests := []struct {
		name    string
		params  interface{}
		wantErr bool
	}{
		{
			name: "valid rate limit with all fields",
			params: map[string]interface{}{
				"retry_after_ms": float64(5000),
				"reason":         "too many requests",
				"limit":          float64(100),
			},
			wantErr: false,
		},
		{
			name: "rate limit with only retry_after",
			params: map[string]interface{}{
				"retry_after_ms": float64(1000),
			},
			wantErr: false,
		},
		{
			name:    "rate limit with empty params",
			params:  map[string]interface{}{},
			wantErr: false,
		},
		{
			name:    "invalid params type (should not error, just warn)",
			params:  "invalid",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.handleRateLimited(tt.params)
			if (err != nil) != tt.wantErr {
				t.Errorf("handleRateLimited() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHandleNotification(t *testing.T) {
	ctx := context.Background()
	privateKey, _ := crypto.GenerateKey()
	creds := &auth.Credentials{
		SidecarKey:   privateKey,
		SidecarID:    "test-sidecar",
		RefreshToken: "test-refresh-token",
		SessionNonce: "test-nonce",
	}

	client := NewClient("http://localhost:8080", ctx, creds, nil)

	tests := []struct {
		name    string
		notif   JSONRPCNotification
		wantErr bool
	}{
		{
			name: "validator_bundle_notification",
			notif: JSONRPCNotification{
				JSONRPC: "2.0",
				Method:  "validator_bundle_notification",
				Params: map[string]interface{}{
					"txs": []interface{}{
						"0x1234567890abcdef",
						"0xfedcba0987654321",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "validator_auth_expiring",
			notif: JSONRPCNotification{
				JSONRPC: "2.0",
				Method:  "validator_auth_expiring",
				Params: map[string]interface{}{
					"expires_at": time.Now().Add(1 * time.Minute).Format(time.RFC3339),
				},
			},
			wantErr: false,
		},
		{
			name: "validator_rate_limited",
			notif: JSONRPCNotification{
				JSONRPC: "2.0",
				Method:  "validator_rate_limited",
				Params: map[string]interface{}{
					"retry_after_ms": float64(5000),
				},
			},
			wantErr: false,
		},
		{
			name: "unknown_notification",
			notif: JSONRPCNotification{
				JSONRPC: "2.0",
				Method:  "unknown_method",
				Params:  map[string]interface{}{},
			},
			wantErr: false, // Unknown methods should not error, just log debug
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.handleNotification(tt.notif)
			if (err != nil) != tt.wantErr {
				t.Errorf("handleNotification() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewClient_MaxInflight(t *testing.T) {
	ctx := context.Background()
	privateKey, _ := crypto.GenerateKey()

	tests := []struct {
		name               string
		maxInflight        int
		expectedBufferSize int
	}{
		{
			name:               "default buffer size when MaxInflight is 0",
			maxInflight:        0,
			expectedBufferSize: 100,
		},
		{
			name:               "custom buffer size from MaxInflight",
			maxInflight:        256,
			expectedBufferSize: 256,
		},
		{
			name:               "small buffer size",
			maxInflight:        10,
			expectedBufferSize: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds := &auth.Credentials{
				SidecarKey:  privateKey,
				SidecarID:   "test-sidecar",
				MaxInflight: tt.maxInflight,
			}

			client := NewClient("http://localhost:8080", ctx, creds, nil)

			// Check buffer capacity by trying to send messages
			actualCapacity := cap(client.txChan)
			if actualCapacity != tt.expectedBufferSize {
				t.Errorf("NewClient() txChan capacity = %d, want %d", actualCapacity, tt.expectedBufferSize)
			}
		})
	}
}
