package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/internal/auth"
	"github.com/FastLane-Labs/fastlane-sidecar/internal/gateway"
	"github.com/FastLane-Labs/fastlane-sidecar/internal/ipc"
	"github.com/FastLane-Labs/fastlane-sidecar/internal/pool"
	"github.com/FastLane-Labs/fastlane-sidecar/internal/priorities"
	"github.com/FastLane-Labs/fastlane-sidecar/internal/processor"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/config"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/types"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

type Sidecar struct {
	config       *config.Config
	shutdownChan chan struct{}

	// Statistics
	txReceived    atomic.Uint64
	bytesTotal    atomic.Uint64
	txStreamed    atomic.Uint64 // Number of transactions streamed to node with priority
	lastHeartbeat atomic.Int64  // Unix timestamp in nanoseconds from node
	poolSize      atomic.Uint64 // Current transaction pool size

	// Components
	nodeListener  *ipc.NodeListener
	nodeSender    *ipc.NodeSender
	gatewayClient *gateway.Client
	txPool        *pool.TransactionPool
	filter        *processor.Filter

	// Authentication
	credentials *auth.Credentials // Loaded once in constructor, reused for registration

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

func NewSidecar(config *config.Config, shutdownChan chan struct{}) (*Sidecar, error) {
	ctx, cancel := context.WithCancel(context.Background())

	filter, err := processor.NewFilter(config.FastlaneContract)
	if err != nil {
		cancel()
		return nil, err
	}

	// Load authentication credentials if gateway is enabled and credentials are provided
	var credentials *auth.Credentials
	if !config.DisableGateway && config.DelegationPath != "" && config.KeystorePath != "" {
		log.Info("Loading authentication credentials")

		// Load delegation envelope
		envelope, err := auth.LoadDelegationEnvelope(config.DelegationPath)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to load delegation envelope: %w", err)
		}

		// Load sidecar key
		sidecarKey, err := auth.LoadSidecarKey(config.KeystorePath, config.KeystorePass)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to load sidecar key: %w", err)
		}

		// Validate that sidecar public key matches the delegation
		if err := auth.ValidateSidecarPubkeyMatch(envelope, sidecarKey); err != nil {
			cancel()
			return nil, fmt.Errorf("sidecar key validation failed: %w", err)
		}

		credentials = &auth.Credentials{
			SidecarKey:         sidecarKey,
			DelegationEnvelope: envelope,
		}

		log.Info("Credentials loaded and validated successfully",
			"validator_pubkey", envelope.Delegation.ValidatorPubkey,
			"sidecar_pubkey", envelope.Delegation.SidecarPubkey)
	} else if !config.DisableGateway {
		log.Warn("No authentication credentials provided, gateway connection will use unauthenticated mode (if supported)")
	} else {
		log.Info("Gateway connection disabled")
	}

	s := &Sidecar{
		config:        config,
		shutdownChan:  shutdownChan,
		nodeListener:  ipc.NewNodeListener(ctx, config.NodeToSidecarSocketPath),
		nodeSender:    ipc.NewNodeSender(ctx, config.SidecarToNodeSocketPath),
		gatewayClient: nil, // Will be created in Start() after registration
		txPool:        pool.NewTransactionPool(config.PoolMaxDuration),
		filter:        filter,
		credentials:   credentials,
		ctx:           ctx,
		cancel:        cancel,
	}

	return s, nil
}

func (s *Sidecar) Start() error {
	// Start node listener
	if err := s.nodeListener.Start(); err != nil {
		return err
	}

	// Connect to node sender
	if err := s.nodeSender.Connect(); err != nil {
		return err
	}

	// Register with gateway if not disabled and credentials are available
	if !s.config.DisableGateway && s.credentials != nil {
		if err := s.registerWithGateway(); err != nil {
			// Check if error is recoverable (e.g., waiting for whitelist, network issues)
			if isRetryableError(err) {
				log.Warn("Gateway registration failed with retryable error, will retry in background",
					"error", err)
				// Start background retry goroutine
				go s.retryGatewayRegistration()
			} else {
				log.Error("Gateway registration failed with non-retryable error",
					"error", err,
					"help", "Check delegation signature, validator whitelist status, or gateway connectivity")
				// Continue without gateway - this is likely a fatal auth error
			}
		}
	}

	// Start processing goroutines
	go s.processNodeTransactions()
	go s.processGatewayTransactions()
	go s.cleanupOldTransactions()

	return nil
}

