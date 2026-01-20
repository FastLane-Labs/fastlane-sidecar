package processor

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

const (
	// FlashExecutionBid method signature: flashExecutionBid(uint256,bytes32[],uint256,bool,bool,address,bytes)
	FlashExecutionBidSig = "0x0c7abd22"
)

type Filter struct {
	fastlaneContract  common.Address
	flashBidMethodSig [4]byte
	flashBidABI       abi.ABI
}

func NewFilter(fastlaneContractHex string) (*Filter, error) {
	f := &Filter{}

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

// ClassifyTransaction determines the type of transaction
func (f *Filter) ClassifyTransaction(tx *ethtypes.Transaction) (types.TransactionType, *types.BidData) {
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

	if len(txHashes) == 0 {
		// Wrong size format
		log.Warn("flashExecutionBid with no targets not supported",
			"bid_amount", bidAmount.String())
		return types.NormalTransaction, nil
	}

	if txHashes[len(txHashes)-1] != (common.Hash{}) {
		// Last tx hash must be the zero hash
		log.Warn("flashExecutionBid with non-zero last tx hash not supported",
			"bid_amount", bidAmount.String())
		return types.NormalTransaction, nil
	}

	// Classify based on txHashes length
	switch len(txHashes) {
	case 1:
		// TOB bid (no target transactions)
		return types.TOBBid, &types.BidData{
			BidAmount: bidAmount,
		}

	case 2:
		// Backrun bid (single target transaction)
		targetHash := common.BytesToHash(txHashes[0][:])
		return types.BackrunBid, &types.BidData{
			BidAmount:    bidAmount,
			TargetTxHash: &targetHash,
		}

	default:
		// Multiple targets not supported yet
		log.Warn("flashExecutionBid with multiple targets not supported yet",
			"target_count", len(txHashes),
			"bid_amount", bidAmount.String())
		return types.NormalTransaction, nil
	}
}
