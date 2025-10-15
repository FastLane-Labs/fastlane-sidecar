package monadgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/internal/auth"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/config"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/websocket"
)

// HealthStats represents health information for the gateway connection
type HealthStats struct {
	Connected     bool   `json:"connected"`
	Authenticated bool   `json:"authenticated"`
	LastError     string `json:"last_error,omitempty"`
}

// Client represents a connection to the Monad MEV Gateway
type Client struct {
	config *config.Config
	creds  *auth.Credentials

	// Registration client for token refresh
	regClient *auth.RegistrationClient

	// WebSocket connection
	conn    *websocket.Conn
	connMu  sync.RWMutex
	writeMu sync.Mutex

	// Channels
	txChan chan []byte // Transactions from gateway

	// State
	authenticated atomic.Bool
	lastError     atomic.Value // stores string
	connected     atomic.Bool

	// Context for lifecycle management
	ctx    context.Context
	cancel context.CancelFunc

	// JSON-RPC
	msgID           atomic.Int64
	pendingRequests sync.Map // map[int64]chan *jsonRPCResponse
}

// jsonRPCRequest represents a JSON-RPC 2.0 request
type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      int64       `json:"id"`
}

// jsonRPCResponse represents a JSON-RPC 2.0 response
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
	ID      int64           `json:"id"`
}

// jsonRPCError represents a JSON-RPC 2.0 error
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// jsonRPCNotification represents a JSON-RPC 2.0 notification (no ID)
type jsonRPCNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// NewMonadGatewayClient creates a new gateway client
// Loads credentials from disk but does NOT perform HTTP registration or WebSocket connection
// Call Start() to register and connect
// Returns nil if gateway is disabled (both ingress and egress disabled)
// Returns nil if credentials are not provided (no delegation or keystore paths)
func NewMonadGatewayClient(cfg *config.Config) (*Client, error) {
	// Check if gateway is disabled
	if cfg.DisableGatewayIngress && cfg.DisableGatewayEgress {
		log.Info("Gateway connection disabled (ingress and egress both disabled)")
		return nil, nil
	}

	// Check if credentials are provided
	if cfg.DelegationPath == "" || cfg.KeystorePath == "" {
		log.Warn("Gateway enabled but no credentials provided - gateway will not be used")
		return nil, nil
	}

	log.Info("Loading authentication credentials")

	// Load delegation envelope
	envelope, err := auth.LoadDelegationEnvelope(cfg.DelegationPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load delegation envelope: %w", err)
	}

	// Load sidecar key
	sidecarKey, err := auth.LoadSidecarKey(cfg.KeystorePath, cfg.KeystorePass)
	if err != nil {
		return nil, fmt.Errorf("failed to load sidecar key: %w", err)
	}

	// Validate that sidecar public key matches the delegation
	if err := auth.ValidateSidecarPubkeyMatch(envelope, sidecarKey); err != nil {
		return nil, fmt.Errorf("sidecar key validation failed: %w", err)
	}

	creds := &auth.Credentials{
		SidecarKey:         sidecarKey,
		DelegationEnvelope: envelope,
	}

	log.Info("Credentials loaded and validated successfully",
		"validator_pubkey", envelope.Delegation.ValidatorPubkey,
		"sidecar_pubkey", envelope.Delegation.SidecarPubkey)

	// Create registration client (will be used in Start())
	regClient := auth.NewRegistrationClient(cfg.GatewayURL)

	// Create client with default buffer size (will be updated after registration)
	client := &Client{
		config:    cfg,
		creds:     creds,
		regClient: regClient,
		txChan:    make(chan []byte, 100), // Default buffer, updated after registration
	}

	return client, nil
}

// Start begins the WebSocket connection with automatic reconnection
// This starts background goroutines that will run until Stop() is called
func (c *Client) Start() error {
	// Create cancellable context for this connection lifecycle
	c.ctx, c.cancel = context.WithCancel(context.Background())

	// Start connection loop with reconnection
	go c.connectionLoop()

	return nil
}

// Stop closes the gateway connection and stops all goroutines
func (c *Client) Stop() error {
	if c.cancel != nil {
		c.cancel()
	}

	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn != nil {
		err := c.conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		if err != nil {
			log.Warn("Error sending close message", "error", err)
		}
		c.conn.Close()
		c.conn = nil
	}

	return nil
}