func (s *Sidecar) Stop() {
	s.cancel()
	if s.nodeListener != nil {
		s.nodeListener.Stop()
	}
	if s.nodeSender != nil {
		s.nodeSender.Close()
	}
	if s.gatewayClient != nil {
		s.gatewayClient.Close()
	}
}

// registerWithGateway performs the initial gateway registration
func (s *Sidecar) registerWithGateway() error {
	log.Info("Registering with MEV gateway")

	// Create registration client
	regClient := auth.NewRegistrationClient(s.config.GatewayURL)

	// Perform registration using credentials loaded in constructor
	registerResp, err := regClient.Register(s.ctx, s.credentials)
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	// Update credentials with tokens
	s.credentials.SidecarID = registerResp.SidecarID
	s.credentials.AccessToken = registerResp.AccessToken
	s.credentials.RefreshToken = registerResp.RefreshToken

	expiry, err := auth.ParseExpiryTime(registerResp.ExpiresAt)
	if err != nil {
		log.Warn("Failed to parse token expiry", "error", err)
		expiry = time.Now().Add(10 * time.Minute)
	}
	s.credentials.TokenExpiry = expiry

	log.Info("Successfully registered with gateway",
		"sidecar_id", registerResp.SidecarID,
		"expires_at", registerResp.ExpiresAt)

	// Create gateway client with authenticated credentials and health provider
	s.gatewayClient = gateway.NewClient(s.config.GatewayURL, s.ctx, s.credentials, s)

	// Connect to gateway WebSocket
	if err := s.gatewayClient.Connect(); err != nil {
		return fmt.Errorf("WebSocket connection failed: %w", err)
	}

	return nil
}

// GetHealthStats returns current health statistics for the sidecar
func (s *Sidecar) GetHealthStats() gateway.HealthStats {
	lastHeartbeatNanos := s.lastHeartbeat.Load()
	var lastHeartbeat time.Time
	if lastHeartbeatNanos > 0 {
		lastHeartbeat = time.Unix(0, lastHeartbeatNanos)
	}

	return gateway.HealthStats{
		LastHeartbeat: lastHeartbeat,
		TxReceived:    s.txReceived.Load(),
		TxStreamed:    s.txStreamed.Load(),
		PoolSize:      s.poolSize.Load(),
	}
}

// retryGatewayRegistration retries registration in the background with exponential backoff
func (s *Sidecar) retryGatewayRegistration() {
	backoff := 30 * time.Second
	maxBackoff := 5 * time.Minute

	for {
		select {
		case <-s.ctx.Done():
			log.Info("Gateway registration retry stopped")
			return
		case <-time.After(backoff):
			log.Info("Retrying gateway registration", "backoff", backoff)

			if err := s.registerWithGateway(); err != nil {
				if !isRetryableError(err) {
					log.Error("Gateway registration retry failed with non-retryable error, stopping retries",
						"error", err)
					return
				}

				log.Warn("Gateway registration retry failed, will retry again",
					"error", err,
					"next_retry_in", backoff)

				// Exponential backoff
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			} else {
				log.Info("Gateway registration retry succeeded")
				return
			}
		}
	}
}

// isRetryableError determines if a registration error is retryable
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Network/connectivity errors - retryable
	if strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "network") ||
		strings.Contains(errStr, "EOF") {
		return true
	}

	// HTTP status errors that might be retryable
	// 401 Unauthorized - might be whitelist pending
	// 403 Forbidden - might be whitelist pending
	// 503 Service Unavailable - temporary gateway issue
	// 429 Too Many Requests - rate limiting
	if strings.Contains(errStr, "status 401") ||
		strings.Contains(errStr, "status 403") ||
		strings.Contains(errStr, "status 503") ||
		strings.Contains(errStr, "status 429") {
		return true
	}

	// Non-retryable errors:
	// 400 Bad Request - invalid delegation format
	// Signature verification failures - wrong keys
	// Other 4xx errors - client errors
	return false
}

