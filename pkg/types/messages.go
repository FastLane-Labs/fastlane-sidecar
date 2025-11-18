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
	Source      string // "node" or "gateway"
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

// BackrunAuctionPool represents an auction pool for backrun bids
type BackrunAuctionPool struct {
	OpportunityTx        *PooledTransaction
	BackrunBids          []*PooledTransaction
	CreatedAt            time.Time
	StreamingScheduledAt time.Time
	Status               AuctionPoolStatus
}

// AuctionPoolStatus represents the status of an auction pool
type AuctionPoolStatus int

const (
	AuctionPoolCollecting AuctionPoolStatus = iota
	AuctionPoolReady
	AuctionPoolStreamed
)

// TOBScheduledTx represents a TOB bid scheduled for streaming
type TOBScheduledTx struct {
	Tx                  *PooledTransaction
	Priority            [16]uint64
	ScheduledStreamTime time.Time
}

// GatewayResponse represents messages received from MEV gateway
// TODO: Define actual structure based on MEV gateway API
type GatewayResponse struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

// TransactionSubmission represents a transaction being sent to MEV gateway
// TODO: Extend with additional metadata as needed
type TransactionSubmission struct {
	Transaction []byte `json:"transaction"`
	Timestamp   int64  `json:"timestamp"`
}
