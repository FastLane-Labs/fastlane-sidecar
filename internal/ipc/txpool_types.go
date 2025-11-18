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

	fmt.Printf("DEBUG: DecodeEthTxPoolEvents: vecLen=%d, total_data_len=%d, first_16_bytes=%x\n",
		vecLen, len(data), data[:min(16, len(data))])

	events := make([]EthTxPoolEvent, 0, vecLen)
	for i := uint64(0); i < vecLen; i++ {
		fmt.Printf("DEBUG: Decoding event %d at offset %d, remaining_bytes=%d, next_36_bytes=%x\n",
			i, offset, len(data[offset:]), data[offset:min(offset+36, len(data))])
		event, bytesRead, err := decodeEthTxPoolEvent(data[offset:])
		if err != nil {
			return nil, fmt.Errorf("failed to decode event %d: %w", i, err)
		}
		fmt.Printf("DEBUG: Event %d decoded successfully, consumed %d bytes, tx_hash=%x\n",
			i, bytesRead, event.TxHash)
		events = append(events, event)
		offset += bytesRead
	}

	return events, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// decodeEthTxPoolEvent decodes a single EthTxPoolEvent from bincode format
// Returns the event and number of bytes consumed
func decodeEthTxPoolEvent(data []byte) (EthTxPoolEvent, int, error) {
	// EthTxPoolEvent structure:
	// - tx_hash: TxHash (bincode serializes as [length:8 bytes LE][32 bytes])
	// - action: EthTxPoolEventType (enum)

	if len(data) < 8 {
		return EthTxPoolEvent{}, 0, fmt.Errorf("data too short for tx_hash length: %d bytes", len(data))
	}

	// Read TxHash length prefix (should be 32)
	txHashLen := binary.LittleEndian.Uint64(data[:8])
	if txHashLen != 32 {
		return EthTxPoolEvent{}, 0, fmt.Errorf("unexpected tx_hash length: %d (expected 32)", txHashLen)
	}
	offset := 8

	if len(data[offset:]) < 32 {
		return EthTxPoolEvent{}, 0, fmt.Errorf("data too short for tx_hash data: %d bytes", len(data[offset:]))
	}

	var txHash common.Hash
	copy(txHash[:], data[offset:offset+32])
	offset += 32

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

	fmt.Printf("DEBUG: decodeEventAction: variantIndex=%d, first_8_bytes=%x\n",
		variantIndex, data[:min(8, len(data))])

	switch EthTxPoolEventType(variantIndex) {
	case EventInsert:
		// Insert { address: Address, owned: bool, tx: TxEnvelope }
		// address: [length:8 bytes LE][20 bytes]
		// owned: [1 byte] (bool as u8)
		// tx: Vec<u8> [length:8 bytes][tx_bytes] (RLP-encoded transaction)

		if len(data[offset:]) < 8+20+1+8 {
			return nil, 0, fmt.Errorf("data too short for Insert action")
		}

		// Read Address length prefix (should be 20)
		addressLen := binary.LittleEndian.Uint64(data[offset : offset+8])
		if addressLen != 20 {
			return nil, 0, fmt.Errorf("unexpected address length: %d (expected 20)", addressLen)
		}
		offset += 8

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
		// Decode the reason enum (u32 variant + optional data)
		if len(data[offset:]) < 4 {
			return nil, 0, fmt.Errorf("data too short for Drop reason variant")
		}
		reasonVariant := binary.LittleEndian.Uint32(data[offset : offset+4])
		offset += 4

		var reasonStr string
		switch reasonVariant {
		case 0: // NotWellFormed(TransactionError) - nested enum, read u32
			if len(data[offset:]) < 4 {
				return nil, 0, fmt.Errorf("data too short for NotWellFormed nested enum")
			}
			offset += 4 // Skip nested enum variant
			reasonStr = "not well formed"
		case 1: // InvalidSignature
			reasonStr = "invalid signature"
		case 2: // NonceTooLow
			reasonStr = "nonce too low"
		case 3: // FeeTooLow
			reasonStr = "fee too low"
		case 4: // InsufficientBalance
			reasonStr = "insufficient balance"
		case 5: // ExistingHigherPriority
			reasonStr = "existing higher priority"
		case 6: // ReplacedByHigherPriority { replacement: TxHash }
			if len(data[offset:]) < 32 {
				return nil, 0, fmt.Errorf("data too short for ReplacedByHigherPriority hash")
			}
			offset += 32 // Skip replacement hash
			reasonStr = "replaced by higher priority"
		case 7: // PoolFull
			reasonStr = "pool full"
		case 8: // PoolNotReady
			reasonStr = "pool not ready"
		case 9: // Internal(EthTxPoolInternalDropReason) - nested enum, read u32
			if len(data[offset:]) < 4 {
				return nil, 0, fmt.Errorf("data too short for Internal nested enum")
			}
			offset += 4 // Skip nested enum variant
			reasonStr = "internal error"
		default:
			reasonStr = fmt.Sprintf("unknown reason %d", reasonVariant)
		}

		return DropAction{Reason: reasonStr}, offset, nil

	case EventEvict:
		// Evict { reason: EthTxPoolEvictReason }
		// Simple enum with only Expired(0) variant
		if len(data[offset:]) < 4 {
			return nil, 0, fmt.Errorf("data too short for Evict reason variant")
		}
		reasonVariant := binary.LittleEndian.Uint32(data[offset : offset+4])
		offset += 4

		var reasonStr string
		switch reasonVariant {
		case 0: // Expired
			reasonStr = "expired"
		default:
			reasonStr = fmt.Sprintf("unknown evict reason %d", reasonVariant)
		}

		return EvictAction{Reason: reasonStr}, offset, nil

	default:
		return nil, 0, fmt.Errorf("unknown event variant: %d", variantIndex)
	}
}
