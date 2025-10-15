package monadgateway

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
	"github.com/gorilla/websocket"
)

// sendRequest sends a JSON-RPC request and waits for response
func (c *Client) sendRequest(method string, params interface{}) (*jsonRPCResponse, error) {
	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		return nil, fmt.Errorf("not connected to gateway")
	}

	id := c.msgID.Add(1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      id,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create response channel
	respChan := make(chan *jsonRPCResponse, 1)
	c.pendingRequests.Store(id, respChan)
	defer c.pendingRequests.Delete(id)

	// Send request
	c.writeMu.Lock()
	err = conn.WriteMessage(websocket.TextMessage, reqBytes)
	c.writeMu.Unlock()

	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	log.Debug("Sent JSON-RPC request", "method", method, "id", id)

	// Wait for response with timeout
	select {
	case resp := <-respChan:
		if resp.Error != nil {
			return nil, fmt.Errorf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp, nil
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("request timeout after 30s")
	case <-c.ctx.Done():
		return nil, fmt.Errorf("client stopped")
	}
}
