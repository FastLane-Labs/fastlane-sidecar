package processor

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type Filter struct {
	// TODO: Add filter configuration
	blacklistedAddresses map[common.Address]bool
	minGasPrice          uint64
}

func NewFilter() *Filter {
	return &Filter{
		blacklistedAddresses: make(map[common.Address]bool),
	}
}

func (f *Filter) ShouldProcess(tx *types.Transaction) bool {
	// TODO: Implement filtering logic
	// This could include:
	// - Blacklist/whitelist checking
	// - Gas price thresholds
	// - Transaction type filtering
	// - Contract interaction filtering
	return true
}

func (f *Filter) AddBlacklistedAddress(addr common.Address) {
	f.blacklistedAddresses[addr] = true
}

func (f *Filter) SetMinGasPrice(price uint64) {
	f.minGasPrice = price
}
