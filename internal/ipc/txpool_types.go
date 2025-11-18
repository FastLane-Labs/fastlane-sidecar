package ipc

import (
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// EthTxPoolSnapshot represents the initial snapshot of txpool state
type EthTxPoolSnapshot struct {
	TxHashes []common.Hash
}

// EthTxPoolEventType represents the type of txpool event
type EthTxPoolEventType uint8

const (
	EventInsert EthTxPoolEventType = iota
	EventCommit
	EventDrop
	EventEvict
)

// EthTxPoolEvent represents an event from the txpool
type EthTxPoolEvent struct {
	TxHash common.Hash
	Action EventAction
}

// EventAction is an interface for different event action types
type EventAction interface {
	isEventAction()
}

// InsertAction represents a transaction insertion event
type InsertAction struct {
	Address common.Address
	Owned   bool
	Tx      *types.Transaction // Full transaction envelope
}

func (InsertAction) isEventAction() {}

// CommitAction represents a transaction commit event
type CommitAction struct{}

func (CommitAction) isEventAction() {}

// DropAction represents a transaction drop event
type DropAction struct {
	Reason string // Drop reason (simplified as string for now)
}

func (DropAction) isEventAction() {}

// EvictAction represents a transaction eviction event
type EvictAction struct {
	Reason string // Evict reason (simplified as string for now)
}

func (EvictAction) isEventAction() {}

// EthTxPoolIpcTx represents a transaction to be sent to the txpool with priority
type EthTxPoolIpcTx struct {
	Tx        *types.Transaction
	Priority  *big.Int // U256 priority value
	ExtraData []byte   // Optional extra data
}

// RLP encoding/decoding helpers

// EncodeRLP encodes EthTxPoolIpcTx to RLP format
func (tx *EthTxPoolIpcTx) EncodeRLP() ([]byte, error) {
	// Encode transaction to RLP
	txBytes, err := tx.Tx.MarshalBinary()
	if err != nil {
		return nil, err
	}

	// Create struct matching Rust EthTxPoolIpcTx
	// struct { tx: Vec<u8>, priority: U256, extra_data: Vec<u8> }
	data := []interface{}{
		txBytes,
		tx.Priority,
		tx.ExtraData,
	}

	return rlp.EncodeToBytes(data)
}

// DecodeEthTxPoolSnapshot decodes a txpool snapshot from bincode format
func DecodeEthTxPoolSnapshot(data []byte) (*EthTxPoolSnapshot, error) {
	// Bincode format for HashSet<TxHash> is encoded as Vec<TxHash>
	// Vec format: [length:8 bytes little-endian][elements...]
	// Each TxHash is 32 bytes
	if len(data) < 8 {
		return nil, fmt.Errorf("data too short for snapshot Vec length: %d bytes", len(data))
	}

	vecLen := binary.LittleEndian.Uint64(data[:8])
	offset := 8

	txHashes := make([]common.Hash, 0, vecLen)
	for i := uint64(0); i < vecLen; i++ {
		if len(data[offset:]) < 32 {
			return nil, fmt.Errorf("data too short for tx_hash %d: need 32, have %d", i, len(data[offset:]))
		}

		var txHash common.Hash
		copy(txHash[:], data[offset:offset+32])
		txHashes = append(txHashes, txHash)
		offset += 32
	}

	return &EthTxPoolSnapshot{
		TxHashes: txHashes,
	}, nil
}

// DecodeEthTxPoolEvents decodes a slice of EthTxPoolEvent from bincode format
func DecodeEthTxPoolEvents(data []byte) ([]EthTxPoolEvent, error) {
	// Bincode format for Vec<T>: [length:8 bytes little-endian][elements...]
	if len(data) < 8 {
		return nil, fmt.Errorf("data too short for Vec length: %d bytes", len(data))
	}

	vecLen := binary.LittleEndian.Uint64(data[:8])
	offset := 8

	events := make([]EthTxPoolEvent, 0, vecLen)
	for i := uint64(0); i < vecLen; i++ {
		event, bytesRead, err := decodeEthTxPoolEvent(data[offset:])
		if err != nil {
			return nil, fmt.Errorf("failed to decode event %d: %w", i, err)
		}
		events = append(events, event)
		offset += bytesRead
	}

	return events, nil
}

// decodeEthTxPoolEvent decodes a single EthTxPoolEvent from bincode format
// Returns the event and number of bytes consumed
func decodeEthTxPoolEvent(data []byte) (EthTxPoolEvent, int, error) {
	// EthTxPoolEvent structure:
	// - tx_hash: [32 bytes]
	// - action: EthTxPoolEventType (enum)

	if len(data) < 32 {
		return EthTxPoolEvent{}, 0, fmt.Errorf("data too short for tx_hash: %d bytes", len(data))
	}

	var txHash common.Hash
	copy(txHash[:], data[:32])
	offset := 32

	// Decode action enum (starts with u32 variant index)
	action, bytesRead, err := decodeEventAction(data[offset:])
	if err != nil {
		return EthTxPoolEvent{}, 0, fmt.Errorf("failed to decode action: %w", err)
	}
	offset += bytesRead

	return EthTxPoolEvent{
		TxHash: txHash,
		Action: action,
	}, offset, nil
}

// decodeEventAction decodes an EventAction from bincode format
// Returns the action and number of bytes consumed
func decodeEventAction(data []byte) (EventAction, int, error) {
	// Bincode enum format: [variant_index:u32 little-endian][variant_data...]
	if len(data) < 4 {
		return nil, 0, fmt.Errorf("data too short for enum variant: %d bytes", len(data))
	}

	variantIndex := binary.LittleEndian.Uint32(data[:4])
	offset := 4

	switch EthTxPoolEventType(variantIndex) {
	case EventInsert:
		// Insert { address: Address, owned: bool, tx: TxEnvelope }
		// address: [20 bytes]
		// owned: [1 byte] (bool as u8)
		// tx: Vec<u8> [length:8 bytes][tx_bytes] (RLP-encoded transaction)

		if len(data[offset:]) < 20+1+8 {
			return nil, 0, fmt.Errorf("data too short for Insert action")
		}

		var address common.Address
		copy(address[:], data[offset:offset+20])
		offset += 20

		owned := data[offset] != 0
		offset += 1

		// Decode tx Vec<u8>
		txBytesLen := binary.LittleEndian.Uint64(data[offset : offset+8])
		offset += 8

		if len(data[offset:]) < int(txBytesLen) {
			return nil, 0, fmt.Errorf("data too short for tx bytes: need %d, have %d", txBytesLen, len(data[offset:]))
		}

		txBytes := data[offset : offset+int(txBytesLen)]
		offset += int(txBytesLen)

		// Unmarshal RLP-encoded transaction
		tx := new(types.Transaction)
		if err := tx.UnmarshalBinary(txBytes); err != nil {
			return nil, 0, fmt.Errorf("failed to unmarshal transaction: %w", err)
		}

		return InsertAction{
			Address: address,
			Owned:   owned,
			Tx:      tx,
		}, offset, nil

	case EventCommit:
		// Commit (unit variant, no data)
		return CommitAction{}, offset, nil

	case EventDrop:
		// Drop { reason: EthTxPoolDropReason }
		// For now, we skip parsing the complex reason enum and just return a generic reason
		// TODO: Implement full reason parsing if needed
		return DropAction{Reason: "dropped"}, offset, nil

	case EventEvict:
		// Evict { reason: EthTxPoolEvictReason }
		// For now, we skip parsing the complex reason enum and just return a generic reason
		// TODO: Implement full reason parsing if needed
		return EvictAction{Reason: "evicted"}, offset, nil

	default:
		return nil, 0, fmt.Errorf("unknown event variant: %d", variantIndex)
	}
}
