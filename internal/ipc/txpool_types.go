package ipc

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
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
	Address       common.Address
	Owned         bool
	Tx            *types.Transaction // Full transaction envelope (for go-ethereum processing)
	OriginalTxRLP []byte             // Original RLP bytes from alloy (for forwarding via IPC)
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
// This must match Rust's struct exactly for RLP encoding/decoding
type EthTxPoolIpcTx struct {
	TxRLP     []byte   // Original alloy RLP-encoded transaction bytes (will be decoded as raw bytes by RLP)
	Priority  *big.Int // U256 priority value
	ExtraData []byte   // Optional extra data
}

// RLP encoding/decoding helpers

// EncodeRLP implements custom RLP encoding to match Rust's alloy_rlp format
// Based on testing against Rust alloy_rlp::encode(EthTxPoolIpcTx)
func (tx *EthTxPoolIpcTx) EncodeRLP(w io.Writer) error {
	var buf bytes.Buffer

	// Write tx RLP bytes directly (TxEnvelope network encoding)
	buf.Write(tx.TxRLP)

	// Encode priority as RLP big int
	priorityRLP, err := rlp.EncodeToBytes(tx.Priority)
	if err != nil {
		return fmt.Errorf("failed to encode priority: %w", err)
	}
	buf.Write(priorityRLP)

	// Encode extra data
	// CRITICAL: Rust's alloy_rlp encodes empty Vec<u8> as 0xc0 (empty list), not 0x80 (empty string)
	if len(tx.ExtraData) == 0 {
		buf.WriteByte(0xc0) // Empty list in RLP
	} else {
		extraDataRLP, err := rlp.EncodeToBytes(tx.ExtraData)
		if err != nil {
			return fmt.Errorf("failed to encode extra_data: %w", err)
		}
		buf.Write(extraDataRLP)
	}

	// Wrap everything in RLP list header
	payload := buf.Bytes()
	var result bytes.Buffer
	if err := encodeRLPListHeader(&result, len(payload)); err != nil {
		return err
	}
	result.Write(payload)

	// Write to output
	_, err = w.Write(result.Bytes())
	return err
}

// encodeRLPListHeader writes an RLP list header for the given payload length
func encodeRLPListHeader(w io.Writer, length int) error {
	if length < 56 {
		return binary.Write(w, binary.BigEndian, uint8(0xc0+length))
	}
	lenBytes := encodeLength(length)
	if err := binary.Write(w, binary.BigEndian, uint8(0xf7+len(lenBytes))); err != nil {
		return err
	}
	_, err := w.Write(lenBytes)
	return err
}

// encodeLength encodes an integer as big-endian bytes (minimum representation)
func encodeLength(n int) []byte {
	if n == 0 {
		return []byte{0}
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte(n & 0xff)}, buf...)
		n >>= 8
	}
	return buf
}

// DecodeRLP implements custom RLP decoding to match Rust's alloy_rlp format
func (tx *EthTxPoolIpcTx) DecodeRLP(s *rlp.Stream) error {
	// Decode as standard RLP list of [bytes, uint, bytes]
	var temp struct {
		TxRLP     []byte
		Priority  *big.Int
		ExtraData []byte
	}

	if err := s.Decode(&temp); err != nil {
		return err
	}

	tx.TxRLP = temp.TxRLP
	tx.Priority = temp.Priority
	tx.ExtraData = temp.ExtraData

	return nil
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

		// Keep a copy of the original alloy RLP bytes for forwarding via IPC
		originalTxRLP := make([]byte, len(txBytes))
		copy(originalTxRLP, txBytes)

		// Try to decode RLP-encoded transaction for go-ethereum processing
		// Alloy's TxEnvelope RLP encoding is compatible with go-ethereum's RLP decoder
		tx := new(types.Transaction)

		// First try UnmarshalBinary (for EIP-2718 typed transactions)
		if err := tx.UnmarshalBinary(txBytes); err != nil {
			// If that fails, try direct RLP decoding
			if err := rlp.DecodeBytes(txBytes, tx); err != nil {
				return nil, 0, fmt.Errorf("failed to decode transaction (tried both UnmarshalBinary and RLP): %w", err)
			}
		}

		return InsertAction{
			Address:       address,
			Owned:         owned,
			Tx:            tx,
			OriginalTxRLP: originalTxRLP,
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
