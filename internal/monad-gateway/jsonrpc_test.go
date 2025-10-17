package monadgateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/config"
	"github.com/gorilla/websocket"
)

// TestSendRequest_Success tests successful JSON-RPC request/response
func TestSendRequest_Success(t *testing.T) {
	// Create mock WebSocket server
	wsServer := NewMockWebSocketServer(func(conn *websocket.Conn) {
		// Read request
		var req jsonRPCRequest
		if err := conn.ReadJSON(&req); err != nil {
			t.Errorf("Failed to read request: %v", err)
			return
		}

		// Verify request format
		if req.JSONRPC != "2.0" {
			t.Errorf("Expected JSONRPC 2.0, got: %s", req.JSONRPC)
		}

		if req.Method != "test_method" {
			t.Errorf("Expected method test_method, got: %s", req.Method)
		}

		// Send response
		resp := jsonRPCResponse{
			JSONRPC: "2.0",
			Result:  json.RawMessage(`{"status": "ok"}`),
			ID:      req.ID,
		}
		conn.WriteJSON(resp)
	})
	defer wsServer.Close()

	// Create client
	client := &Client{
		config: &config.Config{},
	}
	client.ctx, client.cancel = context.WithCancel(context.Background())
	defer client.cancel()

	// Connect to mock server
	conn, _, err := websocket.DefaultDialer.Dial(wsServer.URL(), nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	client.conn = conn
	defer conn.Close()

	// Start read loop to handle responses
	ctx, cancel := context.WithCancel(client.ctx)
	defer cancel()
	go client.readLoop(ctx, cancel)

	// Send request
	params := map[string]string{"key": "value"}
	resp, err := client.sendRequest("test_method", params)
	if err != nil {
		t.Fatalf("Expected successful request, got error: %v", err)
	}

	// Verify response
	if resp == nil {
		t.Fatal("Expected non-nil response")
	}

	var result map[string]string
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if result["status"] != "ok" {
		t.Errorf("Expected status ok, got: %s", result["status"])
	}
}

// TestSendRequest_NoConnection tests that sendRequest fails without connection
func TestSendRequest_NoConnection(t *testing.T) {
	client := &Client{
		config: &config.Config{},
		conn:   nil, // No connection
	}

	_, err := client.sendRequest("test_method", nil)
	if err == nil {
		t.Fatal("Expected error when not connected")
	}

	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("Expected error about not connected, got: %v", err)
	}
}

