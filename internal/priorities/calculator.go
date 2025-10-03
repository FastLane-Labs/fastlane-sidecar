package priorities

import (
	"math/big"
	"sort"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/types"
)

// CalculateTOBPriority calculates priority for a top of block bid
// Priority layout (lexicographic ordering, higher = higher priority):
// [0]: tier = 3 (TOB - highest)
// [1-4]: bid amount (higher bid = higher priority)
// [5-15]: unused (0)
func CalculateTOBPriority(bidAmount *big.Int) [16]uint64 {
	var priority [16]uint64

	// Set tier to 3 (TOB - highest priority)
	priority[0] = 3

	// Encode bid amount in positions 1-4
	encodeBigIntToSlice(&priority, 1, bidAmount)

	return priority
}

// CalculateOpportunityPriority calculates priority for an opportunity transaction
// Priority layout:
// [0]: tier = 2 (backrun)
// [1-4]: gas price (higher gas price = higher priority)
// [5]: tx type = 1 (opportunity - higher than backrun bid)
// [6-15]: unused (0)
func CalculateOpportunityPriority(gasTip *big.Int) [16]uint64 {
	var priority [16]uint64

	// Set tier to 2 (backrun)
	priority[0] = 2

	// Encode gas price in positions 1-4
	encodeBigIntToSlice(&priority, 1, gasTip)

	// Set tx type to 1 (opportunity - comes before backrun bids)
	priority[5] = 1

	return priority
}

// CalculateBackrunPriority calculates priority for a backrun bid
// Priority layout:
// [0]: tier = 2 (backrun)
// [1-4]: gas price (same as opportunity tx - for grouping)
// [5]: tx type = 0 (backrun bid - lower than opportunity)
// [6-9]: bid amount (higher bid = higher priority)
// [10-15]: unused (0)
func CalculateBackrunPriority(bidAmount *big.Int, oppGasTip *big.Int) [16]uint64 {
	var priority [16]uint64

	// Set tier to 2 (backrun)
	priority[0] = 2

	// Encode opportunity tx gas price in positions 1-4
	encodeBigIntToSlice(&priority, 1, oppGasTip)

	// Set tx type to 0 (backrun bid - comes after opportunity)
	priority[5] = 0

	// Encode bid amount in positions 6-9
	encodeBigIntToSlice(&priority, 6, bidAmount)

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

// encodeBigIntToSlice encodes a big.Int into 4 consecutive uint64 slots starting at offset
// The value is encoded big-endian across the 4 uint64s for proper lexicographic comparison
// offset specifies where to start writing (e.g., 1 for positions 1-4, 6 for positions 6-9)
func encodeBigIntToSlice(priority *[16]uint64, offset int, value *big.Int) {
	if value == nil || offset < 0 || offset+4 > 16 {
		return
	}

	// Convert to bytes (big-endian from big.Int)
	bytes := value.Bytes()

	// Pad to 32 bytes if needed
	if len(bytes) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(bytes):], bytes)
		bytes = padded
	}

	// Take only the last 32 bytes if value is larger than uint256
	if len(bytes) > 32 {
		bytes = bytes[len(bytes)-32:]
	}

	// Convert to 4 uint64s in big-endian order for proper lexicographic comparison
	// bytes[0:8] -> priority[offset], bytes[8:16] -> priority[offset+1], etc.
	for i := 0; i < 4; i++ {
		var val uint64
		for j := 0; j < 8; j++ {
			val = (val << 8) | uint64(bytes[i*8+j])
		}
		priority[offset+i] = val
	}
}

// FormatPriority returns a human-readable string representation of a priority
func FormatPriority(priority [16]uint64) string {
	tier := priority[0]

	switch tier {
	case 3:
		// TOB transaction
		bidAmount := decodeBigIntFromSlice(priority, 1)
		return "TopOfBlock(bid=" + bidAmount.String() + ")"
	case 2:
		// Backrun transaction
		txType := priority[5]
		gasPrice := decodeBigIntFromSlice(priority, 1)
		if txType == 1 {
			// Opportunity tx
			return "Backrun(opportunity, gasPrice=" + gasPrice.String() + ")"
		} else {
			// Backrun bid
			bidAmount := decodeBigIntFromSlice(priority, 6)
			return "Backrun(bid=" + bidAmount.String() + ", oppGasPrice=" + gasPrice.String() + ")"
		}
	case 0:
		return "Normal"
	default:
		return "Unknown(tier=" + string(rune(tier)) + ")"
	}
}

// decodeBigIntFromSlice decodes a big.Int from 4 consecutive uint64 slots starting at offset
func decodeBigIntFromSlice(priority [16]uint64, offset int) *big.Int {
	if offset < 0 || offset+4 > 16 {
		return big.NewInt(0)
	}

	// Extract 4 uint64s and convert to bytes (big-endian)
	bytes := make([]byte, 32)

	for i := 0; i < 4; i++ {
		val := priority[offset+i]
		for j := 0; j < 8; j++ {
			bytes[i*8+j] = byte(val >> (56 - j*8))
		}
	}

	return new(big.Int).SetBytes(bytes)
}
