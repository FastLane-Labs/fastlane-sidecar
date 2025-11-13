package processor

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
)

const (
	// FlashExecutionBid method signature: flashExecutionBid(uint256,bytes32[],uint256,bool,bool,address,bytes)
	FlashExecutionBidSig = "0x0c7abd22"
)

type Filter struct {
	blacklistedAddresses map[common.Address]bool
	minGasPrice          uint64
	fastlaneContract     common.Address
	flashBidMethodSig    [4]byte
	flashBidABI          abi.ABI
}

func NewFilter(fastlaneContractHex string) (*Filter, error) {
	f := &Filter{
		blacklistedAddresses: make(map[common.Address]bool),
	}

	// Validate all required config is provided
	if fastlaneContractHex == "" {
		return nil, fmt.Errorf("fastlane contract address is required")
	}

	// Parse fastlane contract address
	f.fastlaneContract = common.HexToAddress(fastlaneContractHex)

	// Parse flashExecutionBid method signature
	flashBidBytes := common.FromHex(FlashExecutionBidSig)
	if len(flashBidBytes) < 4 {
		return nil, fmt.Errorf("flashExecutionBid method signature too short: %s", FlashExecutionBidSig)
	}
	copy(f.flashBidMethodSig[:], flashBidBytes[:4])

	// Create ABI for parsing flashExecutionBid
	// flashExecutionBid(uint256 bidAmount, bytes32[] txHashes, uint256 targetBlockNumber,
	//                   bool executeOnLoss, bool payBidOnFail, address searcherToAddress, bytes searcherCallData)
	abiJSON := `[{
		"name": "flashExecutionBid",
		"type": "function",
		"inputs": [
			{"name": "bidAmount", "type": "uint256"},
			{"name": "txHashes", "type": "bytes32[]"},
			{"name": "targetBlockNumber", "type": "uint256"},
			{"name": "executeOnLoss", "type": "bool"},
			{"name": "payBidOnFail", "type": "bool"},
			{"name": "searcherToAddress", "type": "address"},
			{"name": "searcherCallData", "type": "bytes"}
		]
	}]`

	parsedABI, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to parse flashExecutionBid ABI: %w", err)
	}
	f.flashBidABI = parsedABI

	return f, nil
}

// ShouldProcess determines if a transaction should be processed
func (f *Filter) ShouldProcess(tx *ethTypes.Transaction) bool {
	// Basic filtering logic
	if tx.GasPrice().Uint64() < f.minGasPrice {
		return false
	}

	if tx.To() != nil {
		if f.blacklistedAddresses[*tx.To()] {
			return false
		}
	}

	return true
}

// ClassifyTransaction determines the type of transaction
func (f *Filter) ClassifyTransaction(tx *ethTypes.Transaction) (types.TransactionType, *types.BidData) {
	// Check if this is a fastlane contract call
	if tx.To() != nil && *tx.To() == f.fastlaneContract {
		if len(tx.Data()) >= 4 {
			methodSig := [4]byte{tx.Data()[0], tx.Data()[1], tx.Data()[2], tx.Data()[3]}

			if methodSig == f.flashBidMethodSig {
				return f.classifyFlashExecutionBid(tx.Data())
			}
		}
	}

	return types.NormalTransaction, nil
}

// classifyFlashExecutionBid parses flashExecutionBid calldata and classifies as TOB or Backrun
func (f *Filter) classifyFlashExecutionBid(data []byte) (types.TransactionType, *types.BidData) {
	if len(data) < 4 {
		log.Error("flashExecutionBid calldata too short", "length", len(data))
		return types.NormalTransaction, nil
	}

	// Decode the method arguments
	method := f.flashBidABI.Methods["flashExecutionBid"]
	args, err := method.Inputs.Unpack(data[4:])
	if err != nil {
		log.Error("Failed to decode flashExecutionBid calldata", "error", err)
		return types.NormalTransaction, nil
	}

	if len(args) < 2 {
		log.Error("flashExecutionBid has insufficient arguments", "count", len(args))
		return types.NormalTransaction, nil
	}

	// Extract bidAmount (first argument)
	bidAmount, ok := args[0].(*big.Int)
	if !ok {
		log.Error("Failed to parse bidAmount from flashExecutionBid")
		return types.NormalTransaction, nil
	}

	// Extract txHashes (second argument)
	txHashes, ok := args[1].([][32]byte)
	if !ok {
		log.Error("Failed to parse txHashes from flashExecutionBid")
		return types.NormalTransaction, nil
	}

	// Classify based on txHashes length and content
	if len(txHashes) != 1 {
		// Only single-element arrays are valid bids
		log.Warn("flashExecutionBid with invalid txHashes length",
			"target_count", len(txHashes),
			"bid_amount", bidAmount.String())
		return types.NormalTransaction, nil
	}

	// Check if the hash is zero (TOB) or non-zero (Backrun)
	targetHash := common.BytesToHash(txHashes[0][:])
	if targetHash == (common.Hash{}) {
		// TOB bid (zero hash indicates top-of-block bid)
		return types.TOBBid, &types.BidData{
			BidAmount: bidAmount,
		}
	}

	// Backrun bid (non-zero hash indicates target transaction)
	return types.BackrunBid, &types.BidData{
		BidAmount:    bidAmount,
		TargetTxHash: &targetHash,
	}
}

func (f *Filter) AddBlacklistedAddress(addr common.Address) {
	f.blacklistedAddresses[addr] = true
}

func (f *Filter) SetMinGasPrice(price uint64) {
	f.minGasPrice = price
}

func (f *Filter) SetFastlaneContract(addr common.Address) {
	f.fastlaneContract = addr
}
