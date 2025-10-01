package processor

import (
	"math/big"
	"testing"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/types"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
)

const (
	// Dummy test configuration
	testFastlaneContract = "0x1234567890123456789012345678901234567890"
	testTOBMethodSig     = "0xaabbccdd"
	testBackrunMethodSig = "0x11223344"
)

func TestNewFilter(t *testing.T) {
	tests := []struct {
		name             string
		contractAddr     string
		tobSig           string
		backrunSig       string
		expectError      bool
		errorContains    string
	}{
		{
			name:          "Valid configuration",
			contractAddr:  testFastlaneContract,
			tobSig:        testTOBMethodSig,
			backrunSig:    testBackrunMethodSig,
			expectError:   false,
		},
		{
			name:          "Missing contract address",
			contractAddr:  "",
			tobSig:        testTOBMethodSig,
			backrunSig:    testBackrunMethodSig,
			expectError:   true,
			errorContains: "contract address is required",
		},
		{
			name:          "Missing TOB signature",
			contractAddr:  testFastlaneContract,
			tobSig:        "",
			backrunSig:    testBackrunMethodSig,
			expectError:   true,
			errorContains: "TOB method signature is required",
		},
		{
			name:          "Missing backrun signature",
			contractAddr:  testFastlaneContract,
			tobSig:        testTOBMethodSig,
			backrunSig:    "",
			expectError:   true,
			errorContains: "backrun method signature is required",
		},
		{
			name:          "Invalid TOB signature format",
			contractAddr:  testFastlaneContract,
			tobSig:        "aabbccdd", // missing 0x prefix
			backrunSig:    testBackrunMethodSig,
			expectError:   true,
			errorContains: "invalid TOB method signature format",
		},
		{
			name:          "TOB signature too short",
			contractAddr:  testFastlaneContract,
			tobSig:        "0xaabb", // only 2 bytes
			backrunSig:    testBackrunMethodSig,
			expectError:   true,
			errorContains: "invalid TOB method signature format", // Caught by length check in format validation
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := NewFilter(tt.contractAddr, tt.tobSig, tt.backrunSig)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				} else if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("Error should contain '%s', got: %s", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if filter == nil {
					t.Error("Expected filter to be non-nil")
				}
			}
		})
	}
}

func TestClassifyTOBTransaction(t *testing.T) {
	filter, err := NewFilter(testFastlaneContract, testTOBMethodSig, testBackrunMethodSig)
	if err != nil {
		t.Fatalf("Failed to create filter: %v", err)
	}

	// Create a TOB bid transaction
	bidAmount := big.NewInt(1e18) // 1 ETH
	calldata := buildTOBCalldata(bidAmount)

	tx := ethTypes.NewTransaction(
		0,
		common.HexToAddress(testFastlaneContract),
		big.NewInt(0),
		100000,
		big.NewInt(1e9),
		calldata,
	)

	txType, bidData := filter.ClassifyTransaction(tx)

	if txType != types.TOBBid {
		t.Errorf("Expected TOBBid, got %v", txType)
	}

	if bidData == nil {
		t.Fatal("Expected bidData to be non-nil")
	}

	if bidData.BidAmount.Cmp(bidAmount) != 0 {
		t.Errorf("Expected bid amount %s, got %s", bidAmount.String(), bidData.BidAmount.String())
	}
}

func TestClassifyBackrunTransaction(t *testing.T) {
	filter, err := NewFilter(testFastlaneContract, testTOBMethodSig, testBackrunMethodSig)
	if err != nil {
		t.Fatalf("Failed to create filter: %v", err)
	}

	// Create a backrun bid transaction
	bidAmount := big.NewInt(5e17) // 0.5 ETH
	targetHash := common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	calldata := buildBackrunCalldata(targetHash, bidAmount)

	tx := ethTypes.NewTransaction(
		0,
		common.HexToAddress(testFastlaneContract),
		big.NewInt(0),
		100000,
		big.NewInt(1e9),
		calldata,
	)

	txType, bidData := filter.ClassifyTransaction(tx)

	if txType != types.BackrunBid {
		t.Errorf("Expected BackrunBid, got %v", txType)
	}

	if bidData == nil {
		t.Fatal("Expected bidData to be non-nil")
	}

	if bidData.BidAmount.Cmp(bidAmount) != 0 {
		t.Errorf("Expected bid amount %s, got %s", bidAmount.String(), bidData.BidAmount.String())
	}

	if bidData.TargetTxHash == nil {
		t.Fatal("Expected TargetTxHash to be non-nil")
	}

	if *bidData.TargetTxHash != targetHash {
		t.Errorf("Expected target hash %s, got %s", targetHash.Hex(), bidData.TargetTxHash.Hex())
	}
}

