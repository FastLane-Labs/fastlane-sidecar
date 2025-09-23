package priorities

import (
	"math/big"
	"sort"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/types"
)

// CalculateTOBPriority calculates priority for a top of block bid
func CalculateTOBPriority(bidAmount *big.Int) [16]uint64 {
	var priority [16]uint64

	// Set bid type to 2 (TOB)
	priority[0] = 2

	// Encode bid amount in last 4 elements
	encodeBidAmount(&priority, bidAmount)

	return priority
}

// CalculateOpportunityPriority calculates priority for an opportunity transaction
func CalculateOpportunityPriority(gasTip *big.Int) [16]uint64 {
	var priority [16]uint64

	// Set bid type to 1 (backrun)
	priority[0] = 1

	// Set transaction type to 1 (opportunity)
	priority[1] = 1

	// Encode gas tip in priority[8..12]
	encodeGasTip(&priority, gasTip)

	return priority
}

// CalculateBackrunPriority calculates priority for a backrun bid
func CalculateBackrunPriority(bidAmount *big.Int, oppGasTip *big.Int) [16]uint64 {
	var priority [16]uint64

	// Set bid type to 1 (backrun)
	priority[0] = 1

	// Set transaction type to 2 (backrun bid)
	priority[1] = 2

	// Encode opportunity tx gas tip in priority[8..12]
	encodeGasTip(&priority, oppGasTip)

	// Encode bid amount in priority[12..]
	encodeBidAmount(&priority, bidAmount)

	return priority
}

// SortByPriority sorts transactions by priority (highest first)
func SortByPriority(txsWithPriorities []types.TxWithPriority) {
	sort.Slice(txsWithPriorities, func(i, j int) bool {
		return ComparePriorities(txsWithPriorities[i].Priority, txsWithPriorities[j].Priority) > 0
	})
}

// SortBackrunBidsByPriority sorts backrun bids by bid amount (highest first)
func SortBackrunBidsByPriority(bids []*types.PooledTransaction, bidData map[string]*types.BidData) {
	sort.Slice(bids, func(i, j int) bool {
		bidDataI := bidData[bids[i].Hash.Hex()]
		bidDataJ := bidData[bids[j].Hash.Hex()]

		if bidDataI == nil || bidDataI.BidAmount == nil {
			return false
		}
		if bidDataJ == nil || bidDataJ.BidAmount == nil {
			return true
		}

		return bidDataI.BidAmount.Cmp(bidDataJ.BidAmount) > 0
	})
}

// ComparePriorities compares two priority arrays
// Returns: 1 if a > b, -1 if a < b, 0 if a == b
func ComparePriorities(a, b [16]uint64) int {
	for i := 0; i < 16; i++ {
		if a[i] > b[i] {
			return 1
		} else if a[i] < b[i] {
			return -1
		}
	}
	return 0
}

// encodeBidAmount encodes a big.Int bid amount into the last 4 elements of priority array
// Uses little-endian encoding across the 4 uint64s
func encodeBidAmount(priority *[16]uint64, amount *big.Int) {
	if amount == nil {
		return
	}

	// Convert to bytes (big-endian from big.Int)
	bytes := amount.Bytes()

	// Pad to 32 bytes if needed
	if len(bytes) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(bytes):], bytes)
		bytes = padded
	}

	// Take only the first 32 bytes if amount is larger than uint256
	if len(bytes) > 32 {
		bytes = bytes[len(bytes)-32:]
	}

	// Convert to 4 uint64s in little-endian order
	// bytes[0:8] -> priority[15], bytes[8:16] -> priority[14], bytes[16:24] -> priority[13], bytes[24:32] -> priority[12]
	for i := 0; i < 4; i++ {
		var val uint64
		for j := 0; j < 8; j++ {
			if i*8+j < len(bytes) {
				val |= uint64(bytes[31-(i*8+j)]) << (j * 8)
			}
		}
		priority[12+i] = val
	}
}

// encodeGasTip encodes a big.Int gas tip into priority[8..12]
// Uses little-endian encoding across the 4 uint64s
func encodeGasTip(priority *[16]uint64, gasTip *big.Int) {
	if gasTip == nil {
		return
	}

	// Convert to bytes (big-endian from big.Int)
	bytes := gasTip.Bytes()

	// Pad to 32 bytes if needed
	if len(bytes) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(bytes):], bytes)
		bytes = padded
	}

	// Take only the first 32 bytes if amount is larger than uint256
	if len(bytes) > 32 {
		bytes = bytes[len(bytes)-32:]
	}

	// Convert to 4 uint64s in little-endian order for priority[8..12]
	// bytes[0:8] -> priority[11], bytes[8:16] -> priority[10], bytes[16:24] -> priority[9], bytes[24:32] -> priority[8]
	for i := 0; i < 4; i++ {
		var val uint64
		for j := 0; j < 8; j++ {
			if i*8+j < len(bytes) {
				val |= uint64(bytes[31-(i*8+j)]) << (j * 8)
			}
		}
		priority[8+i] = val
	}
}

// FormatPriority returns a human-readable string representation of a priority
func FormatPriority(priority [16]uint64) string {
	bidType := priority[0]

	switch bidType {
	case 2:
		bidAmount := decodeBidAmount(priority)
		return "TopOfBlock(bid=" + bidAmount.String() + ")"
	case 1:
		txType := priority[1]
		switch txType {
		case 1:
			gasTip := decodeGasTip(priority)
			return "Backrun(opportunity, gasTip=" + gasTip.String() + ")"
		case 2:
			bidAmount := decodeBidAmount(priority)
			gasTip := decodeGasTip(priority)
			return "Backrun(bid=" + bidAmount.String() + ", oppGasTip=" + gasTip.String() + ")"
		default:
			return "Backrun(unknown_type=" + string(rune(txType)) + ")"
		}
	default:
		return "Unknown(type=" + string(rune(bidType)) + ")"
	}
}

// decodeBidAmount decodes the bid amount from the last 4 elements of priority array
func decodeBidAmount(priority [16]uint64) *big.Int {
	// Extract 4 uint64s and convert to bytes
	bytes := make([]byte, 32)

	for i := 0; i < 4; i++ {
		val := priority[12+i]
		for j := 0; j < 8; j++ {
			bytes[31-(i*8+j)] = byte(val >> (j * 8))
		}
	}

	return new(big.Int).SetBytes(bytes)
}

// decodeGasTip decodes the gas tip from priority[8..12]
func decodeGasTip(priority [16]uint64) *big.Int {
	// Extract 4 uint64s and convert to bytes
	bytes := make([]byte, 32)

	for i := 0; i < 4; i++ {
		val := priority[8+i]
		for j := 0; j < 8; j++ {
			bytes[31-(i*8+j)] = byte(val >> (j * 8))
		}
	}

	return new(big.Int).SetBytes(bytes)
}