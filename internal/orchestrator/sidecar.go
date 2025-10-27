package orchestrator

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/internal/health"
	"github.com/FastLane-Labs/fastlane-sidecar/internal/ipc"
	"github.com/FastLane-Labs/fastlane-sidecar/internal/metrics"
	monadgateway "github.com/FastLane-Labs/fastlane-sidecar/internal/monad-gateway"
	"github.com/FastLane-Labs/fastlane-sidecar/internal/pool"
	"github.com/FastLane-Labs/fastlane-sidecar/internal/priorities"
	"github.com/FastLane-Labs/fastlane-sidecar/internal/processor"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/config"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/types"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
)

type Sidecar struct {
	config       *config.Config
	shutdownChan chan struct{}

	// Statistics (kept for backward compatibility, but metrics package is primary source)
	txReceived    atomic.Uint64
	bytesTotal    atomic.Uint64
	txStreamed    atomic.Uint64 // Number of transactions streamed to node with priority
	lastHeartbeat atomic.Int64  // Unix timestamp in nanoseconds from node
	poolSize      atomic.Uint64 // Current transaction pool size

	// Components
	nodeListener     *ipc.NodeListener
	nodeSender       *ipc.NodeSender
	gatewayClient    *monadgateway.Client
	txPool           *pool.TransactionPool
	filter           *processor.Filter
	monitoringServer *health.Server
	metrics          *metrics.Metrics

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

	// Initialize metrics (must be before gateway client creation)
	m := metrics.InitMetrics()

	// Create gateway client (handles all credential loading and registration)
	// Returns nil if gateway is disabled or no credentials provided
	gatewayClient, err := monadgateway.NewMonadGatewayClient(config, m)
	if err != nil {
		// Log error but continue without gateway - don't fail sidecar startup
		log.Error("Failed to create gateway client", "error", err)
		gatewayClient = nil
	}

	s := &Sidecar{
		config:        config,
		shutdownChan:  shutdownChan,
		nodeListener:  ipc.NewNodeListener(ctx, config.NodeToSidecarSocketPath),
		nodeSender:    ipc.NewNodeSender(ctx, config.SidecarToNodeSocketPath),
		gatewayClient: gatewayClient,
		txPool:        pool.NewTransactionPool(config.PoolMaxDuration),
		filter:        filter,
		metrics:       m,
		ctx:           ctx,
		cancel:        cancel,
	}

	// Create monitoring server with both health and metrics endpoints
	adapter := &healthStatsAdapter{sidecar: s}
	s.monitoringServer = health.NewServer(config.MonitoringPort, adapter, m)

	return s, nil
}

// healthStatsAdapter adapts Sidecar to health.StatsProvider interface
type healthStatsAdapter struct {
	sidecar *Sidecar
}

func (a *healthStatsAdapter) GetHealthStats() health.Stats {
	return a.sidecar.GetHealthStatsForServer()
}

func (s *Sidecar) Start() error {
	// Start system metrics collection
	s.metrics.StartSystemMetricsCollection()

	// Start monitoring server in background (serves both /health and /metrics)
	go func() {
		if err := s.monitoringServer.Start(); err != nil && err != http.ErrServerClosed {
			log.Error("Monitoring server failed", "error", err)
		}
	}()

	// Start node listener
	if err := s.nodeListener.Start(); err != nil {
		return err
	}

	// Connect to node sender
	if err := s.nodeSender.Connect(); err != nil {
		return err
	}

	// Set node connected metric
	s.metrics.NodeConnected.Store(1)

	// Start gateway connection if client was created
	if s.gatewayClient != nil {
		log.Info("Starting gateway connection")
		if err := s.gatewayClient.Start(); err != nil {
			log.Error("Failed to start gateway client", "error", err)
			// Don't fail startup, gateway will retry in background
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
		s.gatewayClient.Stop()
	}
	if s.metrics != nil {
		s.metrics.StopSystemMetricsCollection()
	}
	if s.monitoringServer != nil {
		s.monitoringServer.Stop()
	}
}

// GetHealthStatsForServer returns health statistics for the HTTP health server
func (s *Sidecar) GetHealthStatsForServer() health.Stats {
	lastHeartbeatNanos := s.lastHeartbeat.Load()
	var lastHeartbeat time.Time
	if lastHeartbeatNanos > 0 {
		lastHeartbeat = time.Unix(0, lastHeartbeatNanos)
	}

	stats := health.Stats{
		LastHeartbeat: lastHeartbeat,
		TxReceived:    s.txReceived.Load(),
		TxStreamed:    s.txStreamed.Load(),
		PoolSize:      s.poolSize.Load(),
	}

	// Add gateway status and update metrics
	if s.gatewayClient != nil {
		gwHealth := s.gatewayClient.Health()
		stats.GatewayConnected = gwHealth.Connected
		stats.GatewayAuthenticated = gwHealth.Authenticated
		if gwHealth.LastError != "" {
			stats.GatewayError = gwHealth.LastError
		}

		// Update metrics
		if gwHealth.Connected {
			s.metrics.GatewayConnected.Store(1)
		} else {
			s.metrics.GatewayConnected.Store(0)
		}
		if gwHealth.Authenticated {
			s.metrics.GatewayAuthenticated.Store(1)
		} else {
			s.metrics.GatewayAuthenticated.Store(0)
		}
	} else {
		stats.GatewayConnected = false
		stats.GatewayAuthenticated = false
		s.metrics.GatewayConnected.Store(0)
		s.metrics.GatewayAuthenticated.Store(0)
		if s.config.DisableGatewayIngress && s.config.DisableGatewayEgress {
			stats.GatewayError = "gateway disabled (ingress and egress both disabled)"
		} else {
			stats.GatewayError = "gateway not initialized"
		}
	}

	return stats
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
			s.handleIncomingMessage(msgBytes, "node")
		}
	}
}

