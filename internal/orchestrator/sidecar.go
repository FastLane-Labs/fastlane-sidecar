package orchestrator

import (
	"context"
	"sync/atomic"
	"time"

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
	txReceived atomic.Uint64
	bytesTotal atomic.Uint64

	// Components
	nodeListener  *ipc.NodeListener
	nodeSender    *ipc.NodeSender
	gatewayClient *gateway.Client
	txPool        *pool.TransactionPool
	filter        *processor.Filter

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

func NewSidecar(config *config.Config, shutdownChan chan struct{}) (*Sidecar, error) {
	ctx, cancel := context.WithCancel(context.Background())

	filter, err := processor.NewFilter(config.FastlaneContract, config.TOBMethodSig, config.BackrunMethodSig)
	if err != nil {
		cancel()
		return nil, err
	}

	return &Sidecar{
		config:        config,
		shutdownChan:  shutdownChan,
		nodeListener:  ipc.NewNodeListener(ctx, config.NodeToSidecarSocketPath),
		nodeSender:    ipc.NewNodeSender(ctx, config.SidecarToNodeSocketPath),
		gatewayClient: gateway.NewClient(config.GatewayURL, ctx),
		txPool:        pool.NewTransactionPool(config.PoolMaxDuration),
		filter:        filter,
		ctx:           ctx,
		cancel:        cancel,
	}, nil
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

	// Connect to gateway
	if err := s.gatewayClient.Connect(); err != nil {
		log.Error("Failed to connect to gateway", "error", err)
		// Continue without gateway connection for now
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
		if removedTx.Source != "gateway" {
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
	}
}

// forwardToGateway sends transaction to gateway (if it didn't come from there)
func (s *Sidecar) forwardToGateway(txBytes []byte) {
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
		}
	}
}
