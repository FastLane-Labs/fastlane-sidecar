package ipc

import "github.com/ethereum/go-ethereum/core/types"

type TransactionHandler func(*types.Transaction) error
