package orchestrator

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/internal/ipc"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/config"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
	"github.com/ethereum/go-ethereum/core/types"
)

type Sidecar struct {
	config       *config.Config
	shutdownChan chan struct{}

	// Statistics
	txReceived atomic.Uint64
	bytesTotal atomic.Uint64

	// IPC client
	ipcClient *ipc.Client

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

func NewSidecar(config *config.Config, shutdownChan chan struct{}) *Sidecar {
	ctx, cancel := context.WithCancel(context.Background())
	return &Sidecar{
		config:       config,
		shutdownChan: shutdownChan,
		ipcClient:    ipc.NewClient(config.IPCPath, ctx),
		ctx:          ctx,
		cancel:       cancel,
	}
}

func (s *Sidecar) Start() error {
	go s.streamTransactions()
	return nil
}

func (s *Sidecar) Stop() {
	s.cancel()
}

func (s *Sidecar) streamTransactions() {
	defer close(s.shutdownChan)

	reconnectDelay := 5 * time.Second

	for {
		select {
		case <-s.ctx.Done():
			log.Info("Transaction streaming stopped")
			return
		default:
			if err := s.handleIPCConnection(); err != nil {
				log.Error("IPC error", "error", err, "path", s.config.IPCPath)
				// Wait before reconnecting
				select {
				case <-s.ctx.Done():
					return
				case <-time.After(reconnectDelay):
					log.Info("Attempting to reconnect", "path", s.config.IPCPath)
					continue
				}
			}
		}
	}
}

func (s *Sidecar) handleIPCConnection() error {
	// Establish connection
	if err := s.ipcClient.Connect(); err != nil {
		return err
	}
	defer s.ipcClient.Close()

	// Read loop
	for {
		select {
		case <-s.ctx.Done():
			return nil
		default:
			tx, err := s.ipcClient.ReadTransaction()
			if err != nil {
				return err
			}
			if tx == nil {
				// Timeout occurred, continue
				continue
			}

			// Update statistics
			s.txReceived.Add(1)

			// Process transaction
			s.handleTransaction(tx)
		}
	}
}

func (s *Sidecar) handleTransaction(tx *types.Transaction) {
	counter := s.txReceived.Load()

	log.Info("Transaction received",
		"index", counter,
		"hash", tx.Hash().Hex(),
	)

	// TODO: Send to processor for validation
	// TODO: Forward validated transactions to MEV gateway
}
