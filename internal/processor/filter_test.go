package processor

import (
	"math/big"
	"testing"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
)

const testContractAddr = "0xf9436C4b1353D5B411AD5bb65B9826f34737BbC7"

func TestClassifyTOBBid(t *testing.T) {
	filter, err := NewFilter(testContractAddr)
	if err != nil {
		t.Fatalf("Failed to create filter: %v", err)
	}

	// Create flashExecutionBid calldata for TOB (empty txHashes array)
	bidAmount := big.NewInt(1000)

	// Encode using the ABI
	method := filter.flashBidABI.Methods["flashExecutionBid"]
	calldata, err := method.Inputs.Pack(
		bidAmount,                           // bidAmount
		[][32]byte{},                       // txHashes (empty = TOB)
		big.NewInt(100),                    // targetBlockNumber
		false,                               // executeOnLoss
		false,                               // payBidOnFail
		common.HexToAddress("0x1234567890123456789012345678901234567890"), // searcherToAddress
		[]byte{},                           // searcherCallData
	)
	if err != nil {
		t.Fatalf("Failed to pack calldata: %v", err)
	}

	// Prepend method signature
	fullCalldata := append(filter.flashBidMethodSig[:], calldata...)

	// Create transaction
	tx := ethTypes.NewTransaction(
		0,
		filter.fastlaneContract,
		big.NewInt(0),
		100000,
		big.NewInt(1000000000),
		fullCalldata,
	)

	// Classify
	txType, bidData := filter.ClassifyTransaction(tx)

	// Verify
	if txType != types.TOBBid {
		t.Errorf("Expected TOBBid, got %v", txType)
	}

	if bidData == nil {
		t.Fatal("Expected bid data, got nil")
	}

	if bidData.BidAmount.Cmp(bidAmount) != 0 {
		t.Errorf("Expected bid amount %s, got %s", bidAmount.String(), bidData.BidAmount.String())
	}

	if bidData.TargetTxHash != nil {
		t.Error("Expected no target hash for TOB bid")
	}
}

func TestClassifyBackrunBid(t *testing.T) {
	filter, err := NewFilter(testContractAddr)
	if err != nil {
		t.Fatalf("Failed to create filter: %v", err)
	}

	// Create flashExecutionBid calldata for backrun (single txHash in array)
	bidAmount := big.NewInt(2000)
	targetHash := common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")

	// Encode using the ABI
	method := filter.flashBidABI.Methods["flashExecutionBid"]
	calldata, err := method.Inputs.Pack(
		bidAmount,                           // bidAmount
		[][32]byte{targetHash},              // txHashes (single hash = backrun)
		big.NewInt(100),                    // targetBlockNumber
		false,                               // executeOnLoss
		false,                               // payBidOnFail
		common.HexToAddress("0x1234567890123456789012345678901234567890"), // searcherToAddress
		[]byte{},                           // searcherCallData
	)
	if err != nil {
		t.Fatalf("Failed to pack calldata: %v", err)
	}

	// Prepend method signature
	fullCalldata := append(filter.flashBidMethodSig[:], calldata...)

	// Create transaction
	tx := ethTypes.NewTransaction(
		0,
		filter.fastlaneContract,
		big.NewInt(0),
		100000,
		big.NewInt(1000000000),
		fullCalldata,
	)

	// Classify
	txType, bidData := filter.ClassifyTransaction(tx)

	// Verify
	if txType != types.BackrunBid {
		t.Errorf("Expected BackrunBid, got %v", txType)
	}

	if bidData == nil {
		t.Fatal("Expected bid data, got nil")
	}

	if bidData.BidAmount.Cmp(bidAmount) != 0 {
		t.Errorf("Expected bid amount %s, got %s", bidAmount.String(), bidData.BidAmount.String())
	}

	if bidData.TargetTxHash == nil {
		t.Fatal("Expected target hash, got nil")
	}

	if *bidData.TargetTxHash != targetHash {
		t.Errorf("Expected target hash %s, got %s", targetHash.Hex(), bidData.TargetTxHash.Hex())
	}
}

func TestClassifyMultipleTargets(t *testing.T) {
	filter, err := NewFilter(testContractAddr)
	if err != nil {
		t.Fatalf("Failed to create filter: %v", err)
	}

	// Create flashExecutionBid calldata with multiple targets (not supported)
	bidAmount := big.NewInt(3000)
	hash1 := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	hash2 := common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")

	// Encode using the ABI
	method := filter.flashBidABI.Methods["flashExecutionBid"]
	calldata, err := method.Inputs.Pack(
		bidAmount,                           // bidAmount
		[][32]byte{hash1, hash2},           // txHashes (multiple = not supported)
		big.NewInt(100),                    // targetBlockNumber
		false,                               // executeOnLoss
		false,                               // payBidOnFail
		common.HexToAddress("0x1234567890123456789012345678901234567890"), // searcherToAddress
		[]byte{},                           // searcherCallData
	)
	if err != nil {
		t.Fatalf("Failed to pack calldata: %v", err)
	}

	// Prepend method signature
	fullCalldata := append(filter.flashBidMethodSig[:], calldata...)

	// Create transaction
	tx := ethTypes.NewTransaction(
		0,
		filter.fastlaneContract,
		big.NewInt(0),
		100000,
		big.NewInt(1000000000),
		fullCalldata,
	)

	// Classify
	txType, bidData := filter.ClassifyTransaction(tx)

	// Verify - should be classified as normal (not supported)
	if txType != types.NormalTransaction {
		t.Errorf("Expected NormalTransaction for multiple targets, got %v", txType)
	}

	if bidData != nil {
		t.Error("Expected nil bid data for unsupported multiple targets")
	}
}

func TestClassifyNormalTransaction(t *testing.T) {
	filter, err := NewFilter(testContractAddr)
	if err != nil {
		t.Fatalf("Failed to create filter: %v", err)
	}

	// Create a normal transaction (not to fastlane contract)
	tx := ethTypes.NewTransaction(
		0,
		common.HexToAddress("0x9999999999999999999999999999999999999999"),
		big.NewInt(100),
		21000,
		big.NewInt(1000000000),
		[]byte{},
	)

	// Classify
	txType, bidData := filter.ClassifyTransaction(tx)

	// Verify
	if txType != types.NormalTransaction {
		t.Errorf("Expected NormalTransaction, got %v", txType)
	}

	if bidData != nil {
		t.Error("Expected nil bid data for normal transaction")
	}
}

func TestMethodSignature(t *testing.T) {
	// Verify the method signature is correct
	expected := []byte{0x0c, 0x7a, 0xbd, 0x22}
	actual := common.FromHex(FlashExecutionBidSig)

	if len(actual) != 4 {
		t.Fatalf("Expected 4 bytes, got %d", len(actual))
	}

	for i := 0; i < 4; i++ {
		if actual[i] != expected[i] {
			t.Errorf("Method signature mismatch at byte %d: expected 0x%02x, got 0x%02x", i, expected[i], actual[i])
		}
	}

	t.Logf("Method signature verified: %s", hexutil.Encode(actual))
}
