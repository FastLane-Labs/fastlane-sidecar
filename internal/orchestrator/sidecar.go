package orchestrator

import (
	"context"
	"math/big"
	"net/http"
	"os/exec"
	"strings"
	"sync"
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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Sidecar struct {
	config       *config.Config
	shutdownChan chan struct{}

	// Statistics (kept for backward compatibility, but metrics package is primary source)
	txReceived     atomic.Uint64
	txStreamed     atomic.Uint64 // Number of transactions streamed to node with priority
	poolSize       atomic.Uint64 // Current transaction pool size
	lastReceivedAt atomic.Int64  // Unix timestamp in nanoseconds of last tx received
	lastSentAt     atomic.Int64  // Unix timestamp in nanoseconds of last tx sent with priority
	lastCommitTime atomic.Int64  // Unix nanoseconds of last Commit event processed

	// Tracks TXs we sent with priority, keyed by hash → send time
	prioritizedTxs   map[common.Hash]time.Time
	prioritizedTxsMu sync.Mutex

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
		txpoolClient: ipc.NewTxPoolIPCClient(ctx, config.TxPoolSocketPath, func() {
			m.NodeReconnections.Add(1)
		}),
		txPool:         pool.NewTransactionPool(config.PoolMaxDuration),
		filter:         filter,
		metrics:        m,
		prioritizedTxs: make(map[common.Hash]time.Time),
		ctx:            ctx,
		cancel:         cancel,
	}

	// Create monitoring server with health, metrics, and optionally Prometheus endpoints
	adapter := &healthStatsAdapter{sidecar: s}

	var promHandler http.Handler
	if config.PrometheusEnabled {
		promCollector := metrics.NewSidecarCollector(m, &promHealthAdapter{sidecar: s})
		promRegistry := prometheus.NewRegistry()
		promRegistry.MustRegister(promCollector)
		promHandler = promhttp.HandlerFor(promRegistry, promhttp.HandlerOpts{})
	}

	s.monitoringServer = health.NewServer(config.MonitoringPort, adapter, m, promHandler)

	return s, nil
}

// healthStatsAdapter adapts Sidecar to health.StatsProvider interface
type healthStatsAdapter struct {
	sidecar *Sidecar
}

func (a *healthStatsAdapter) GetHealthStats() health.Stats {
	return a.sidecar.GetHealthStatsForServer()
}

// promHealthAdapter adapts Sidecar to metrics.HealthStatsProvider for Prometheus
type promHealthAdapter struct {
	sidecar *Sidecar
}