// processGatewayTransactions handles transactions from the gateway
func (s *Sidecar) processGatewayTransactions() {
	if s.gatewayClient == nil {
		log.Info("Gateway client not initialized, not processing gateway transactions")
		return
	}

	if s.config.DisableGatewayIngress {
		log.Info("Gateway ingress disabled, not processing gateway transactions")
		return
	}

	gatewayTxChan := s.gatewayClient.GetTransactionChannel()

	for {
		select {
		case <-s.ctx.Done():
			log.Info("Gateway transaction processing stopped")
			return
		case txBytes := <-gatewayTxChan:
			// Gateway sends raw RLP transaction bytes, not FastlaneMessage enums
			s.handleIncomingTransaction(txBytes, "gateway")
		}
	}
}

// handleIncomingMessage handles FastlaneMessage from node
func (s *Sidecar) handleIncomingMessage(msgBytes []byte, source string) {
	// Parse the FastlaneMessage enum
	msgType, data := types.ParseFastlaneMessage(msgBytes)

	switch msgType {
	case "TxAdded":
		txAdded, ok := data.(types.TxAdded)
		if !ok {
			log.Error("Invalid TxAdded data", "source", source)
			s.metrics.DecodeErrors.Add(1)
			return
		}

		// Calculate latency if timestamp is provided
		var latencyMs int64
		if txAdded.TimestampMs > 0 {
			nowMs := uint64(time.Now().UnixMilli())
			latencyMs = int64(nowMs - txAdded.TimestampMs)
			// Track node message latency
			s.metrics.RecordNodeMessageLatency(float64(latencyMs) / 1000.0)
		}

		log.Info("Received TxAdded message", "bytes", len(txAdded.TxBytes), "source", source, "latency_ms", latencyMs)
		s.txReceived.Add(1) // Only count actual transactions, not heartbeats

		// Track by source
		if source == "node" {
			s.metrics.TxReceivedFromNode.Add(1)
		} else if source == "gateway" {
			s.metrics.TxReceivedFromGateway.Add(1)
		}

		s.handleIncomingTransaction(txAdded.TxBytes, source)

	case "TxDropped":
		txDropped, ok := data.(types.TxDropped)
		if !ok {
			log.Error("Invalid TxDropped data", "source", source)
			s.metrics.DecodeErrors.Add(1)
			return
		}
		log.Info("Received TxDropped message", "hash", common.BytesToHash(txDropped.TxHash[:]).Hex(), "source", source)
		s.metrics.TxDropped.Add(1)
		s.handleTransactionDropped(txDropped.TxHash[:])

	case "Heartbeat":
		log.Debug("Received Heartbeat message", "source", source)
		now := time.Now()
		s.lastHeartbeat.Store(now.UnixNano())
		s.metrics.LastHeartbeatTimestamp.Store(now.Unix())

	default:
		log.Info("Unknown message type, treating as raw tx bytes", "source", source)
		// Fallback to old behavior for compatibility
		s.txReceived.Add(1)
		if source == "node" {
			s.metrics.TxReceivedFromNode.Add(1)
		} else if source == "gateway" {
			s.metrics.TxReceivedFromGateway.Add(1)
		}
		s.handleIncomingTransaction(msgBytes, source)
	}
}

