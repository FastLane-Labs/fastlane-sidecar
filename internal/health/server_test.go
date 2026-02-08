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

// mockMetricsProvider implements MetricsProvider for testing
type mockMetricsProvider struct{}

func (m *mockMetricsProvider) GetSnapshot() interface{} {
	return map[string]interface{}{
		"test_metric": 123,
	}
}

func TestHealthEndpoint(t *testing.T) {
	// Create mock stats with timestamps
	now := time.Now()
	mockProvider := &mockStatsProvider{
		stats: Stats{
			TxReceived:     100,
			TxStreamed:     50,
			PoolSize:       25,
			LastReceivedAt: now,
			LastSentAt:     now.Add(-time.Second),
		},
	}

	// Create server
	server := NewServer(0, mockProvider, &mockMetricsProvider{}, nil) // Port 0 for testing

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

	// Verify status field
	if status, ok := response["status"].(string); !ok || status != "ok" {
		t.Errorf("Expected status 'ok', got %v", response["status"])
	}

	// Verify last_received_at exists
	if _, ok := response["last_received_at"].(string); !ok {
		t.Errorf("Expected last_received_at field, got %v", response["last_received_at"])
	}

	// Verify last_sent_at exists
	if _, ok := response["last_sent_at"].(string); !ok {
		t.Errorf("Expected last_sent_at field, got %v", response["last_sent_at"])
	}
}

func TestHealthEndpoint_MethodNotAllowed(t *testing.T) {
	mockProvider := &mockStatsProvider{
		stats: Stats{},
	}

	server := NewServer(0, mockProvider, &mockMetricsProvider{}, nil)

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
	// Test with zero values (no timestamps set)
	mockProvider := &mockStatsProvider{
		stats: Stats{
			TxReceived: 0,
			TxStreamed: 0,
			PoolSize:   0,
			// LastReceivedAt and LastSentAt are zero time
		},
	}

	server := NewServer(0, mockProvider, &mockMetricsProvider{}, nil)

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

	// Verify timestamps are omitted when zero
	if _, ok := response["last_received_at"]; ok {
		t.Errorf("Expected last_received_at to be omitted when zero, got %v", response["last_received_at"])
	}
	if _, ok := response["last_sent_at"]; ok {
		t.Errorf("Expected last_sent_at to be omitted when zero, got %v", response["last_sent_at"])
	}
}
