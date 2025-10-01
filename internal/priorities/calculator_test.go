package priorities

import (
	"math/big"
	"testing"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/types"
)

func TestCalculateTOBPriority(t *testing.T) {
	tests := []struct {
		name      string
		bidAmount *big.Int
		expected  [16]uint64
	}{
		{
			name:      "TOB with bid 1000",
			bidAmount: big.NewInt(1000),
			expected: [16]uint64{
				3,    // tier
				0,    // bid amount (positions 1-4, big-endian)
				0, 0,
				1000, // bid amount continues
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // rest unused
			},
		},
		{
			name:      "TOB with large bid",
			bidAmount: new(big.Int).Mul(big.NewInt(1e18), big.NewInt(100)), // 100 ETH in wei
			expected: [16]uint64{
				3, // tier
				0, 0, 0,
				1441151880758558720, // 100e18 in big-endian across uint64s
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateTOBPriority(tt.bidAmount)

			// Check tier
			if result[0] != 3 {
				t.Errorf("Expected tier 3, got %d", result[0])
			}

			// Decode and compare bid amount
			decoded := decodeBigIntFromSlice(result, 1)
			if decoded.Cmp(tt.bidAmount) != 0 {
				t.Errorf("Bid amount mismatch: expected %s, got %s", tt.bidAmount.String(), decoded.String())
			}
		})
	}
}

func TestCalculateOpportunityPriority(t *testing.T) {
	tests := []struct {
		name     string
		gasPrice *big.Int
	}{
		{
			name:     "Opportunity with gas price 50 gwei",
			gasPrice: big.NewInt(50e9),
		},
		{
			name:     "Opportunity with gas price 1 wei",
			gasPrice: big.NewInt(1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateOpportunityPriority(tt.gasPrice)

			// Check tier
			if result[0] != 2 {
				t.Errorf("Expected tier 2, got %d", result[0])
			}

			// Check tx type
			if result[5] != 1 {
				t.Errorf("Expected tx type 1 (opportunity), got %d", result[5])
			}

			// Decode and compare gas price
			decoded := decodeBigIntFromSlice(result, 1)
			if decoded.Cmp(tt.gasPrice) != 0 {
				t.Errorf("Gas price mismatch: expected %s, got %s", tt.gasPrice.String(), decoded.String())
			}
		})
	}
}

func TestCalculateBackrunPriority(t *testing.T) {
	tests := []struct {
		name       string
		bidAmount  *big.Int
		oppGasTip  *big.Int
	}{
		{
			name:      "Backrun with 1 ETH bid",
			bidAmount: big.NewInt(1e18),
			oppGasTip: big.NewInt(50e9),
		},
		{
			name:      "Backrun with small bid",
			bidAmount: big.NewInt(1000),
			oppGasTip: big.NewInt(1e9),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateBackrunPriority(tt.bidAmount, tt.oppGasTip)

			// Check tier
			if result[0] != 2 {
				t.Errorf("Expected tier 2, got %d", result[0])
			}

			// Check tx type
			if result[5] != 0 {
				t.Errorf("Expected tx type 0 (backrun bid), got %d", result[5])
			}

			// Decode and compare gas price
			decodedGas := decodeBigIntFromSlice(result, 1)
			if decodedGas.Cmp(tt.oppGasTip) != 0 {
				t.Errorf("Gas price mismatch: expected %s, got %s", tt.oppGasTip.String(), decodedGas.String())
			}

			// Decode and compare bid amount
			decodedBid := decodeBigIntFromSlice(result, 6)
			if decodedBid.Cmp(tt.bidAmount) != 0 {
				t.Errorf("Bid amount mismatch: expected %s, got %s", tt.bidAmount.String(), decodedBid.String())
			}
		})
	}
}