// handleIncomingTransaction implements the new transaction lifecycle
func (s *Sidecar) handleIncomingTransaction(txBytes []byte, source string) {
	startTime := time.Now()

	// The txBytes are raw transaction bytes from TxAdded message

	// Decode transaction using UnmarshalBinary which handles both:
	// - Legacy RLP transactions
	// - EIP-2718 typed transactions (envelope format)
	var tx ethTypes.Transaction
	if err := tx.UnmarshalBinary(txBytes); err != nil {
		log.Error("Failed to decode transaction", "error", err, "source", source)
		s.metrics.DecodeErrors.Add(1)
		return
	}

	hash := tx.Hash()

	// Check if transaction already exists in pool
	if s.txPool.Exists(hash) {
		log.Debug("Transaction already in pool", "hash", hash.Hex())
		return
	}

	// Check if this is a fastlane bid
	txType, bidData := s.filter.ClassifyTransaction(&tx)

	// Add to transaction pool with decoded tx
	if err := s.txPool.AddTransaction(&tx, txBytes, source); err != nil {
		log.Error("Failed to add transaction to pool", "error", err)
		return
	}

	// Update transaction type in pool
	s.txPool.UpdateTransactionType(hash, txType)
	if pooledTx := s.txPool.GetTransaction(hash); pooledTx != nil {
		pooledTx.BidData = bidData
	}

	// Update pool size metric
	poolSize := s.txPool.Size()
	s.poolSize.Store(poolSize)
	s.metrics.PoolSize.Store(poolSize)

	switch txType {
	case types.TOBBid:
		s.metrics.TOBBidsProcessed.Add(1)
		s.handleTOBBid(txBytes, bidData)
	case types.BackrunBid:
		s.metrics.BackrunBidsProcessed.Add(1)
		s.handleBackrunBid(txBytes, hash, bidData)
	case types.NormalTransaction:
		s.metrics.NormalTxsProcessed.Add(1)
		// Normal transaction - just keep in pool and forward to gateway if from node
		if source == "node" {
			s.forwardToGateway(txBytes)
		}
	}

	// Track processing latency
	processingTime := time.Since(startTime).Seconds()
	s.metrics.RecordTxProcessingLatency(processingTime)
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
				log.Debug("Failed to notify gateway of dropped transaction", "error", err, "hash", txHash.Hex())
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

	// Track successful backrun pair match
	s.metrics.BackrunPairsMatched.Add(1)

	log.Info("Processed backrun pair immediately", "bid_hash", bidHash.Hex(), "target_hash", targetTxHash.Hex(), "bid_amount", bidData.BidAmount.String(), "opp_gas_tip", oppGasTip.String())
}

// streamTransaction sends a transaction with priority to the node
func (s *Sidecar) streamTransaction(txWithPriority types.TxWithPriority) {
	if err := s.nodeSender.SendTxWithPriority(txWithPriority); err != nil {
		log.Error("Failed to send transaction to node", "error", err)
		s.metrics.SendErrors.Add(1)
		return
	}
	s.txStreamed.Add(1)
	s.metrics.TxSentToNode.Add(1)
}

// forwardToGateway sends transaction to gateway (if it didn't come from there)
func (s *Sidecar) forwardToGateway(txBytes []byte) {
	if s.gatewayClient == nil {
		return // Gateway client not initialized
	}
	if err := s.gatewayClient.SendToGateway(txBytes); err != nil {
		log.Debug("Failed to send transaction to gateway", "error", err)
		s.metrics.GatewayErrors.Add(1)
		return
	}
	s.metrics.TxSentToGateway.Add(1)
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
			// Track pool size before cleanup
			sizeBefore := s.txPool.Size()

			s.txPool.CleanupOldTransactions()
			s.metrics.PoolCleanupOps.Add(1)

			// Calculate how many were removed
			sizeAfter := s.txPool.Size()
			if sizeAfter < sizeBefore {
				expired := sizeBefore - sizeAfter
				s.metrics.TxExpired.Add(expired)
			}

			// Update pool size metric
			s.poolSize.Store(sizeAfter)
			s.metrics.PoolSize.Store(sizeAfter)
		}
	}
}
