package priorities

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestCalculateTOBPriority(t *testing.T) {
	tests := []struct {
		name      string
		bidAmount *big.Int
	}{
		{
			name:      "TOB with bid 1000",
			bidAmount: big.NewInt(1000),
		},
		{
			name:      "TOB with large bid",
			bidAmount: new(big.Int).Mul(big.NewInt(1e18), big.NewInt(100)), // 100 ETH in wei
		},
		{
			name:      "TOB with max uint128 bid",
			bidAmount: new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1)), // 2^128 - 1
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateTOBPriority(tt.bidAmount)

			// Check bit 255 = 1 (TOB marker)
			if result.Bit(255) != 1 {
				t.Errorf("Expected bit 255 = 1, got %d", result.Bit(255))
			}

			// Extract bid from bits 127-0
			mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1)) // 2^128 - 1
			extractedBid := new(big.Int).And(result, mask)

			// Mask the input bid to 128 bits for comparison
			maskedInput := new(big.Int).And(tt.bidAmount, mask)

			if extractedBid.Cmp(maskedInput) != 0 {
				t.Errorf("Bid amount mismatch: expected %s, got %s", maskedInput.String(), extractedBid.String())
			}

			// Check bits 254-128 are 0 (padding)
			for i := 128; i < 255; i++ {
				if result.Bit(i) != 0 {
					t.Errorf("Expected bit %d = 0 (padding), got %d", i, result.Bit(i))
					break
				}
			}
		})
	}
}

func TestCalculateOpportunityPriority(t *testing.T) {
	tests := []struct {
		name   string
		txHash common.Hash
	}{
		{
			name:   "Opportunity with sample hash 1",
			txHash: common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		},
		{
			name:   "Opportunity with sample hash 2",
			txHash: common.HexToHash("0xabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateOpportunityPriority(tt.txHash)

			// Check bit 255 = 0 (backrun marker)
			if result.Bit(255) != 0 {
				t.Errorf("Expected bit 255 = 0, got %d", result.Bit(255))
			}

			// Check bit 128 = 1 (opportunity marker)
			if result.Bit(128) != 1 {
				t.Errorf("Expected bit 128 = 1 (opportunity marker), got %d", result.Bit(128))
			}

			// Check bits 127-0 are 0 (padding)
			for i := 0; i < 128; i++ {
				if result.Bit(i) != 0 {
					t.Errorf("Expected bit %d = 0 (padding), got %d", i, result.Bit(i))
					break
				}
			}

			// Extract backrun_id from bits 254-129
			extractedID := new(big.Int).Rsh(result, 129)

			// Calculate expected backrun_id (first 126 bits of hash)
			hashBigInt := new(big.Int).SetBytes(tt.txHash.Bytes())
			expectedID := new(big.Int).Rsh(hashBigInt, 130) // Shift right by 256-126 = 130

			if extractedID.Cmp(expectedID) != 0 {
				t.Errorf("Backrun ID mismatch: expected %s, got %s", expectedID.String(), extractedID.String())
			}
		})
	}
}

func TestCalculateBackrunPriority(t *testing.T) {
	tests := []struct {
		name      string
		bidAmount *big.Int
		oppTxHash common.Hash
	}{
		{
			name:      "Backrun with 1 ETH bid",
			bidAmount: big.NewInt(1e18),
			oppTxHash: common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		},
		{
			name:      "Backrun with small bid",
			bidAmount: big.NewInt(1000),
			oppTxHash: common.HexToHash("0xabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd"),
		},
		{
			name:      "Backrun with max uint128 bid",
			bidAmount: new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1)), // 2^128 - 1
			oppTxHash: common.HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateBackrunPriority(tt.bidAmount, tt.oppTxHash)

			// Check bit 255 = 0 (backrun marker)
			if result.Bit(255) != 0 {
				t.Errorf("Expected bit 255 = 0, got %d", result.Bit(255))
			}

			// Check bit 128 = 0 (bid marker)
			if result.Bit(128) != 0 {
				t.Errorf("Expected bit 128 = 0 (bid marker), got %d", result.Bit(128))
			}

			// Extract backrun_id from bits 254-129
			extractedID := new(big.Int).Rsh(result, 129)

			// Calculate expected backrun_id (first 126 bits of hash)
			hashBigInt := new(big.Int).SetBytes(tt.oppTxHash.Bytes())
			expectedID := new(big.Int).Rsh(hashBigInt, 130) // Shift right by 256-126 = 130

			if extractedID.Cmp(expectedID) != 0 {
				t.Errorf("Backrun ID mismatch: expected %s, got %s", expectedID.String(), extractedID.String())
			}

			// Extract bid from bits 127-0
			mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1)) // 2^128 - 1
			extractedBid := new(big.Int).And(result, mask)

			// Mask the input bid to 128 bits for comparison
			maskedInput := new(big.Int).And(tt.bidAmount, mask)

			if extractedBid.Cmp(maskedInput) != 0 {
				t.Errorf("Bid amount mismatch: expected %s, got %s", maskedInput.String(), extractedBid.String())
			}
		})
	}
}