func TestPriorityOrdering(t *testing.T) {
	// Create various transactions with different priorities
	tob1 := CalculateTOBPriority(big.NewInt(1000))  // Higher bid
	tob2 := CalculateTOBPriority(big.NewInt(500))   // Lower bid

	opp1 := CalculateOpportunityPriority(big.NewInt(100e9))  // Higher gas price
	backrun1 := CalculateBackrunPriority(big.NewInt(2e18), big.NewInt(100e9))  // Higher bid, same gas price as opp1
	backrun2 := CalculateBackrunPriority(big.NewInt(1e18), big.NewInt(100e9))  // Lower bid, same gas price as opp1

	opp2 := CalculateOpportunityPriority(big.NewInt(50e9))  // Lower gas price
	backrun3 := CalculateBackrunPriority(big.NewInt(3e18), big.NewInt(50e9))  // Same gas price as opp2

	tests := []struct {
		name     string
		p1       [16]uint64
		p2       [16]uint64
		expected int // 1 if p1 > p2, -1 if p1 < p2, 0 if equal
	}{
		{
			name:     "TOB with higher bid > TOB with lower bid",
			p1:       tob1,
			p2:       tob2,
			expected: 1,
		},
		{
			name:     "Any TOB > Any backrun",
			p1:       tob2,
			p2:       opp1,
			expected: 1,
		},
		{
			name:     "Backrun opp > Backrun bid (same gas price)",
			p1:       opp1,
			p2:       backrun1,
			expected: 1,
		},
		{
			name:     "Backrun bid with higher bid > Backrun bid with lower bid (same gas price)",
			p1:       backrun1,
			p2:       backrun2,
			expected: 1,
		},
		{
			name:     "Backrun bunch with higher gas price > Backrun bunch with lower gas price",
			p1:       opp1,
			p2:       opp2,
			expected: 1,
		},
		{
			name:     "Backrun bunch with higher gas price > Lower gas price backrun (even with higher bid)",
			p1:       backrun2, // Lower bid but higher gas price (100e9)
			p2:       backrun3, // Higher bid but lower gas price (50e9)
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ComparePriorities(tt.p1, tt.p2)
			if result != tt.expected {
				t.Errorf("ComparePriorities mismatch: expected %d, got %d", tt.expected, result)
				t.Errorf("p1: %v", tt.p1[:6])
				t.Errorf("p2: %v", tt.p2[:6])
			}
		})
	}
}

