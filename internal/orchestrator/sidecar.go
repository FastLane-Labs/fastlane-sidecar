package orchestrator

import (
	"context"
	"math/big"
	"net/http"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/internal/health"
	"github.com/FastLane-Labs/fastlane-sidecar/internal/ipc"
	"github.com/FastLane-Labs/fastlane-sidecar/internal/metrics"
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
	txpoolClient     *ipc.TxPoolIPCClient
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

	// Initialize metrics
	m := metrics.InitMetrics()

	s := &Sidecar{
		config:       config,
		shutdownChan: shutdownChan,
		txpoolClient: ipc.NewTxPoolIPCClient(ctx, config.TxPoolSocketPath),
		txPool:       pool.NewTransactionPool(config.PoolMaxDuration),
		filter:       filter,
		metrics:      m,
		ctx:          ctx,
		cancel:       cancel,
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

	// TxPool IPC client connects automatically in background
	// Just wait a moment to allow initial connection
	time.Sleep(100 * time.Millisecond)

	// Set node connected metric (will be updated by connection status)
	if s.txpoolClient.IsConnected() {
		s.metrics.NodeConnected.Store(1)
	}

	// Start processing goroutines
	go s.processTxPoolEvents()
	go s.cleanupOldTransactions()

	return nil
}

func (s *Sidecar) Stop() {
	s.cancel()
	if s.txpoolClient != nil {
		s.txpoolClient.Close()
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

	return health.Stats{
		LastHeartbeat:   lastHeartbeat,
		TxReceived:      s.txReceived.Load(),
		TxStreamed:      s.txStreamed.Load(),
		PoolSize:        s.poolSize.Load(),
		MonadBftVersion: getMonadBftVersion(),
	}
}

// processTxPoolEvents handles events from the txpool IPC
func (s *Sidecar) processTxPoolEvents() {
	defer close(s.shutdownChan)

	eventChan := s.txpoolClient.GetEventChannel()

	for {
		select {
		case <-s.ctx.Done():
			log.Info("TxPool event processing stopped")
			return
		case event := <-eventChan:
			s.handleTxPoolEvent(event)
		}
	}
}

// handleTxPoolEvent handles events from the txpool
func (s *Sidecar) handleTxPoolEvent(event ipc.EthTxPoolEvent) {
	startTime := time.Now()

	switch action := event.Action.(type) {
	case ipc.InsertAction:
		// New transaction inserted into txpool
		log.Info("Received Insert event", "tx_hash", event.TxHash.Hex(), "address", action.Address.Hex(), "owned", action.Owned)
		s.txReceived.Add(1)
		s.metrics.TxReceivedFromNode.Add(1)

		// Update heartbeat timestamp
		s.lastHeartbeat.Store(time.Now().UnixNano())

		// Track message latency (from insertion to sidecar receipt)
		s.metrics.RecordNodeMessageLatency(time.Since(startTime).Seconds())

		// Process the transaction with original RLP bytes
		s.handleIncomingTransactionFromEvent(action.Tx, action.OriginalTxRLP)

	case ipc.CommitAction:
		// Transaction committed to blockchain - remove from pool if exists
		log.Debug("Received Commit event", "tx_hash", event.TxHash.Hex())
		removedTx := s.txPool.RemoveTransaction(event.TxHash)
		if removedTx != nil {
			log.Info("Transaction committed and removed from pool", "hash", event.TxHash.Hex())
		}

	case ipc.DropAction:
		// Transaction dropped from txpool
		log.Info("Received Drop event", "tx_hash", event.TxHash.Hex(), "reason", action.Reason)
		s.metrics.TxDropped.Add(1)
		removedTx := s.txPool.RemoveTransaction(event.TxHash)
		if removedTx != nil {
			log.Info("Transaction dropped and removed from pool", "hash", event.TxHash.Hex(), "source", removedTx.Source)
		}

	case ipc.EvictAction:
		// Transaction evicted from txpool
		log.Info("Received Evict event", "tx_hash", event.TxHash.Hex(), "reason", action.Reason)
		removedTx := s.txPool.RemoveTransaction(event.TxHash)
		if removedTx != nil {
			log.Info("Transaction evicted and removed from pool", "hash", event.TxHash.Hex())
		}

	default:
		log.Error("Unknown event action type", "tx_hash", event.TxHash.Hex())
	}
}

// handleIncomingTransactionFromEvent processes a transaction from txpool event
func (s *Sidecar) handleIncomingTransactionFromEvent(tx *ethTypes.Transaction, originalRLP []byte) {
	startTime := time.Now()

	hash := tx.Hash()

	// Get transaction bytes for storage
	txBytes, err := tx.MarshalBinary()
	if err != nil {
		log.Error("Failed to marshal transaction", "error", err, "hash", hash.Hex())
		s.metrics.DecodeErrors.Add(1)
		return
	}

	// Check if transaction already exists in pool
	if s.txPool.Exists(hash) {
		log.Debug("Transaction already in pool", "hash", hash.Hex())
		return
	}

	// Check if this is a fastlane bid
	txType, bidData := s.filter.ClassifyTransaction(tx)

	// Add to transaction pool with decoded tx and original RLP
	if err := s.txPool.AddTransactionWithRLP(tx, txBytes, originalRLP, "txpool"); err != nil {
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
		s.handleTOBBid(tx, bidData)
	case types.BackrunBid:
		s.metrics.BackrunBidsProcessed.Add(1)
		s.handleBackrunBid(tx, hash, bidData)
	case types.NormalTransaction:
		s.metrics.NormalTxsProcessed.Add(1)
		// Normal transaction - just keep in pool
	}

	// Track processing latency
	processingTime := time.Since(startTime).Seconds()
	s.metrics.RecordTxProcessingLatency(processingTime)
}

// handleTOBBid processes TOB bid - compute priority and stream immediately
func (s *Sidecar) handleTOBBid(tx *ethTypes.Transaction, bidData *types.BidData) {
	if bidData == nil || bidData.BidAmount == nil {
		log.Error("TOB bid missing bid data")
		return
	}

	// Compute priority
	priority := priorities.CalculateTOBPriority(bidData.BidAmount)

	// Stream immediately to txpool
	s.streamTransaction(tx, priority)

	log.Info("Processed TOB bid", "bid_amount", bidData.BidAmount.String(), "priority", priorities.FormatPriority(priority))
}

// handleBackrunBid processes backrun bid - look for opportunity and stream both if found
func (s *Sidecar) handleBackrunBid(tx *ethTypes.Transaction, bidHash common.Hash, bidData *types.BidData) {
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
	// Use target transaction hash as backrun_id for grouping

	// Stream opportunity transaction first
	oppPriority := priorities.CalculateOpportunityPriority(targetTxHash)
	s.streamTransaction(targetTx.Tx, oppPriority)

	// Stream backrun bid
	backrunPriority := priorities.CalculateBackrunPriority(bidData.BidAmount, targetTxHash)
	s.streamTransaction(tx, backrunPriority)

	// Track successful backrun pair match
	s.metrics.BackrunPairsMatched.Add(1)

	log.Info("Processed backrun pair immediately", "bid_hash", bidHash.Hex(), "target_hash", targetTxHash.Hex(), "bid_amount", bidData.BidAmount.String())
}

// streamTransaction sends a transaction with priority to the txpool
func (s *Sidecar) streamTransaction(tx *ethTypes.Transaction, priority *big.Int) {
	// Look up the original RLP bytes from the pool
	pooledTx := s.txPool.GetTransaction(tx.Hash())
	if pooledTx == nil || len(pooledTx.OriginalRLP) == 0 {
		log.Error("Cannot send transaction: original RLP not found in pool", "hash", tx.Hash().Hex())
		s.metrics.SendErrors.Add(1)
		return
	}

	if err := s.txpoolClient.SendTxWithPriorityRLP(pooledTx.OriginalRLP, priority, []byte{}); err != nil {
		log.Error("Failed to send transaction to txpool", "error", err)
		s.metrics.SendErrors.Add(1)
		return
	}
	s.txStreamed.Add(1)
	s.metrics.TxSentToNode.Add(1)
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

// getMonadBftVersion attempts to get the monad-bft package version
func getMonadBftVersion() string {
	// Try dpkg-query first (most reliable for installed packages)
	cmd := exec.Command("dpkg-query", "-W", "-f=${Version}", "monad-bft")
	output, err := cmd.Output()
	if err == nil && len(output) > 0 {
		return strings.TrimSpace(string(output))
	}

	// If dpkg-query fails, return empty string
	// This is expected if monad-bft is not installed via apt
	return ""
}