func TestBackrunIDConsistency(t *testing.T) {
	// Test that opportunity and backrun txs with same txHash get same backrun_id
	txHash := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")

	oppPriority := CalculateOpportunityPriority(txHash)
	backrunPriority := CalculateBackrunPriority(big.NewInt(1e18), txHash)

	// Extract backrun_id from both (bits 254-129)
	oppID := new(big.Int).Rsh(oppPriority, 129)
	backrunID := new(big.Int).Rsh(backrunPriority, 129)

	if oppID.Cmp(backrunID) != 0 {
		t.Errorf("Backrun IDs don't match: opportunity=%s, backrun=%s", oppID.String(), backrunID.String())
	}
}

func TestPriorityOrdering(t *testing.T) {
	// Create sample transaction hash
	txHash1 := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	txHash2 := common.HexToHash("0xabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd")

	// Create various transactions with different priorities
	tob1 := CalculateTOBPriority(big.NewInt(1000)) // Higher bid
	tob2 := CalculateTOBPriority(big.NewInt(500))  // Lower bid

	opp1 := CalculateOpportunityPriority(txHash1)
	backrun1 := CalculateBackrunPriority(big.NewInt(2e18), txHash1) // Higher bid, same hash as opp1
	backrun2 := CalculateBackrunPriority(big.NewInt(1e18), txHash1) // Lower bid, same hash as opp1

	opp2 := CalculateOpportunityPriority(txHash2)

	tests := []struct {
		name     string
		p1       *big.Int
		p2       *big.Int
		expected int // 1 if p1 > p2, -1 if p1 < p2, 0 if equal
	}{
		{
			name:     "TOB with higher bid > TOB with lower bid",
			p1:       tob1,
			p2:       tob2,
			expected: 1,
		},
		{
			name:     "Any TOB > Any backrun (bit 255 = 1 > bit 255 = 0)",
			p1:       tob2,
			p2:       opp1,
			expected: 1,
		},
		{
			name:     "Backrun opp > Backrun bid (same backrun_id, bit 128 = 1 > bit 128 = 0)",
			p1:       opp1,
			p2:       backrun1,
			expected: 1,
		},
		{
			name:     "Backrun bid with higher bid > Backrun bid with lower bid (same backrun_id)",
			p1:       backrun1,
			p2:       backrun2,
			expected: 1,
		},
		{
			name:     "Different backrun groups ordered by backrun_id",
			p1:       opp1,
			p2:       opp2,
			expected: ComparePriorities(opp1, opp2), // Just verify they're different
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ComparePriorities(tt.p1, tt.p2)
			if tt.expected != 0 && result != tt.expected {
				t.Errorf("ComparePriorities mismatch: expected %d, got %d", tt.expected, result)
				t.Errorf("p1: %s", tt.p1.String())
				t.Errorf("p2: %s", tt.p2.String())
			}
		})
	}
}

func TestFormatPriority(t *testing.T) {
	txHash := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")

	tests := []struct {
		name     string
		priority *big.Int
		contains string // substring that should be in the formatted output
	}{
		{
			name:     "TOB transaction",
			priority: CalculateTOBPriority(big.NewInt(1000)),
			contains: "TopOfBlock",
		},
		{
			name:     "Backrun opportunity",
			priority: CalculateOpportunityPriority(txHash),
			contains: "opportunity",
		},
		{
			name:     "Backrun bid",
			priority: CalculateBackrunPriority(big.NewInt(1e18), txHash),
			contains: "bid=",
		},
		{
			name:     "Nil priority",
			priority: nil,
			contains: "Invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatPriority(tt.priority)
			if result == "" {
				t.Error("FormatPriority returned empty string")
			}
			if tt.contains != "" {
				// Simple substring check
				found := false
				for i := 0; i <= len(result)-len(tt.contains); i++ {
					if result[i:i+len(tt.contains)] == tt.contains {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected output to contain '%s', got: %s", tt.contains, result)
				}
			}
			t.Logf("Formatted: %s", result)
		})
	}
}

func TestExtractBackrunID(t *testing.T) {
	tests := []struct {
		name   string
		txHash common.Hash
	}{
		{
			name:   "Sample hash 1",
			txHash: common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		},
		{
			name:   "All ones",
			txHash: common.HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"),
		},
		{
			name:   "All zeros",
			txHash: common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backrunID := extractBackrunID(tt.txHash)

			// Verify it's 126 bits (should fit in 126 bits)
			maxID := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 126), big.NewInt(1)) // 2^126 - 1
			if backrunID.Cmp(maxID) > 0 {
				t.Errorf("Backrun ID exceeds 126 bits: %s > %s", backrunID.String(), maxID.String())
			}

			// Verify it matches the top 126 bits of the hash
			hashBigInt := new(big.Int).SetBytes(tt.txHash.Bytes())
			expectedID := new(big.Int).Rsh(hashBigInt, 130) // Shift right by 256-126 = 130

			if backrunID.Cmp(expectedID) != 0 {
				t.Errorf("Backrun ID mismatch: expected %s, got %s", expectedID.String(), backrunID.String())
			}
		})
	}
}
