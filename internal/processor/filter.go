package processor

import (
	"fmt"
	"math/big"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/types"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
)

type Filter struct {
	blacklistedAddresses map[common.Address]bool
	minGasPrice          uint64
	fastlaneContract     common.Address
	tobMethodSig         [4]byte
	backrunMethodSig     [4]byte
}

func NewFilter(fastlaneContractHex, tobMethodSigHex, backrunMethodSigHex string) (*Filter, error) {
	f := &Filter{
		blacklistedAddresses: make(map[common.Address]bool),
	}

	// Validate all required config is provided
	if fastlaneContractHex == "" {
		return nil, fmt.Errorf("fastlane contract address is required")
	}
	if tobMethodSigHex == "" {
		return nil, fmt.Errorf("TOB method signature is required")
	}
	if backrunMethodSigHex == "" {
		return nil, fmt.Errorf("backrun method signature is required")
	}

	// Parse fastlane contract address
	f.fastlaneContract = common.HexToAddress(fastlaneContractHex)

	// Parse TOB method signature
	if len(tobMethodSigHex) < 10 || tobMethodSigHex[:2] != "0x" {
		return nil, fmt.Errorf("invalid TOB method signature format: %s (expected 0x12345678)", tobMethodSigHex)
	}
	tobBytes := common.FromHex(tobMethodSigHex)
	if len(tobBytes) < 4 {
		return nil, fmt.Errorf("TOB method signature too short: %s", tobMethodSigHex)
	}
	copy(f.tobMethodSig[:], tobBytes[:4])

	// Parse backrun method signature
	if len(backrunMethodSigHex) < 10 || backrunMethodSigHex[:2] != "0x" {
		return nil, fmt.Errorf("invalid backrun method signature format: %s (expected 0x12345678)", backrunMethodSigHex)
	}
	backrunBytes := common.FromHex(backrunMethodSigHex)
	if len(backrunBytes) < 4 {
		return nil, fmt.Errorf("backrun method signature too short: %s", backrunMethodSigHex)
	}
	copy(f.backrunMethodSig[:], backrunBytes[:4])

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

			if methodSig == f.tobMethodSig {
				bidAmount := f.extractBidAmountFromTOBData(tx.Data())
				return types.TOBBid, &types.BidData{BidAmount: bidAmount}
			} else if methodSig == f.backrunMethodSig {
				bidAmount, targetTxHash := f.extractBidDataFromBackrunData(tx.Data())
				return types.BackrunBid, &types.BidData{
					BidAmount:    bidAmount,
					TargetTxHash: &targetTxHash,
				}
			}
		}
	}

	return types.NormalTransaction, nil
}

// extractBidAmountFromTOBData extracts bid amount from TOB transaction calldata
// Expected calldata format: 4 bytes (method sig) + 32 bytes (uint256 bid)
func (f *Filter) extractBidAmountFromTOBData(data []byte) *big.Int {
	// Need at least 4 bytes (method sig) + 32 bytes (uint256)
	if len(data) < 36 {
		log.Error("TOB calldata too short", "length", len(data))
		return big.NewInt(0)
	}

	// Skip first 4 bytes (method signature), read next 32 bytes as uint256
	bidAmount := new(big.Int).SetBytes(data[4:36])
	return bidAmount
}

// extractBidDataFromBackrunData extracts bid amount and target tx hash from backrun transaction calldata
// Expected calldata format: 4 bytes (method sig) + 32 bytes (bytes32 targetHash) + 32 bytes (uint256 bid)
func (f *Filter) extractBidDataFromBackrunData(data []byte) (*big.Int, common.Hash) {
	// Need at least 4 bytes (method sig) + 32 bytes (hash) + 32 bytes (uint256)
	if len(data) < 68 {
		log.Error("Backrun calldata too short", "length", len(data))
		return big.NewInt(0), common.Hash{}
	}

	// Skip first 4 bytes (method signature)
	// Read next 32 bytes as target tx hash
	targetHash := common.BytesToHash(data[4:36])

	// Read next 32 bytes as bid amount (uint256)
	bidAmount := new(big.Int).SetBytes(data[36:68])

	return bidAmount, targetHash
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

func (f *Filter) SetTOBMethodSignature(sig [4]byte) {
	f.tobMethodSig = sig
}

func (f *Filter) SetBackrunMethodSignature(sig [4]byte) {
	f.backrunMethodSig = sig
}