// processNodeTransactions handles transactions from the node
func (s *Sidecar) processNodeTransactions() {
	defer close(s.shutdownChan)

	txChan := s.nodeListener.GetTransactionChannel()

	for {
		select {
		case <-s.ctx.Done():
			log.Info("Node transaction processing stopped")
			return
		case msgBytes := <-txChan:
			s.txReceived.Add(1)
			s.handleIncomingMessage(msgBytes, "node")
		}
	}
}

// processGatewayTransactions handles transactions from the gateway
func (s *Sidecar) processGatewayTransactions() {
	if s.gatewayClient == nil {
		log.Info("Gateway disabled, not processing gateway transactions")
		return
	}

	gatewayTxChan := s.gatewayClient.GetTransactionChannel()

	for {
		select {
		case <-s.ctx.Done():
			log.Info("Gateway transaction processing stopped")
			return
		case msgBytes := <-gatewayTxChan:
			s.txReceived.Add(1)
			s.handleIncomingMessage(msgBytes, "gateway")
		}
	}
}

// handleIncomingMessage handles FastlaneMessage from node
func (s *Sidecar) handleIncomingMessage(msgBytes []byte, source string) {
	// Parse the FastlaneMessage enum
	msgType, data := types.ParseFastlaneMessage(msgBytes)

	switch msgType {
	case "TxAdded":
		log.Info("Received TxAdded message", "bytes", len(data), "source", source)
		s.handleIncomingTransaction(data, source)

	case "TxDropped":
		log.Info("Received TxDropped message", "hash", common.BytesToHash(data[:32]).Hex(), "source", source)
		s.handleTransactionDropped(data[:32])

	case "Heartbeat":
		log.Debug("Received Heartbeat message", "source", source)
		s.lastHeartbeat.Store(time.Now().UnixNano())

	default:
		log.Info("Unknown message type, treating as raw tx bytes", "source", source)
		// Fallback to old behavior for compatibility
		s.handleIncomingTransaction(msgBytes, source)
	}
}

// handleIncomingTransaction implements the new transaction lifecycle
func (s *Sidecar) handleIncomingTransaction(txBytes []byte, source string) {
	// The txBytes are raw transaction bytes from TxAdded message

	// Decode transaction
	var tx ethTypes.Transaction
	if err := rlp.DecodeBytes(txBytes, &tx); err != nil {
		log.Error("Failed to decode transaction", "error", err, "source", source)
		return
	}

	// Add to transaction pool first
	if err := s.txPool.AddTransaction(txBytes, source); err != nil {
		log.Error("Failed to add transaction to pool", "error", err)
		return
	}

	// Check if this is a fastlane bid
	txType, bidData := s.filter.ClassifyTransaction(&tx)
	hash := tx.Hash()

	// Update transaction type in pool
	s.txPool.UpdateTransactionType(hash, txType)
	if pooledTx := s.txPool.GetTransaction(hash); pooledTx != nil {
		pooledTx.BidData = bidData
	}

	switch txType {
	case types.TOBBid:
		s.handleTOBBid(txBytes, bidData)
	case types.BackrunBid:
		s.handleBackrunBid(txBytes, hash, bidData)
	case types.NormalTransaction:
		// Normal transaction - just keep in pool and forward to gateway if from node
		if source == "node" {
			s.forwardToGateway(txBytes)
		}
	}
}

