package ipc

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

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

// DecodeEthTxPoolEvents decodes a slice of EthTxPoolEvent from RLP
func DecodeEthTxPoolEvents(data []byte) ([]EthTxPoolEvent, error) {
	var events []EthTxPoolEvent

	// The Rust side sends Vec<EthTxPoolEvent> encoded as RLP list
	var rawEvents [][]byte
	if err := rlp.DecodeBytes(data, &rawEvents); err != nil {
		return nil, err
	}

	for _, rawEvent := range rawEvents {
		event, err := decodeEthTxPoolEvent(rawEvent)
		if err != nil {
			continue // Skip malformed events
		}
		events = append(events, event)
	}

	return events, nil
}

// decodeEthTxPoolEvent decodes a single EthTxPoolEvent from RLP
func decodeEthTxPoolEvent(data []byte) (EthTxPoolEvent, error) {
	// EthTxPoolEvent is encoded as: [tx_hash, action]
	// where action is an enum encoded as: [variant_index, data...]

	var raw struct {
		TxHash     common.Hash
		ActionData []byte
	}

	if err := rlp.DecodeBytes(data, &raw); err != nil {
		return EthTxPoolEvent{}, err
	}

	// Decode action enum
	action, err := decodeEventAction(raw.ActionData)
	if err != nil {
		return EthTxPoolEvent{}, err
	}

	return EthTxPoolEvent{
		TxHash: raw.TxHash,
		Action: action,
	}, nil
}

// decodeEventAction decodes an EventAction from RLP
func decodeEventAction(data []byte) (EventAction, error) {
	// Enum is encoded as list: [variant_index, fields...]
	var rawAction []interface{}
	if err := rlp.DecodeBytes(data, &rawAction); err != nil {
		return nil, err
	}

	if len(rawAction) == 0 {
		return nil, rlp.EOL
	}

	// First element is variant index
	variantIndex, ok := rawAction[0].(uint64)
	if !ok {
		return nil, rlp.EOL
	}

	switch EthTxPoolEventType(variantIndex) {
	case EventInsert:
		// Insert { address, owned, tx }
		if len(rawAction) < 4 {
			return nil, rlp.EOL
		}

		addressBytes, _ := rawAction[1].([]byte)
		ownedUint, _ := rawAction[2].(uint64)
		txBytes, _ := rawAction[3].([]byte)

		var address common.Address
		copy(address[:], addressBytes)

		tx := new(types.Transaction)
		if err := tx.UnmarshalBinary(txBytes); err != nil {
			return nil, err
		}

		return InsertAction{
			Address: address,
			Owned:   ownedUint != 0,
			Tx:      tx,
		}, nil

	case EventCommit:
		return CommitAction{}, nil

	case EventDrop:
		// Drop { reason }
		if len(rawAction) < 2 {
			return DropAction{Reason: "unknown"}, nil
		}
		reason, _ := rawAction[1].(string)
		return DropAction{Reason: reason}, nil

	case EventEvict:
		// Evict { reason }
		if len(rawAction) < 2 {
			return EvictAction{Reason: "unknown"}, nil
		}
		reason, _ := rawAction[1].(string)
		return EvictAction{Reason: reason}, nil

	default:
		return nil, rlp.EOL
	}
}