// GetTransactionChannel returns the channel for receiving transactions from gateway
func (c *Client) GetTransactionChannel() <-chan []byte {
	return c.txChan
}

// SendToGateway sends a transaction to the gateway
func (c *Client) SendToGateway(txBytes []byte) error {
	if c.config.DisableGatewayEgress {
		return nil // Egress disabled, silently ignore
	}

	if !c.authenticated.Load() {
		return fmt.Errorf("not authenticated with gateway")
	}

	// Encode transaction as hex
	txHex := "0x" + fmt.Sprintf("%x", txBytes)

	params := map[string]interface{}{
		"tx": txHex,
	}

	_, err := c.sendRequest("validator_publish_tx", params)
	return err
}

// NotifyTransactionDropped notifies the gateway that a transaction was dropped
func (c *Client) NotifyTransactionDropped(txHash common.Hash) error {
	if c.config.DisableGatewayEgress {
		return nil // Egress disabled, silently ignore
	}

	if !c.authenticated.Load() {
		return fmt.Errorf("not authenticated with gateway")
	}

	params := map[string]interface{}{
		"tx_hash": txHash.Hex(),
	}

	_, err := c.sendRequest("validator_tx_dropped", params)
	return err
}

// Health returns current health statistics
func (c *Client) Health() HealthStats {
	stats := HealthStats{
		Connected:     c.connected.Load(),
		Authenticated: c.authenticated.Load(),
	}

	if err := c.getLastError(); err != "" {
		stats.LastError = err
	}

	return stats
}

// connectionLoop manages the WebSocket connection with automatic reconnection
func (c *Client) connectionLoop() {
	backoff := 1 * time.Second
	maxBackoff := 60 * time.Second

	for {
		select {
		case <-c.ctx.Done():
			log.Info("Connection loop stopped")
			return
		default:
		}

		// Check if token is expired and refresh if needed
		if time.Now().After(c.creds.TokenExpiry) {
			log.Info("Access token expired, refreshing before connect")
			if err := c.refreshTokens(); err != nil {
				log.Warn("Token refresh failed", "error", err, "retry_in", backoff)
				c.setLastError(err)
				time.Sleep(backoff)
				backoff = min(backoff*2, maxBackoff)
				continue
			}
			// Reset backoff on successful refresh
			backoff = 1 * time.Second
		}

		// Attempt to connect (also starts readLoop)
		connCtx, connCancel, err := c.connect()
		if err != nil {
			c.setLastError(err)
			time.Sleep(backoff)
			backoff = min(backoff*2, maxBackoff)
			log.Warn("Connection failed", "error", err, "retry_in", backoff)
			continue
		}

		// Connection successful, reset backoff
		backoff = 1 * time.Second
		log.Info("Connected to gateway")

		// Start goroutines for this connection
		var wg sync.WaitGroup

		// Heartbeat goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.heartbeatLoop(connCtx)
		}()

		// Token refresh goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.tokenRefreshLoop(connCtx)
		}()

		// Wait for any goroutine to exit (indicates disconnection)
		// Note: readLoop is already running from connect()
		wg.Wait()
		connCancel() // Ensure readLoop is stopped

		log.Info("Disconnected from gateway, will reconnect")
		c.connected.Store(false)
		c.authenticated.Store(false)

		// Close the connection
		c.connMu.Lock()
		if c.conn != nil {
			c.conn.Close()
			c.conn = nil
		}
		c.connMu.Unlock()
	}
}