func (a *promHealthAdapter) GetHealthStatsForPrometheus() metrics.HealthStatsForPrometheus {
	var lastReceivedAt, lastSentAt float64
	if ts := a.sidecar.lastReceivedAt.Load(); ts > 0 {
		lastReceivedAt = float64(ts) / 1e9 // nanoseconds to seconds
	}
	if ts := a.sidecar.lastSentAt.Load(); ts > 0 {
		lastSentAt = float64(ts) / 1e9
	}
	return metrics.HealthStatsForPrometheus{
		TxReceived:     a.sidecar.txReceived.Load(),
		TxStreamed:     a.sidecar.txStreamed.Load(),
		PoolSize:       a.sidecar.poolSize.Load(),
		LastReceivedAt: lastReceivedAt,
		LastSentAt:     lastSentAt,
	}
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
	var lastReceivedAt, lastSentAt time.Time
	if ts := s.lastReceivedAt.Load(); ts > 0 {
		lastReceivedAt = time.Unix(0, ts)
	}
	if ts := s.lastSentAt.Load(); ts > 0 {
		lastSentAt = time.Unix(0, ts)
	}

	return health.Stats{
		TxReceived:      s.txReceived.Load(),
		TxStreamed:      s.txStreamed.Load(),
		PoolSize:        s.poolSize.Load(),
		LastReceivedAt:  lastReceivedAt,
		LastSentAt:      lastSentAt,
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
		// Record arrival-after-commit before any processing
		if lastCommit := s.lastCommitTime.Load(); lastCommit > 0 {
			deltaMs := float64(time.Now().UnixNano()-lastCommit) / 1e6
			go s.metrics.RecordTxArrivalAfterCommit(deltaMs)
		}

		s.txReceived.Add(1)
		s.metrics.TxReceivedFromNode.Add(1)
		s.lastReceivedAt.Store(time.Now().UnixNano())

		// Track message latency (from insertion to sidecar receipt)
		s.metrics.RecordNodeMessageLatency(time.Since(startTime).Seconds())

		// Process the transaction with original RLP bytes
		s.handleIncomingTransactionFromEvent(action.Tx, action.OriginalTxRLP)

	case ipc.CommitAction:
		s.lastCommitTime.Store(time.Now().UnixNano())
		if s.removePrioritizedTx(event.TxHash) {
			s.metrics.PrioritizedTxCommittedBeforeEcho.Add(1)
		}
		s.txPool.RemoveTransaction(event.TxHash)

	case ipc.DropAction:
		s.metrics.TxDropped.Add(1)
		if s.removePrioritizedTx(event.TxHash) {
			s.metrics.PrioritizedTxDroppedBeforeEcho.Add(1)
		}
		s.txPool.RemoveTransaction(event.TxHash)

	case ipc.EvictAction:
		if s.removePrioritizedTx(event.TxHash) {
			s.metrics.PrioritizedTxEvictedBeforeEcho.Add(1)
		}
		s.txPool.RemoveTransaction(event.TxHash)

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
		// Check if this is an echo of a TX we prioritized
		s.prioritizedTxsMu.Lock()
		if sendTime, ok := s.prioritizedTxs[hash]; ok {
			deltaMs := float64(time.Since(sendTime).Nanoseconds()) / 1e6
			delete(s.prioritizedTxs, hash)
			s.prioritizedTxsMu.Unlock()
			go func(t time.Time, h common.Hash, ms float64) {
				s.metrics.RecordPriorityRoundTrip(ms)
				log.Info("Priority TX echo received", "hash", h.Hex(), "latency_ms", ms, "t", t)
			}(time.Now(), hash, deltaMs)
		} else {
			s.prioritizedTxsMu.Unlock()
		}
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

	go func(t time.Time, h common.Hash, bid *big.Int, p *big.Int) {
		log.Info("TOB bid classified", "hash", h.Hex(), "bid_amount", bid.String(), "priority", priorities.FormatPriority(p), "t", t)
	}(time.Now(), tx.Hash(), bidData.BidAmount, priority)

	// Stream immediately to txpool
	s.streamTransaction(tx, priority)
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
		go func(t time.Time, bh common.Hash, th common.Hash) {
			log.Info("Backrun target not found", "bid_hash", bh.Hex(), "target_hash", th.Hex(), "t", t)
		}(time.Now(), bidHash, targetTxHash)
		return
	}

	// Found target transaction - compute priorities and stream both
	go func(t time.Time, bh common.Hash, th common.Hash, bid *big.Int) {
		log.Info("Backrun pair classified", "bid_hash", bh.Hex(), "target_hash", th.Hex(), "bid_amount", bid.String(), "t", t)
	}(time.Now(), bidHash, targetTxHash, bidData.BidAmount)

	// Stream opportunity transaction first
	oppPriority := priorities.CalculateOpportunityPriority(targetTxHash)
	s.streamTransaction(targetTx.Tx, oppPriority)

	// Stream backrun bid
	backrunPriority := priorities.CalculateBackrunPriority(bidData.BidAmount, targetTxHash)
	s.streamTransaction(tx, backrunPriority)

	// Track successful backrun pair match
	s.metrics.BackrunPairsMatched.Add(1)
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
	s.lastSentAt.Store(time.Now().UnixNano())

	go func(t time.Time, h common.Hash) {
		log.Info("Priority TX sent to node", "hash", h.Hex(), "t", t)
	}(time.Now(), tx.Hash())

	// Track send time for round-trip latency measurement
	s.prioritizedTxsMu.Lock()
	s.prioritizedTxs[tx.Hash()] = time.Now()
	s.prioritizedTxsMu.Unlock()
}

// removePrioritizedTx removes a hash from the prioritized TX tracking map.
// Returns true if the hash was present (i.e. we sent it with priority but never got the echo).
func (s *Sidecar) removePrioritizedTx(hash common.Hash) bool {
	s.prioritizedTxsMu.Lock()
	_, existed := s.prioritizedTxs[hash]
	delete(s.prioritizedTxs, hash)
	s.prioritizedTxsMu.Unlock()
	return existed
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

			// Clean up stale prioritized TX entries
			s.prioritizedTxsMu.Lock()
			now := time.Now()
			for hash, sendTime := range s.prioritizedTxs {
				if now.Sub(sendTime) > s.config.PoolMaxDuration {
					delete(s.prioritizedTxs, hash)
				}
			}
			s.prioritizedTxsMu.Unlock()
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
