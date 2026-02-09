package types

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// TxAdded represents a transaction added to mempool
type TxAdded struct {
	TxBytes     []byte `json:"tx_bytes"`
	TimestampMs uint64 `json:"timestamp_ms"`
}

// TxDropped represents a transaction dropped from mempool
type TxDropped struct {
	TxHash [32]byte `json:"tx_hash"`
}

// TxWithPriority represents a transaction with priority sent to node
type TxWithPriority struct {
	TxBytes  []byte     `json:"tx_bytes"`
	Priority [16]uint64 `json:"priority"`
}

// SidecarMessage represents messages sent from sidecar to node
// This is a Rust enum serialized with bincode
type SidecarMessage struct {
	Type string      `json:"type"` // "TxWithPriority" or "Heartbeat"
	Data interface{} `json:"data"` // TxWithPriority or nil
}

// PooledTransaction represents a transaction in the pool with metadata
type PooledTransaction struct {
	Tx          *types.Transaction
	TxBytes     []byte
	OriginalRLP []byte // Original alloy RLP bytes from txpool (for forwarding back with priority)
	ReceivedAt  time.Time
	Source      string // "txpool"
	TxType      TransactionType
	Hash        common.Hash
	BidData     *BidData // Bid-specific data if this is a bid transaction
}

// TransactionType represents the type of transaction
type TransactionType int

const (
	NormalTransaction TransactionType = iota
	TOBBid
	BackrunBid
)

// BidData contains bid-specific information
type BidData struct {
	BidAmount    *big.Int     // Bid amount extracted from tx data
	TargetTxHash *common.Hash // For backrun bids, the target tx hash
}
