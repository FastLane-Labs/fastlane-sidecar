package priorities

import (
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/types"
)

// CalculateTOBPriority calculates priority for a top of block bid
// Priority layout (U256, 256 bits):
// Bit 255 (MSB): 1
// Bits 254-128: 0 (127 bits of padding)
// Bits 127-0: bid amount (128 bits for bid)
func CalculateTOBPriority(bidAmount *big.Int) *big.Int {
	priority := new(big.Int)

	// Set bit 255 to 1 (TOB marker)
	priority.SetBit(priority, 255, 1)

	// Mask bid to 128 bits (ensure it doesn't overflow)
	mask := new(big.Int)
	mask.SetBit(mask, 128, 1)
	mask.Sub(mask, big.NewInt(1)) // mask = 2^128 - 1

	maskedBid := new(big.Int).And(bidAmount, mask)

	// OR the bid amount into bits 127-0
	priority.Or(priority, maskedBid)

	return priority
}

// CalculateOpportunityPriority calculates priority for an opportunity transaction
// Priority layout (U256, 256 bits):
// Bit 255: 0 (backrun marker)
// Bits 254-129: backrun_id (126 bits from txHash)
// Bit 128: 1 (opportunity marker)
// Bits 127-0: 0 (padding)
func CalculateOpportunityPriority(txHash common.Hash) *big.Int {
	priority := new(big.Int)

	// Extract first 126 bits of txHash as backrun_id
	backrunID := extractBackrunID(txHash)

	// Shift backrun_id to bits 254-129 (shift left by 129)
	shiftedID := new(big.Int).Lsh(backrunID, 129)
	priority.Or(priority, shiftedID)

	// Set bit 128 to 1 (opportunity marker)
	priority.SetBit(priority, 128, 1)

	// Bits 127-0 are already 0

	return priority
}

// CalculateBackrunPriority calculates priority for a backrun bid
// Priority layout (U256, 256 bits):
// Bit 255: 0 (backrun marker)
// Bits 254-129: backrun_id (126 bits from opportunity txHash)
// Bit 128: 0 (bid marker)
// Bits 127-0: bid amount (128 bits)
func CalculateBackrunPriority(bidAmount *big.Int, oppTxHash common.Hash) *big.Int {
	priority := new(big.Int)

	// Extract first 126 bits of opportunity txHash as backrun_id
	backrunID := extractBackrunID(oppTxHash)

	// Shift backrun_id to bits 254-129 (shift left by 129)
	shiftedID := new(big.Int).Lsh(backrunID, 129)
	priority.Or(priority, shiftedID)

	// Bit 128 is already 0 (bid marker)

	// Mask bid to 128 bits
	mask := new(big.Int)
	mask.SetBit(mask, 128, 1)
	mask.Sub(mask, big.NewInt(1)) // mask = 2^128 - 1

	maskedBid := new(big.Int).And(bidAmount, mask)

	// OR the bid amount into bits 127-0
	priority.Or(priority, maskedBid)

	return priority
}

// extractBackrunID extracts the first 126 bits of a transaction hash as backrun_id
func extractBackrunID(txHash common.Hash) *big.Int {
	// Get hash bytes
	hashBytes := txHash.Bytes()

	// Convert to big.Int
	fullHash := new(big.Int).SetBytes(hashBytes)

	// Shift right by (256 - 126) = 130 bits to get the top 126 bits
	backrunID := new(big.Int).Rsh(fullHash, 130)

	return backrunID
}

// ComparePriorities compares two U256 priorities
// Returns: 1 if a > b, -1 if a < b, 0 if a == b
func ComparePriorities(a, b *big.Int) int {
	return a.Cmp(b)
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

// FormatPriority returns a human-readable string representation of a U256 priority
func FormatPriority(priority *big.Int) string {
	if priority == nil {
		return "Invalid(nil)"
	}

	// Check bit 255 to determine if TOB or backrun
	if priority.Bit(255) == 1 {
		// TOB transaction
		// Extract bid from bits 127-0
		mask := new(big.Int)
		mask.SetBit(mask, 128, 1)
		mask.Sub(mask, big.NewInt(1)) // mask = 2^128 - 1
		bid := new(big.Int).And(priority, mask)
		return "TopOfBlock(bid=" + bid.String() + ")"
	} else {
		// Backrun transaction
		// Check bit 128 to determine if opportunity or bid
		if priority.Bit(128) == 1 {
			// Opportunity transaction
			// Extract backrun_id from bits 254-129
			backrunID := new(big.Int).Rsh(priority, 129)
			return "Backrun(opportunity, backrun_id=" + backrunID.String() + ")"
		} else {
			// Backrun bid
			// Extract backrun_id from bits 254-129
			backrunID := new(big.Int).Rsh(priority, 129)

			// Extract bid from bits 127-0
			mask := new(big.Int)
			mask.SetBit(mask, 128, 1)
			mask.Sub(mask, big.NewInt(1)) // mask = 2^128 - 1
			bid := new(big.Int).And(priority, mask)

			return "Backrun(bid=" + bid.String() + ", backrun_id=" + backrunID.String() + ")"
		}
	}
}