// connect establishes a WebSocket connection and authenticates
// Performs HTTP registration if needed, then connects via WebSocket
// Returns a context for this connection and a cancel function
func (c *Client) connect() (context.Context, context.CancelFunc, error) {
	// Perform HTTP registration if we don't have tokens yet
	if c.creds.AccessToken == "" {
		log.Info("Registering with MEV gateway")

		ctx := context.Background()
		registerResp, err := c.regClient.Register(ctx, c.creds)
		if err != nil {
			return nil, nil, fmt.Errorf("registration failed: %w", err)
		}

		// Update credentials with tokens and connection metadata
		c.creds.SidecarID = registerResp.SidecarID
		c.creds.AccessToken = registerResp.AccessToken
		c.creds.RefreshToken = registerResp.RefreshToken
		c.creds.WssURL = registerResp.WssURL
		c.creds.WsSubprotocol = registerResp.WsSubprotocol

		if registerResp.HeartbeatInterval > 0 {
			c.creds.HeartbeatInterval = time.Duration(registerResp.HeartbeatInterval) * time.Second
		}
		if registerResp.MaxInflight > 0 {
			c.creds.MaxInflight = registerResp.MaxInflight
		}

		expiry, err := auth.ParseExpiryTime(registerResp.ExpiresAt)
		if err != nil {
			log.Warn("Failed to parse token expiry", "error", err)
			expiry = time.Now().Add(15 * time.Minute)
		}
		c.creds.TokenExpiry = expiry

		log.Info("Successfully registered with gateway",
			"sidecar_id", registerResp.SidecarID,
			"expires_at", registerResp.ExpiresAt)
	}

	// Check if token is expired (might need refresh)
	if time.Now().After(c.creds.TokenExpiry) {
		return nil, nil, fmt.Errorf("access token expired at %v", c.creds.TokenExpiry)
	}

	// Get WebSocket URL
	wsURL := auth.GetWebSocketURL(c.config.GatewayURL, c.creds.WssURL)

	// Prepare headers
	protocol := c.creds.WsSubprotocol
	if protocol == "" {
		protocol = "fastlane.sidecar.v1"
	}

	header := http.Header{
		"Authorization": []string{"Bearer " + c.creds.AccessToken},
	}
	if protocol != "" {
		header["Sec-WebSocket-Protocol"] = []string{protocol}
	}

	// Dial WebSocket
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		if resp != nil {
			return nil, nil, fmt.Errorf("WebSocket handshake failed: status %d: %w", resp.StatusCode, err)
		}
		return nil, nil, fmt.Errorf("WebSocket dial failed: %w", err)
	}
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}

	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()

	c.connected.Store(true)

	// Set up ping/pong handlers
	conn.SetPongHandler(func(appData string) error {
		log.Debug("Received pong from gateway")
		return nil
	})

	conn.SetPingHandler(func(appData string) error {
		log.Debug("Received ping from gateway, sending pong")
		c.writeMu.Lock()
		err := conn.WriteMessage(websocket.PongMessage, []byte(appData))
		c.writeMu.Unlock()
		return err
	})

	// Create connection-specific context
	connCtx, connCancel := context.WithCancel(c.ctx)

	// Start read loop BEFORE sending validator_register so responses can be received
	go c.readLoop(connCtx)

	// Send validator_register
	if err := c.sendValidatorRegister(); err != nil {
		connCancel() // Cancel context to stop read loop
		return nil, nil, fmt.Errorf("validator_register failed: %w", err)
	}

	c.authenticated.Store(true)
	c.setLastError(nil)

	log.Info("Authenticated with gateway")
	return connCtx, connCancel, nil
}

// sendValidatorRegister sends the validator_register JSON-RPC method
func (c *Client) sendValidatorRegister() error {
	params := map[string]interface{}{
		"sidecar_id":   c.creds.SidecarID,
		"capabilities": []string{"tx_publish", "auth_refresh_inband"},
	}

	resp, err := c.sendRequest("validator_register", params)
	if err != nil {
		return err
	}

	// Extract session_nonce from result if present
	var result map[string]interface{}
	if err := json.Unmarshal(resp.Result, &result); err == nil {
		if nonce, ok := result["session_nonce"].(string); ok {
			c.creds.SessionNonce = nonce
			log.Debug("Received session nonce", "nonce", nonce)
		}
	}

	return nil
}

// refreshTokens refreshes the access token via HTTP
func (c *Client) refreshTokens() error {
	refreshResp, err := c.regClient.RefreshTokens(c.ctx, c.creds)
	if err != nil {
		return fmt.Errorf("token refresh failed: %w", err)
	}

	c.creds.AccessToken = refreshResp.AccessToken
	c.creds.RefreshToken = refreshResp.RefreshToken

	if expiry, err := auth.ParseExpiryTime(refreshResp.ExpiresAt); err == nil {
		c.creds.TokenExpiry = expiry
	}

	log.Info("Successfully refreshed tokens", "expires_at", refreshResp.ExpiresAt)
	return nil
}

// Helper functions
func (c *Client) setLastError(err error) {
	if err == nil {
		c.lastError.Store("")
	} else {
		c.lastError.Store(err.Error())
	}
}

func (c *Client) getLastError() string {
	if val := c.lastError.Load(); val != nil {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
