package monadgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/internal/auth"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
)

// readLoop reads messages from the WebSocket connection
func (c *Client) readLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Debug("Read loop stopped")
			return
		default:
		}

		c.connMu.RLock()
		conn := c.conn
		c.connMu.RUnlock()

		if conn == nil {
			log.Error("Connection is nil in read loop")
			return
		}

		var msg json.RawMessage
		if err := conn.ReadJSON(&msg); err != nil {
			log.Error("Error reading from gateway", "error", err)
			c.setLastError(err)
			return
		}

		// Handle message in separate goroutine to not block reading
		go c.handleMessage(msg)
	}
}

// handleMessage processes a JSON-RPC message (response or notification)
func (c *Client) handleMessage(msg json.RawMessage) {
	// Try to detect if it's a response or notification
	var peek struct {
		ID     *int64  `json:"id"`
		Method *string `json:"method"`
	}

	if err := json.Unmarshal(msg, &peek); err != nil {
		log.Error("Failed to parse message", "error", err)
		return
	}

	// Response (has ID)
	if peek.ID != nil {
		var resp jsonRPCResponse
		if err := json.Unmarshal(msg, &resp); err != nil {
			log.Error("Failed to parse JSON-RPC response", "error", err)
			return
		}

		// Find pending request
		if ch, ok := c.pendingRequests.LoadAndDelete(resp.ID); ok {
			respChan := ch.(chan *jsonRPCResponse)
			select {
			case respChan <- &resp:
			default:
				log.Warn("Response channel full or closed", "id", resp.ID)
			}
		} else {
			log.Warn("Received response for unknown request", "id", resp.ID)
		}
		return
	}

	// Notification (has method)
	if peek.Method != nil {
		var notif jsonRPCNotification
		if err := json.Unmarshal(msg, &notif); err != nil {
			log.Error("Failed to parse JSON-RPC notification", "error", err)
			return
		}

		if err := c.handleNotification(notif); err != nil {
			log.Error("Error handling notification", "method", notif.Method, "error", err)
		}
		return
	}

	log.Warn("Received message that is neither response nor notification")
}

// handleNotification handles JSON-RPC notifications from gateway
func (c *Client) handleNotification(notif jsonRPCNotification) error {
	log.Debug("Received notification", "method", notif.Method)

	switch notif.Method {
	case "validator_bundle_notification":
		return c.handleBundleNotification(notif.Params)

	case "validator_auth_expiring":
		return c.handleAuthExpiring(notif.Params)

	case "validator_rate_limited":
		return c.handleRateLimited(notif.Params)

	default:
		log.Debug("Unknown notification method", "method", notif.Method)
	}

	return nil
}

// handleBundleNotification processes bundle notifications and sends txs to channel
func (c *Client) handleBundleNotification(params json.RawMessage) error {
	if c.config.DisableGatewayIngress {
		return nil // Ingress disabled, ignore
	}

	var paramsMap map[string]interface{}
	if err := json.Unmarshal(params, &paramsMap); err != nil {
		return fmt.Errorf("invalid bundle notification params: %w", err)
	}

	// Extract bundles array
	bundlesInterface, ok := paramsMap["bundles"].([]interface{})
	if !ok {
		return fmt.Errorf("bundle notification missing bundles field")
	}

	// Process each bundle
	for _, bundleInterface := range bundlesInterface {
		bundle, ok := bundleInterface.(map[string]interface{})
		if !ok {
			log.Warn("Invalid bundle format")
			continue
		}

		bundleID, _ := bundle["bundle_id"].(string)

		// Extract transactions from bundle
		txsInterface, ok := bundle["txs"].([]interface{})
		if !ok {
			log.Warn("Bundle missing txs field", "bundle_id", bundleID)
			continue
		}

		// Process each transaction
		for _, txInterface := range txsInterface {
			txMap, ok := txInterface.(map[string]interface{})
			if !ok {
				log.Warn("Invalid transaction format in bundle", "bundle_id", bundleID)
				continue
			}

			txHex, ok := txMap["raw_tx_hex"].(string)
			if !ok {
				log.Warn("Transaction missing raw_tx_hex", "bundle_id", bundleID)
				continue
			}

			// Decode hex transaction
			txBytes, err := decodeHex(txHex)
			if err != nil {
				log.Warn("Failed to decode transaction hex", "bundle_id", bundleID, "error", err)
				continue
			}

			// Send to channel (non-blocking)
			select {
			case c.txChan <- txBytes:
				log.Debug("Sent transaction to channel", "bundle_id", bundleID, "bytes", len(txBytes))
			default:
				log.Warn("Transaction channel full, dropping transaction", "bundle_id", bundleID)
			}
		}
	}

	return nil
}

