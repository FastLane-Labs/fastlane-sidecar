package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// mockStatsProvider implements StatsProvider for testing
type mockStatsProvider struct {
	stats Stats
}

func (m *mockStatsProvider) GetHealthStats() Stats {
	return m.stats
}

func TestHealthEndpoint(t *testing.T) {
	// Create mock stats
	now := time.Now()
	mockProvider := &mockStatsProvider{
		stats: Stats{
			LastHeartbeat:        now,
			TxReceived:           100,
			TxStreamed:           50,
			PoolSize:             25,
			GatewayConnected:     true,
			GatewayAuthenticated: true,
			GatewayError:         "",
		},
	}

	// Create server
	server := NewServer(0, mockProvider) // Port 0 for testing

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	// Call handler
	server.handleHealth(w, req)

	// Check response
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if contentType := resp.Header.Get("Content-Type"); contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	// Parse response body
	var response map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify response fields
	if status, ok := response["status"].(string); !ok || status != "ok" {
		t.Errorf("Expected status 'ok', got %v", response["status"])
	}

	if txReceived, ok := response["tx_received"].(float64); !ok || txReceived != 100 {
		t.Errorf("Expected tx_received 100, got %v", response["tx_received"])
	}

	if txStreamed, ok := response["tx_streamed"].(float64); !ok || txStreamed != 50 {
		t.Errorf("Expected tx_streamed 50, got %v", response["tx_streamed"])
	}

	if poolSize, ok := response["pool_size"].(float64); !ok || poolSize != 25 {
		t.Errorf("Expected pool_size 25, got %v", response["pool_size"])
	}

	// Verify timestamp field exists
	if _, ok := response["timestamp"].(string); !ok {
		t.Errorf("Expected timestamp field, got %v", response["timestamp"])
	}

	// Verify last_heartbeat exists and matches
	if lastHeartbeat, ok := response["last_heartbeat"].(string); !ok {
		t.Errorf("Expected last_heartbeat field, got %v", response["last_heartbeat"])
	} else {
		parsedTime, err := time.Parse(time.RFC3339, lastHeartbeat)
		if err != nil {
			t.Errorf("Failed to parse last_heartbeat: %v", err)
		}
		// Check it's close to the expected time (within 1 second)
		if parsedTime.Sub(now).Abs() > time.Second {
			t.Errorf("last_heartbeat time mismatch: expected %v, got %v", now, parsedTime)
		}
	}

	// Verify gateway_connected field
	if gatewayConnected, ok := response["gateway_connected"].(bool); !ok || !gatewayConnected {
		t.Errorf("Expected gateway_connected true, got %v", response["gateway_connected"])
	}

	// Verify gateway_authenticated field
	if gatewayAuthenticated, ok := response["gateway_authenticated"].(bool); !ok || !gatewayAuthenticated {
		t.Errorf("Expected gateway_authenticated true, got %v", response["gateway_authenticated"])
	}

	// Verify gateway_error is not present when empty
	if _, exists := response["gateway_error"]; exists {
		t.Errorf("Expected gateway_error to be omitted when empty")
	}
}

func TestHealthEndpoint_MethodNotAllowed(t *testing.T) {
	mockProvider := &mockStatsProvider{
		stats: Stats{},
	}

	server := NewServer(0, mockProvider)

	// Test POST method (should be rejected)
	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}
}

func TestHealthEndpoint_WithZeroValues(t *testing.T) {
	// Test with zero values and empty timestamp
	mockProvider := &mockStatsProvider{
		stats: Stats{
			LastHeartbeat: time.Time{}, // Zero value
			TxReceived:    0,
			TxStreamed:    0,
			PoolSize:      0,
		},
	}

	server := NewServer(0, mockProvider)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify zero values are properly returned
	if txReceived, ok := response["tx_received"].(float64); !ok || txReceived != 0 {
		t.Errorf("Expected tx_received 0, got %v", response["tx_received"])
	}
}