func TestSortByPriority(t *testing.T) {
	// Create 3 TOB transactions with different bids
	tob1 := types.TxWithPriority{
		TxBytes:  []byte("tob_bid_3eth"),
		Priority: CalculateTOBPriority(big.NewInt(3e18)),
	}
	tob2 := types.TxWithPriority{
		TxBytes:  []byte("tob_bid_2eth"),
		Priority: CalculateTOBPriority(big.NewInt(2e18)),
	}
	tob3 := types.TxWithPriority{
		TxBytes:  []byte("tob_bid_1eth"),
		Priority: CalculateTOBPriority(big.NewInt(1e18)),
	}

	// Create backrun bunch 1: gas price 100 gwei, 1 backrun
	bunch1_opp := types.TxWithPriority{
		TxBytes:  []byte("bunch1_opp"),
		Priority: CalculateOpportunityPriority(big.NewInt(100e9)),
	}
	bunch1_backrun1 := types.TxWithPriority{
		TxBytes:  []byte("bunch1_backrun1"),
		Priority: CalculateBackrunPriority(big.NewInt(1e18), big.NewInt(100e9)),
	}

	// Create backrun bunch 2: gas price 80 gwei, 2 backruns
	bunch2_opp := types.TxWithPriority{
		TxBytes:  []byte("bunch2_opp"),
		Priority: CalculateOpportunityPriority(big.NewInt(80e9)),
	}
	bunch2_backrun1 := types.TxWithPriority{
		TxBytes:  []byte("bunch2_backrun1_high"),
		Priority: CalculateBackrunPriority(big.NewInt(2e18), big.NewInt(80e9)),
	}
	bunch2_backrun2 := types.TxWithPriority{
		TxBytes:  []byte("bunch2_backrun2_low"),
		Priority: CalculateBackrunPriority(big.NewInt(1e18), big.NewInt(80e9)),
	}

	// Create backrun bunch 3: gas price 50 gwei, 3 backruns
	bunch3_opp := types.TxWithPriority{
		TxBytes:  []byte("bunch3_opp"),
		Priority: CalculateOpportunityPriority(big.NewInt(50e9)),
	}
	bunch3_backrun1 := types.TxWithPriority{
		TxBytes:  []byte("bunch3_backrun1_high"),
		Priority: CalculateBackrunPriority(big.NewInt(3e18), big.NewInt(50e9)),
	}
	bunch3_backrun2 := types.TxWithPriority{
		TxBytes:  []byte("bunch3_backrun2_mid"),
		Priority: CalculateBackrunPriority(big.NewInt(2e18), big.NewInt(50e9)),
	}
	bunch3_backrun3 := types.TxWithPriority{
		TxBytes:  []byte("bunch3_backrun3_low"),
		Priority: CalculateBackrunPriority(big.NewInt(1e18), big.NewInt(50e9)),
	}

	// Create transactions in random order
	txs := []types.TxWithPriority{
		bunch2_backrun1,
		tob2,
		bunch3_backrun2,
		bunch1_opp,
		tob3,
		bunch3_backrun3,
		bunch2_opp,
		bunch1_backrun1,
		tob1,
		bunch3_opp,
		bunch2_backrun2,
		bunch3_backrun1,
	}

	SortByPriority(txs)

	// Expected order after sorting (highest priority first):
	// 1. All TOBs sorted by bid (highest to lowest)
	// 2. Backrun bunch 1 (100 gwei): opp, then backruns by bid
	// 3. Backrun bunch 2 (80 gwei): opp, then backruns by bid
	// 4. Backrun bunch 3 (50 gwei): opp, then backruns by bid
	expected := []string{
		"tob_bid_3eth",              // TOB tier: highest bid first
		"tob_bid_2eth",
		"tob_bid_1eth",
		"bunch1_opp",                // Bunch 1 (100 gwei): highest gas price
		"bunch1_backrun1",
		"bunch2_opp",                // Bunch 2 (80 gwei)
		"bunch2_backrun1_high",      // Higher bid first
		"bunch2_backrun2_low",
		"bunch3_opp",                // Bunch 3 (50 gwei): lowest gas price
		"bunch3_backrun1_high",      // Sorted by bid (highest to lowest)
		"bunch3_backrun2_mid",
		"bunch3_backrun3_low",
	}

	for i, tx := range txs {
		if string(tx.TxBytes) != expected[i] {
			t.Errorf("Position %d: expected %s, got %s", i, expected[i], string(tx.TxBytes))
			// Print full sorted order for debugging
			t.Logf("Full sorted order:")
			for j, debugTx := range txs {
				t.Logf("  [%d] %s: tier=%d, gas/bid[1]=%d, txType[5]=%d, bid[6]=%d",
					j, string(debugTx.TxBytes),
					debugTx.Priority[0], debugTx.Priority[1], debugTx.Priority[5], debugTx.Priority[6])
			}
			break
		}
	}
}

func TestFormatPriority(t *testing.T) {
	tests := []struct {
		name     string
		priority [16]uint64
		contains string // substring that should be in the formatted output
	}{
		{
			name:     "TOB transaction",
			priority: CalculateTOBPriority(big.NewInt(1000)),
			contains: "TopOfBlock",
		},
		{
			name:     "Backrun opportunity",
			priority: CalculateOpportunityPriority(big.NewInt(50e9)),
			contains: "opportunity",
		},
		{
			name:     "Backrun bid",
			priority: CalculateBackrunPriority(big.NewInt(1e18), big.NewInt(50e9)),
			contains: "bid=",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatPriority(tt.priority)
			if result == "" {
				t.Error("FormatPriority returned empty string")
			}
			t.Logf("Formatted: %s", result)
		})
	}
}
