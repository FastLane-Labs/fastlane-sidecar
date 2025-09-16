package types

import (
	"github.com/ethereum/go-ethereum/core/types"
)

type IPCClient interface {
	Connect() error
	Close() error
	ReadTransaction() (*types.Transaction, error)
	WriteData([]byte) error
}

type GatewayClient interface {
	Connect() error
	Close() error
	SendTransaction(*types.Transaction) error
}

type TransactionProcessor interface {
	ValidateTransaction(*types.Transaction) error
	ShouldForward(*types.Transaction) bool
}

type MessageHandler interface {
	HandleMessage([]byte) error
}

type SidecarOrchestrator interface {
	Start() error
	Stop()
}
