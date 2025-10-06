package gateway

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/internal/auth"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/websocket"
)

type Client struct {
	url    string
	ctx    context.Context
	conn   *websocket.Conn
	connMu sync.RWMutex
	writeMu sync.Mutex // Serializes writes to WebSocket (required by gorilla/websocket)

	// Authentication
	creds           *auth.Credentials
	registrationMu  sync.Mutex
	heartbeatTicker *time.Ticker

	// JSON-RPC
	msgID           atomic.Int64
	pendingRequests sync.Map // map[int64]chan *JSONRPCResponse

	// Channels
	txChan chan []byte // Channel for receiving transactions from gateway

	// Reconnection state
	reconnectDelay    time.Duration
	maxReconnectDelay time.Duration
	reconnecting      atomic.Bool
}

func NewClient(url string, ctx context.Context, creds *auth.Credentials) *Client {
	return &Client{
		url:               url,
		ctx:               ctx,
		creds:             creds,
		txChan:            make(chan []byte, 100),
		reconnectDelay:    1 * time.Second,
		maxReconnectDelay: 60 * time.Second,
	}
}

// Connect establishes WebSocket connection with authentication
func (c *Client) Connect() error {
	// Ensure we have valid tokens
	if c.creds.AccessToken == "" {
		return fmt.Errorf("no access token available, registration required")
	}

	// Check if token is expired
	if time.Now().After(c.creds.TokenExpiry) {
		return fmt.Errorf("access token expired at %v", c.creds.TokenExpiry)
	}

	// Get WebSocket URL
	wsURL := auth.GetWebSocketURL(c.url, "")

	// Prepare headers with Bearer token and subprotocol
	header := http.Header{
		"Authorization":          []string{"Bearer " + c.creds.AccessToken},
		"Sec-WebSocket-Protocol": []string{"fastlane.sidecar.v1"},
	}

	log.Info("Connecting to gateway WebSocket", "url", wsURL)

	// Connect with authentication
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		if resp != nil {
			log.Error("WebSocket handshake failed", "status", resp.StatusCode)
		}
		log.Warn("Failed to connect to gateway, will retry", "url", wsURL, "error", err)
		go c.reconnectLoop()
		return nil
	}
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}

	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()

	log.Info("Connected to gateway WebSocket")

	// Send validator_register
	if err := c.sendValidatorRegister(); err != nil {
		log.Error("Failed to send validator_register", "error", err)
		conn.Close()
		go c.reconnectLoop()
		return nil
	}

	// Start message reader
	go c.readMessages()

	// Start heartbeat
	c.startHeartbeat()

	return nil
}

// sendValidatorRegister sends the validator_register JSON-RPC method
func (c *Client) sendValidatorRegister() error {
	params := map[string]interface{}{
		"sidecar_id":   c.creds.SidecarID,
		"capabilities": []string{"tx_publish", "auth_refresh_inband"},
	}

	resp, err := c.sendRequest("validator_register", params)
	if err != nil {
		return fmt.Errorf("failed to send validator_register: %w", err)
	}

	// Extract session_nonce from result if present
	if result, ok := resp.Result.(map[string]interface{}); ok {
		if nonce, ok := result["session_nonce"].(string); ok {
			c.creds.SessionNonce = nonce
			log.Debug("Received session nonce", "nonce", nonce)
		}
	}

	log.Info("Validator registered with gateway")
	return nil
}