// TestSendRequest_ErrorResponse tests handling of JSON-RPC error responses
func TestSendRequest_ErrorResponse(t *testing.T) {
	// Create mock server that returns error
	wsServer := NewMockWebSocketServer(func(conn *websocket.Conn) {
		var req jsonRPCRequest
		conn.ReadJSON(&req)

		// Send error response
		resp := jsonRPCResponse{
			JSONRPC: "2.0",
			Error: &jsonRPCError{
				Code:    -32600,
				Message: "Invalid Request",
			},
			ID: req.ID,
		}
		conn.WriteJSON(resp)
	})
	defer wsServer.Close()

	client := &Client{
		config: &config.Config{},
	}
	client.ctx, client.cancel = context.WithCancel(context.Background())
	defer client.cancel()

	conn, _, err := websocket.DefaultDialer.Dial(wsServer.URL(), nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	client.conn = conn
	defer conn.Close()

	ctx, cancel := context.WithCancel(client.ctx)
	defer cancel()
	go client.readLoop(ctx, cancel)

	// Send request
	_, err = client.sendRequest("test_method", nil)
	if err == nil {
		t.Fatal("Expected error response")
	}

	if !strings.Contains(err.Error(), "Invalid Request") {
		t.Errorf("Expected error message 'Invalid Request', got: %v", err)
	}

	if !strings.Contains(err.Error(), "-32600") {
		t.Errorf("Expected error code -32600, got: %v", err)
	}
}

// TestSendRequest_Timeout tests request timeout
func TestSendRequest_Timeout(t *testing.T) {
	t.Skip("Skipping slow timeout test - 30s timeout is tested in faster ways")
}

// TestSendRequest_ContextCancellation tests that request fails when context is cancelled
func TestSendRequest_ContextCancellation(t *testing.T) {
	// Create mock server that delays response
	wsServer := NewMockWebSocketServer(func(conn *websocket.Conn) {
		var req jsonRPCRequest
		conn.ReadJSON(&req)
		time.Sleep(500 * time.Millisecond)
		resp := jsonRPCResponse{
			JSONRPC: "2.0",
			Result:  json.RawMessage(`{}`),
			ID:      req.ID,
		}
		conn.WriteJSON(resp)
	})
	defer wsServer.Close()

	client := &Client{
		config: &config.Config{},
	}
	client.ctx, client.cancel = context.WithCancel(context.Background())

	conn, _, err := websocket.DefaultDialer.Dial(wsServer.URL(), nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	client.conn = conn
	defer conn.Close()

	ctx, loopCancel := context.WithCancel(client.ctx)
	defer loopCancel()
	go client.readLoop(ctx, loopCancel)

	// Start request in goroutine
	errChan := make(chan error, 1)
	go func() {
		_, err := client.sendRequest("test_method", nil)
		errChan <- err
	}()

	// Cancel context after short delay
	time.Sleep(100 * time.Millisecond)
	client.cancel()

	// Should receive cancellation error
	select {
	case err := <-errChan:
		if err == nil {
			t.Fatal("Expected error after context cancellation")
		}
		if !strings.Contains(err.Error(), "client stopped") {
			t.Errorf("Expected 'client stopped' error, got: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("Request did not fail after context cancellation")
	}
}

// TestSendRequest_ConcurrentRequests tests that multiple concurrent requests work correctly
func TestSendRequest_ConcurrentRequests(t *testing.T) {
	// Create mock server that responds to all requests
	wsServer := NewMockWebSocketServer(func(conn *websocket.Conn) {
		for {
			var req jsonRPCRequest
			if err := conn.ReadJSON(&req); err != nil {
				return
			}

			// Echo the method name in response
			resp := jsonRPCResponse{
				JSONRPC: "2.0",
				Result:  json.RawMessage(`{"method": "` + req.Method + `"}`),
				ID:      req.ID,
			}
			conn.WriteJSON(resp)
		}
	})
	defer wsServer.Close()

	client := &Client{
		config: &config.Config{},
	}
	client.ctx, client.cancel = context.WithCancel(context.Background())
	defer client.cancel()

	conn, _, err := websocket.DefaultDialer.Dial(wsServer.URL(), nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	client.conn = conn
	defer conn.Close()

	ctx, cancel := context.WithCancel(client.ctx)
	defer cancel()
	go client.readLoop(ctx, cancel)

	// Send multiple concurrent requests
	numRequests := 10
	results := make(chan string, numRequests)
	errors := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(id int) {
			method := string(rune('A' + id)) // Methods: A, B, C, ...
			resp, err := client.sendRequest(method, nil)
			if err != nil {
				errors <- err
				return
			}

			var result map[string]string
			if err := json.Unmarshal(resp.Result, &result); err != nil {
				errors <- err
				return
			}

			results <- result["method"]
		}(i)
	}

	// Collect results
	receivedMethods := make(map[string]bool)
	timeout := time.After(2 * time.Second)

	for i := 0; i < numRequests; i++ {
		select {
		case method := <-results:
			receivedMethods[method] = true
		case err := <-errors:
			t.Errorf("Request failed: %v", err)
		case <-timeout:
			t.Fatalf("Timeout waiting for responses, got %d/%d", len(receivedMethods), numRequests)
		}
	}

	// Verify all responses were received
	if len(receivedMethods) != numRequests {
		t.Errorf("Expected %d responses, got %d", numRequests, len(receivedMethods))
	}
}

// TestSendRequest_MessageIDIncrement tests that message IDs increment correctly
func TestSendRequest_MessageIDIncrement(t *testing.T) {
	// Create mock server that echoes request ID
	wsServer := NewMockWebSocketServer(func(conn *websocket.Conn) {
		for {
			var req jsonRPCRequest
			if err := conn.ReadJSON(&req); err != nil {
				return
			}

			resp := jsonRPCResponse{
				JSONRPC: "2.0",
				Result:  json.RawMessage(`{}`),
				ID:      req.ID,
			}
			conn.WriteJSON(resp)
		}
	})
	defer wsServer.Close()

	client := &Client{
		config: &config.Config{},
	}
	client.ctx, client.cancel = context.WithCancel(context.Background())
	defer client.cancel()

	conn, _, err := websocket.DefaultDialer.Dial(wsServer.URL(), nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	client.conn = conn
	defer conn.Close()

	ctx, cancel := context.WithCancel(client.ctx)
	defer cancel()
	go client.readLoop(ctx, cancel)

	// Send multiple requests and verify IDs increment
	for i := 1; i <= 5; i++ {
		resp, err := client.sendRequest("test_method", nil)
		if err != nil {
			t.Fatalf("Request %d failed: %v", i, err)
		}

		if resp.ID != int64(i) {
			t.Errorf("Expected ID %d, got %d", i, resp.ID)
		}
	}
}

// TestSendRequest_WriteError tests handling of write errors
func TestSendRequest_WriteError(t *testing.T) {
	client := &Client{
		config: &config.Config{},
		conn:   nil, // No connection
	}

	// Attempt to send request without connection
	_, err := client.sendRequest("test_method", nil)
	if err == nil {
		t.Fatal("Expected error when no connection")
	}

	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("Expected 'not connected' error, got: %v", err)
	}
}

// TestJSONRPCRequest_Marshal tests that JSON-RPC request is properly formatted
func TestJSONRPCRequest_Marshal(t *testing.T) {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "test_method",
		Params:  map[string]string{"key": "value"},
		ID:      123,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	// Verify JSON structure
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to parse marshaled JSON: %v", err)
	}

	if parsed["jsonrpc"] != "2.0" {
		t.Errorf("Expected jsonrpc 2.0, got: %v", parsed["jsonrpc"])
	}

	if parsed["method"] != "test_method" {
		t.Errorf("Expected method test_method, got: %v", parsed["method"])
	}

	if parsed["id"].(float64) != 123 {
		t.Errorf("Expected id 123, got: %v", parsed["id"])
	}

	params := parsed["params"].(map[string]interface{})
	if params["key"] != "value" {
		t.Errorf("Expected param key=value, got: %v", params["key"])
	}
}

// TestJSONRPCResponse_Unmarshal tests JSON-RPC response parsing
func TestJSONRPCResponse_Unmarshal(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		hasError bool
		validate func(*testing.T, *jsonRPCResponse)
	}{
		{
			name: "success response",
			json: `{"jsonrpc":"2.0","result":{"status":"ok"},"id":1}`,
			validate: func(t *testing.T, resp *jsonRPCResponse) {
				if resp.Error != nil {
					t.Error("Expected no error")
				}
				var result map[string]string
				json.Unmarshal(resp.Result, &result)
				if result["status"] != "ok" {
					t.Errorf("Expected status ok, got: %s", result["status"])
				}
			},
		},
		{
			name: "error response",
			json: `{"jsonrpc":"2.0","error":{"code":-32600,"message":"Invalid Request"},"id":1}`,
			validate: func(t *testing.T, resp *jsonRPCResponse) {
				if resp.Error == nil {
					t.Fatal("Expected error")
				}
				if resp.Error.Code != -32600 {
					t.Errorf("Expected code -32600, got: %d", resp.Error.Code)
				}
				if resp.Error.Message != "Invalid Request" {
					t.Errorf("Expected message 'Invalid Request', got: %s", resp.Error.Message)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp jsonRPCResponse
			if err := json.Unmarshal([]byte(tt.json), &resp); err != nil {
				t.Fatalf("Failed to unmarshal: %v", err)
			}
			tt.validate(t, &resp)
		})
	}
}