func TestClassifyNormalTransaction(t *testing.T) {
	filter, err := NewFilter(testFastlaneContract, testTOBMethodSig, testBackrunMethodSig)
	if err != nil {
		t.Fatalf("Failed to create filter: %v", err)
	}

	// Create a normal transaction (to different address)
	tx := ethTypes.NewTransaction(
		0,
		common.HexToAddress("0x9999999999999999999999999999999999999999"),
		big.NewInt(1e18),
		21000,
		big.NewInt(1e9),
		nil,
	)

	txType, bidData := filter.ClassifyTransaction(tx)

	if txType != types.NormalTransaction {
		t.Errorf("Expected NormalTransaction, got %v", txType)
	}

	if bidData != nil {
		t.Error("Expected bidData to be nil for normal transaction")
	}
}

func TestClassifyTransactionToFastlaneWithUnknownMethod(t *testing.T) {
	filter, err := NewFilter(testFastlaneContract, testTOBMethodSig, testBackrunMethodSig)
	if err != nil {
		t.Fatalf("Failed to create filter: %v", err)
	}

	// Create a transaction to fastlane contract but with unknown method
	unknownCalldata := []byte{0xff, 0xee, 0xdd, 0xcc, 0x00, 0x00, 0x00, 0x00}

	tx := ethTypes.NewTransaction(
		0,
		common.HexToAddress(testFastlaneContract),
		big.NewInt(0),
		100000,
		big.NewInt(1e9),
		unknownCalldata,
	)

	txType, bidData := filter.ClassifyTransaction(tx)

	if txType != types.NormalTransaction {
		t.Errorf("Expected NormalTransaction for unknown method, got %v", txType)
	}

	if bidData != nil {
		t.Error("Expected bidData to be nil for unknown method")
	}
}

func TestExtractBidAmountFromTOBData(t *testing.T) {
	filter, err := NewFilter(testFastlaneContract, testTOBMethodSig, testBackrunMethodSig)
	if err != nil {
		t.Fatalf("Failed to create filter: %v", err)
	}

	tests := []struct {
		name      string
		bidAmount *big.Int
	}{
		{
			name:      "1 ETH bid",
			bidAmount: big.NewInt(1e18),
		},
		{
			name:      "0.5 ETH bid",
			bidAmount: big.NewInt(5e17),
		},
		{
			name:      "100 ETH bid",
			bidAmount: new(big.Int).Mul(big.NewInt(100), big.NewInt(1e18)),
		},
		{
			name:      "Small bid",
			bidAmount: big.NewInt(1000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calldata := buildTOBCalldata(tt.bidAmount)
			extracted := filter.extractBidAmountFromTOBData(calldata)

			if extracted.Cmp(tt.bidAmount) != 0 {
				t.Errorf("Expected bid amount %s, got %s", tt.bidAmount.String(), extracted.String())
			}
		})
	}
}

func TestExtractBidDataFromBackrunData(t *testing.T) {
	filter, err := NewFilter(testFastlaneContract, testTOBMethodSig, testBackrunMethodSig)
	if err != nil {
		t.Fatalf("Failed to create filter: %v", err)
	}

	bidAmount := big.NewInt(2e18)
	targetHash := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")

	calldata := buildBackrunCalldata(targetHash, bidAmount)
	extractedBid, extractedHash := filter.extractBidDataFromBackrunData(calldata)

	if extractedBid.Cmp(bidAmount) != 0 {
		t.Errorf("Expected bid amount %s, got %s", bidAmount.String(), extractedBid.String())
	}

	if extractedHash != targetHash {
		t.Errorf("Expected target hash %s, got %s", targetHash.Hex(), extractedHash.Hex())
	}
}

// Helper functions

func buildTOBCalldata(bidAmount *big.Int) []byte {
	// 4 bytes method sig + 32 bytes uint256
	calldata := make([]byte, 36)

	// Copy method signature
	sig := common.FromHex(testTOBMethodSig)
	copy(calldata[0:4], sig)

	// Copy bid amount (32 bytes, big-endian)
	bidBytes := bidAmount.Bytes()
	copy(calldata[36-len(bidBytes):36], bidBytes)

	return calldata
}

func buildBackrunCalldata(targetHash common.Hash, bidAmount *big.Int) []byte {
	// 4 bytes method sig + 32 bytes hash + 32 bytes uint256
	calldata := make([]byte, 68)

	// Copy method signature
	sig := common.FromHex(testBackrunMethodSig)
	copy(calldata[0:4], sig)

	// Copy target hash (32 bytes)
	copy(calldata[4:36], targetHash.Bytes())

	// Copy bid amount (32 bytes, big-endian)
	bidBytes := bidAmount.Bytes()
	copy(calldata[68-len(bidBytes):68], bidBytes)

	return calldata
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && (s[:len(substr)] == substr || contains(s[1:], substr))))
}