// sendRequest sends a JSON-RPC request and waits for response
func (c *Client) sendRequest(method string, params interface{}) (*JSONRPCResponse, error) {
	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		return nil, fmt.Errorf("not connected to gateway")
	}

	id := c.msgID.Add(1)
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      id,
	}

	// Create response channel
	respChan := make(chan *JSONRPCResponse, 1)
	c.pendingRequests.Store(id, respChan)
	defer c.pendingRequests.Delete(id)

	// Serialize writes to WebSocket (required by gorilla/websocket)
	c.writeMu.Lock()
	err := conn.WriteJSON(req)
	c.writeMu.Unlock()

	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Wait for response with timeout
	select {
	case resp := <-respChan:
		if resp.Error != nil {
			return nil, fmt.Errorf("RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp, nil
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("request timeout")
	case <-c.ctx.Done():
		return nil, fmt.Errorf("context cancelled")
	}
}

// SendTransactionBytes publishes mempool transactions to the gateway
func (c *Client) SendTransactionBytes(txBytes []byte) error {
	// Convert to hex string with 0x prefix
	txHex := "0x" + hex.EncodeToString(txBytes)

	params := map[string]interface{}{
		"sidecar_id": c.creds.SidecarID,
		"txs":        []string{txHex},
	}

	_, err := c.sendRequest("validator_publish_mempool", params)
	if err != nil {
		log.Error("Failed to publish transaction to gateway", "error", err)
		return err
	}

	log.Debug("Published transaction to gateway", "bytes", len(txBytes))
	return nil
}

// NotifyTransactionDropped notifies gateway of dropped transaction
func (c *Client) NotifyTransactionDropped(txHash common.Hash) error {
	// For now, we don't have a specific JSON-RPC method for this
	// This could be extended later if the gateway adds support
	log.Debug("Transaction drop notification not yet supported via JSON-RPC", "hash", txHash.Hex())
	return nil
}

// readMessages reads and handles WebSocket messages
func (c *Client) readMessages() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			c.connMu.RLock()
			conn := c.conn
			c.connMu.RUnlock()

			if conn == nil {
				return
			}

			var msg json.RawMessage
			err := conn.ReadJSON(&msg)
			if err != nil {
				log.Error("Error reading from gateway", "error", err)
				c.connMu.Lock()
				c.conn = nil
				c.connMu.Unlock()
				// Trigger reconnection
				go c.reconnectLoop()
				return
			}

			// Try to parse as JSON-RPC response or notification
			if err := c.handleJSONRPCMessage(msg); err != nil {
				log.Error("Error handling gateway message", "error", err)
			}
		}
	}
}

// handleJSONRPCMessage handles JSON-RPC responses and notifications
func (c *Client) handleJSONRPCMessage(msg json.RawMessage) error {
	// Try to detect if it's a response (has "id") or notification (has "method")
	var peek struct {
		ID     *int64  `json:"id"`
		Method *string `json:"method"`
	}
	if err := json.Unmarshal(msg, &peek); err != nil {
		return fmt.Errorf("failed to peek message: %w", err)
	}

	// Handle JSON-RPC response
	if peek.ID != nil {
		var resp JSONRPCResponse
		if err := json.Unmarshal(msg, &resp); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}

		// Find pending request
		if ch, ok := c.pendingRequests.Load(*peek.ID); ok {
			respChan := ch.(chan *JSONRPCResponse)
			select {
			case respChan <- &resp:
			default:
				log.Warn("Response channel full, dropping response", "id", *peek.ID)
			}
		}
		return nil
	}

	// Handle JSON-RPC notification
	if peek.Method != nil {
		var notif JSONRPCNotification
		if err := json.Unmarshal(msg, &notif); err != nil {
			return fmt.Errorf("failed to unmarshal notification: %w", err)
		}

		return c.handleNotification(notif)
	}

	return fmt.Errorf("unknown message type")
}

// handleNotification handles JSON-RPC notifications from gateway
func (c *Client) handleNotification(notif JSONRPCNotification) error {
	switch notif.Method {
	case "mev_subscription":
		// Handle mempool subscription notifications
		return c.handleMempoolSubscription(notif.Params)

	case "auth_refresh_challenge":
		// Handle in-band token refresh challenge
		return c.handleRefreshChallenge(notif.Params)

	default:
		log.Debug("Unknown notification method", "method", notif.Method)
	}

	return nil
}

