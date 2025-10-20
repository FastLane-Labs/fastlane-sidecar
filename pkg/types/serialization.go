package types

import (
	"encoding/binary"
)

// SerializeTxWithPriority serializes TxWithPriority using bincode-compatible format
// Bincode format for struct: [field1][field2]...
// For Vec<u8>: [length:8 bytes little-endian][data]
// For [u64; 16]: [u64:8 bytes little-endian] x 16
func SerializeTxWithPriority(tx TxWithPriority) []byte {
	result := make([]byte, 0)

	// Serialize tx_bytes as Vec<u8> (length + data)
	txBytesLen := uint64(len(tx.TxBytes))
	lenBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(lenBytes, txBytesLen)
	result = append(result, lenBytes...)
	result = append(result, tx.TxBytes...)

	// Serialize priority array as [u64; 16]
	for _, p := range tx.Priority {
		pBytes := make([]byte, 8)
		binary.LittleEndian.PutUint64(pBytes, p)
		result = append(result, pBytes...)
	}

	return result
}

// ParseFastlaneMessage parses a bincode-encoded FastlaneMessage enum
// Returns message type and structured data
func ParseFastlaneMessage(msgData []byte) (string, interface{}) {
	// Basic bincode parsing for FastlaneMessage enum
	// Bincode encodes Rust enums as: [variant_index: u32][variant_data...]

	if len(msgData) < 4 {
		return "Unknown", msgData
	}

	// Read variant index (little-endian u32)
	variantIndex := binary.LittleEndian.Uint32(msgData[:4])
	data := msgData[4:]

	switch variantIndex {
	case 0: // TxAdded variant
		// TxAdded { tx_bytes: Vec<u8>, timestamp_ms: u64 }
		// Format: [vec_len:8][tx_bytes:vec_len][timestamp_ms:8]
		if len(data) < 8 {
			return "Unknown", msgData
		}
		txBytesLen := binary.LittleEndian.Uint64(data[:8])
		if len(data) < 8+int(txBytesLen)+8 {
			return "Unknown", msgData
		}
		txBytes := data[8 : 8+txBytesLen]

		// Extract timestamp_ms (8 bytes after tx_bytes)
		timestampOffset := 8 + txBytesLen
		timestampMs := binary.LittleEndian.Uint64(data[timestampOffset : timestampOffset+8])

		return "TxAdded", TxAdded{
			TxBytes:     txBytes,
			TimestampMs: timestampMs,
		}

	case 1: // TxDropped variant
		// TxDropped { tx_hash: [u8; 32] }
		// Fixed-size array is encoded directly
		if len(data) < 32 {
			return "Unknown", msgData
		}
		var txHash [32]byte
		copy(txHash[:], data[:32])
		return "TxDropped", TxDropped{
			TxHash: txHash,
		}

	case 2: // Heartbeat variant (unit variant, no data)
		return "Heartbeat", nil

	default:
		return "Unknown", msgData
	}
}
