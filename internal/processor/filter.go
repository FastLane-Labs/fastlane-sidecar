package processor

import (
	"math/big"

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

func NewFilter() *Filter {
	return &Filter{
		blacklistedAddresses: make(map[common.Address]bool),
		// TODO: Set actual fastlane contract address and method signatures
		// fastlaneContract: common.HexToAddress("0x..."),
		// tobMethodSig: [4]byte{0x12, 0x34, 0x56, 0x78}, // TODO: Set actual method sig
		// backrunMethodSig: [4]byte{0x87, 0x65, 0x43, 0x21}, // TODO: Set actual method sig
	}
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

// extractBidAmountFromTOBData would extract bid amount from TOB transaction data
func (f *Filter) extractBidAmountFromTOBData(data []byte) *big.Int {
	// TODO: Implement actual ABI decoding
	// This would decode the contract call parameters to extract the bid amount
	return big.NewInt(0)
}

// extractBidDataFromBackrunData would extract bid amount and target tx hash from backrun transaction data
func (f *Filter) extractBidDataFromBackrunData(data []byte) (*big.Int, common.Hash) {
	// TODO: Implement actual ABI decoding
	// This would decode the contract call parameters to extract:
	// - bid amount
	// - target transaction hash that this bid is trying to backrun
	return big.NewInt(0), common.Hash{}
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