// handleMempoolSubscription handles mempool transaction notifications
func (c *Client) handleMempoolSubscription(params interface{}) error {
	paramsMap, ok := params.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid mempool subscription params")
	}

	result, ok := paramsMap["result"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid mempool subscription result")
	}

	// Extract transaction data
	data, ok := result["data"].([]interface{})
	if !ok {
		return nil // No transactions
	}

	for _, txData := range data {
		txHex, ok := txData.(string)
		if !ok {
			continue
		}

		// Remove 0x prefix if present
		if len(txHex) > 2 && txHex[:2] == "0x" {
			txHex = txHex[2:]
		}

		txBytes, err := hex.DecodeString(txHex)
		if err != nil {
			log.Error("Failed to decode transaction hex", "error", err)
			continue
		}

		select {
		case c.txChan <- txBytes:
			log.Info("Received transaction from gateway", "bytes", len(txBytes))
		default:
			log.Warn("Transaction channel full, dropping transaction from gateway")
		}
	}

	return nil
}

// handleRefreshChallenge handles in-band token refresh
func (c *Client) handleRefreshChallenge(params interface{}) error {
	paramsMap, ok := params.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid refresh challenge params")
	}

	challenge, ok := paramsMap["challenge"].(string)
	if !ok {
		return fmt.Errorf("missing challenge in refresh")
	}

	log.Info("Received in-band refresh challenge")

	// Create refresh PoP with session nonce
	popSignature, err := auth.CreateRefreshPoP(challenge, c.creds.RefreshToken, c.creds.SessionNonce, c.creds.SidecarKey)
	if err != nil {
		return fmt.Errorf("failed to create refresh PoP: %w", err)
	}

	// Send validator_refresh_auth
	refreshParams := map[string]interface{}{
		"refresh_token": c.creds.RefreshToken,
		"challenge":     challenge,
		"pop_signature": popSignature,
	}

	resp, err := c.sendRequest("validator_refresh_auth", refreshParams)
	if err != nil {
		return fmt.Errorf("failed to refresh auth: %w", err)
	}

	// Update credentials with new tokens
	if result, ok := resp.Result.(map[string]interface{}); ok {
		if accessToken, ok := result["access_token"].(string); ok && accessToken != "" {
			c.creds.AccessToken = accessToken
		}
		if refreshToken, ok := result["refresh_token"].(string); ok && refreshToken != "" {
			c.creds.RefreshToken = refreshToken
		}
		if expiresAt, ok := result["expires_at"].(string); ok && expiresAt != "" {
			if expiry, err := auth.ParseExpiryTime(expiresAt); err == nil {
				c.creds.TokenExpiry = expiry
			}
		}
	}

	log.Info("Successfully refreshed tokens in-band")
	return nil
}

// startHeartbeat sends periodic heartbeats
func (c *Client) startHeartbeat() {
	interval := 30 * time.Second // Default heartbeat interval

	c.heartbeatTicker = time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-c.heartbeatTicker.C:
				params := map[string]interface{}{
					"sidecar_id": c.creds.SidecarID,
				}
				if _, err := c.sendRequest("validator_heartbeat", params); err != nil {
					log.Debug("Failed to send heartbeat", "error", err)
				}
			case <-c.ctx.Done():
				c.heartbeatTicker.Stop()
				return
			}
		}
	}()
}

// reconnectLoop attempts to reconnect with exponential backoff
func (c *Client) reconnectLoop() {
	if c.reconnecting.Swap(true) {
		return // Already reconnecting
	}
	defer c.reconnecting.Store(false)

	delay := c.reconnectDelay

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-time.After(delay):
			if err := c.Connect(); err != nil {
				log.Debug("Gateway reconnection failed", "error", err, "retry_in", delay)
				// Exponential backoff
				delay *= 2
				if delay > c.maxReconnectDelay {
					delay = c.maxReconnectDelay
				}
				continue
			}

			log.Info("Reconnected to gateway")
			return
		}
	}
}

func (c *Client) GetTransactionChannel() <-chan []byte {
	return c.txChan
}

func (c *Client) Close() error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.heartbeatTicker != nil {
		c.heartbeatTicker.Stop()
	}

	if c.conn != nil {
		err := c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		if err != nil {
			log.Warn("Error sending close message", "error", err)
		}
		err = c.conn.Close()
		c.conn = nil
		close(c.txChan)
		return err
	}

	close(c.txChan)
	return nil
}