// handleAuthExpiring handles token expiry warnings and performs in-band refresh
func (c *Client) handleAuthExpiring(params json.RawMessage) error {
	var paramsMap map[string]interface{}
	if err := json.Unmarshal(params, &paramsMap); err != nil {
		return fmt.Errorf("invalid auth expiring params: %w", err)
	}

	expiresAt, _ := paramsMap["expires_at"].(string)
	log.Info("Received auth expiring notification", "expires_at", expiresAt)

	// Check if challenge is provided for in-band refresh
	challenge, hasChallenge := paramsMap["challenge"].(string)
	if !hasChallenge {
		log.Debug("No challenge provided, skipping in-band refresh")
		return nil
	}

	log.Info("Performing in-band token refresh")

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
	var result map[string]interface{}
	if err := json.Unmarshal(resp.Result, &result); err == nil {
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

// handleRateLimited handles rate limit notifications
func (c *Client) handleRateLimited(params json.RawMessage) error {
	var paramsMap map[string]interface{}
	if err := json.Unmarshal(params, &paramsMap); err != nil {
		// Not critical, just log
		log.Warn("Invalid rate limit params", "error", err)
		return nil
	}

	retryAfter, _ := paramsMap["retry_after_ms"].(float64)
	reason, _ := paramsMap["reason"].(string)

	if reason != "" {
		log.Warn("Gateway rate limit signal received", "reason", reason, "retry_after_ms", retryAfter)
	} else {
		log.Warn("Gateway rate limit signal received", "retry_after_ms", retryAfter)
	}

	return nil
}

// heartbeatLoop sends periodic heartbeats
func (c *Client) heartbeatLoop(ctx context.Context) {
	interval := 30 * time.Second
	if c.creds.HeartbeatInterval > 0 {
		interval = c.creds.HeartbeatInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Debug("Heartbeat loop stopped")
			return
		case <-ticker.C:
			params := map[string]interface{}{
				"sidecar_id": c.creds.SidecarID,
			}

			if _, err := c.sendRequest("validator_heartbeat", params); err != nil {
				log.Warn("Failed to send heartbeat", "error", err)
				// Don't return - let read loop detect the error
			}
		}
	}
}

// tokenRefreshLoop proactively refreshes tokens at 80% of token lifetime
func (c *Client) tokenRefreshLoop(ctx context.Context) {
	for {
		// Calculate time until refresh (80% of token lifetime)
		timeUntilRefresh := time.Until(c.creds.TokenExpiry) * 8 / 10
		if timeUntilRefresh < time.Minute {
			timeUntilRefresh = time.Minute
		}

		select {
		case <-ctx.Done():
			log.Debug("Token refresh loop stopped")
			return
		case <-time.After(timeUntilRefresh):
			log.Info("Proactively refreshing tokens", "current_expiry", c.creds.TokenExpiry)

			if err := c.refreshTokens(); err != nil {
				log.Warn("HTTP token refresh failed", "error", err)
				// Continue - will try again next cycle or rely on in-band refresh
			}
		}
	}
}

// Helper function to decode hex strings
func decodeHex(hexStr string) ([]byte, error) {
	// Remove 0x prefix if present
	if len(hexStr) >= 2 && hexStr[0:2] == "0x" {
		hexStr = hexStr[2:]
	}

	bytes := make([]byte, len(hexStr)/2)
	for i := 0; i < len(bytes); i++ {
		_, err := fmt.Sscanf(hexStr[i*2:i*2+2], "%02x", &bytes[i])
		if err != nil {
			return nil, err
		}
	}

	return bytes, nil
}