// handleTransactionDropped handles TxDropped messages
func (s *Sidecar) handleTransactionDropped(txHashBytes []byte) {
	if len(txHashBytes) < 32 {
		log.Error("Invalid transaction hash in TxDropped message")
		return
	}

	txHash := common.BytesToHash(txHashBytes[:32])

	// Remove from pool if it exists
	removedTx := s.txPool.RemoveTransaction(txHash)
	if removedTx != nil {
		log.Info("Transaction dropped by node and removed from pool", "hash", txHash.Hex(), "source", removedTx.Source)

		// Notify gateway about the dropped transaction if it didn't come from gateway
		if s.gatewayClient != nil && removedTx.Source != "gateway" {
			if err := s.gatewayClient.NotifyTransactionDropped(txHash); err != nil {
				log.Error("Failed to notify gateway of dropped transaction", "error", err, "hash", txHash.Hex())
			}
		}
	} else {
		log.Info("Transaction dropped by node but not found in pool", "hash", txHash.Hex())
	}
}

// handleTOBBid processes TOB bid - compute priority and stream immediately
func (s *Sidecar) handleTOBBid(txBytes []byte, bidData *types.BidData) {
	if bidData == nil || bidData.BidAmount == nil {
		log.Error("TOB bid missing bid data")
		return
	}

	// Compute priority
	priority := priorities.CalculateTOBPriority(bidData.BidAmount)

	// Stream immediately to node
	txWithPriority := types.TxWithPriority{
		TxBytes:  txBytes,
		Priority: priority,
	}
	s.streamTransaction(txWithPriority)

	log.Info("Processed TOB bid", "bid_amount", bidData.BidAmount.String(), "priority", priorities.FormatPriority(priority))
}

// handleBackrunBid processes backrun bid - look for opportunity and stream both if found
func (s *Sidecar) handleBackrunBid(txBytes []byte, bidHash common.Hash, bidData *types.BidData) {
	if bidData == nil || bidData.BidAmount == nil || bidData.TargetTxHash == nil {
		log.Error("Backrun bid missing bid data")
		return
	}

	targetTxHash := *bidData.TargetTxHash
	targetTx := s.txPool.GetTransaction(targetTxHash)

	if targetTx == nil {
		log.Info("Target transaction not found for backrun bid", "bid_hash", bidHash.Hex(), "target_hash", targetTxHash.Hex())
		return
	}

	// Found target transaction - compute priorities and stream both
	oppGasTip := targetTx.Tx.GasTipCap()

	// Stream opportunity transaction first
	oppPriority := priorities.CalculateOpportunityPriority(oppGasTip)
	oppTxWithPriority := types.TxWithPriority{
		TxBytes:  targetTx.TxBytes,
		Priority: oppPriority,
	}
	s.streamTransaction(oppTxWithPriority)

	// Stream backrun bid
	backrunPriority := priorities.CalculateBackrunPriority(bidData.BidAmount, oppGasTip)
	backrunTxWithPriority := types.TxWithPriority{
		TxBytes:  txBytes,
		Priority: backrunPriority,
	}
	s.streamTransaction(backrunTxWithPriority)

	log.Info("Processed backrun pair immediately", "bid_hash", bidHash.Hex(), "target_hash", targetTxHash.Hex(), "bid_amount", bidData.BidAmount.String(), "opp_gas_tip", oppGasTip.String())
}

// streamTransaction sends a transaction with priority to the node
func (s *Sidecar) streamTransaction(txWithPriority types.TxWithPriority) {
	if err := s.nodeSender.SendTxWithPriority(txWithPriority); err != nil {
		log.Error("Failed to send transaction to node", "error", err)
		return
	}
	s.txStreamed.Add(1)
}

// forwardToGateway sends transaction to gateway (if it didn't come from there)
func (s *Sidecar) forwardToGateway(txBytes []byte) {
	if s.gatewayClient == nil {
		return // Gateway disabled
	}
	if err := s.gatewayClient.SendTransactionBytes(txBytes); err != nil {
		log.Error("Failed to send transaction to gateway", "error", err)
	}
}

// cleanupOldTransactions periodically removes old transactions
func (s *Sidecar) cleanupOldTransactions() {
	// Use the pool refresh duration for cleanup frequency
	ticker := time.NewTicker(s.config.PoolMaxDuration / 4) // Cleanup 4x more frequently than max duration
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			log.Info("Cleanup stopped")
			return
		case <-ticker.C:
			s.txPool.CleanupOldTransactions()
			// Update pool size metric
			s.poolSize.Store(s.txPool.Size())
		}
	}
}
