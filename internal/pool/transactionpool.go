package pool

import (
	"sync"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/types"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
)

// TransactionPool manages transactions with TTL
type TransactionPool struct {
	mu          sync.RWMutex
	allTxs      map[common.Hash]*types.PooledTransaction
	maxDuration time.Duration
}

// NewTransactionPool creates a new transaction pool
func NewTransactionPool(maxDuration time.Duration) *TransactionPool {
	return &TransactionPool{
		allTxs:      make(map[common.Hash]*types.PooledTransaction),
		maxDuration: maxDuration,
	}
}

func (tp *TransactionPool) Exists(hash common.Hash) bool {
	tp.mu.RLock()
	defer tp.mu.RUnlock()
	_, exists := tp.allTxs[hash]
	return exists
}

// AddTransactionWithRLP adds a transaction to the pool with original RLP bytes
func (tp *TransactionPool) AddTransactionWithRLP(tx *ethTypes.Transaction, txBytes []byte, originalRLP []byte, source string) error {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	hash := tx.Hash()

	// Check if already exists
	if _, exists := tp.allTxs[hash]; exists {
		log.Debug("Transaction already in pool", "hash", hash.Hex())
		return nil
	}

	// Create pooled transaction with original RLP
	pooledTx := &types.PooledTransaction{
		Tx:          tx,
		TxBytes:     txBytes,
		OriginalRLP: originalRLP,
		ReceivedAt:  time.Now(),
		Source:      source,
		TxType:      types.NormalTransaction, // Will be updated by classifier
		Hash:        hash,
	}

	tp.allTxs[hash] = pooledTx

	log.Info("Transaction added to pool", "hash", hash.Hex(), "source", source)
	return nil
}

// GetTransaction retrieves a transaction by hash
func (tp *TransactionPool) GetTransaction(hash common.Hash) *types.PooledTransaction {
	tp.mu.RLock()
	defer tp.mu.RUnlock()
	return tp.allTxs[hash]
}

// UpdateTransactionType updates the type of a transaction
func (tp *TransactionPool) UpdateTransactionType(hash common.Hash, txType types.TransactionType) {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	if tx, exists := tp.allTxs[hash]; exists {
		tx.TxType = txType
	}
}

// RemoveTransaction removes a transaction from the pool by hash
func (tp *TransactionPool) RemoveTransaction(hash common.Hash) *types.PooledTransaction {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	// Get the transaction before removing it
	tx, exists := tp.allTxs[hash]
	if !exists {
		return nil
	}

	// Remove from main pool
	delete(tp.allTxs, hash)

	log.Info("Removed transaction from pool", "hash", hash.Hex())
	return tx
}

// CleanupOldTransactions removes transactions older than maxDuration
func (tp *TransactionPool) CleanupOldTransactions() {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-tp.maxDuration)

	// Clean up main transaction pool
	for hash, tx := range tp.allTxs {
		if tx.ReceivedAt.Before(cutoff) {
			delete(tp.allTxs, hash)
			log.Info("Removed expired transaction", "hash", hash.Hex(), "age", now.Sub(tx.ReceivedAt))
		}
	}
}

// Size returns the current number of transactions in the pool
func (tp *TransactionPool) Size() uint64 {
	tp.mu.RLock()
	defer tp.mu.RUnlock()
	return uint64(len(tp.allTxs))
}
