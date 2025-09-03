package sidecar

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/config"
	"github.com/FastLane-Labs/fastlane-sidecar/log"
	"github.com/ethereum/go-ethereum/core/types"
)

type Sidecar struct {
	config       *config.Config
	shutdownChan chan struct{}

	// Statistics
	txReceived atomic.Uint64
	bytesTotal atomic.Uint64

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

func NewSidecar(config *config.Config, shutdownChan chan struct{}) *Sidecar {
	ctx, cancel := context.WithCancel(context.Background())
	return &Sidecar{
		config:       config,
		shutdownChan: shutdownChan,
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
	conn, err := net.Dial("unix", s.config.IPCPath)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer conn.Close()

	log.Info("IPC connected", "endpoint", s.config.IPCPath)

	// Use buffered reader for newline-delimited protocol
	reader := bufio.NewReader(conn)

	// Read loop
	for {
		select {
		case <-s.ctx.Done():
			return nil
		default:
			// Set read deadline to check context periodically
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))

			// Read raw transaction bytes (expecting newline delimiter)
			txBytes, err := reader.ReadBytes('\n')
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue // Timeout is expected, continue checking context
				}
				return fmt.Errorf("reading from IPC: %w", err)
			}

			// Remove the newline delimiter
			if len(txBytes) > 0 && txBytes[len(txBytes)-1] == '\n' {
				txBytes = txBytes[:len(txBytes)-1]
			}

			// Skip if this looks like a text message (starts with ASCII characters)
			// ACK messages start with "ACK:", keccak responses start with "keccak256:"
			if len(txBytes) > 0 && (txBytes[0] >= 'A' && txBytes[0] <= 'z') {
				log.Debug("Received text message", "message", string(txBytes))
				continue
			}

			// Update statistics
			s.bytesTotal.Add(uint64(len(txBytes)) + 1) // +1 for the newline

			// Process transaction
			s.handleTransaction(txBytes)
		}
	}
}

func (s *Sidecar) handleTransaction(data []byte) {
	counter := s.txReceived.Add(1)

	// Decode transaction
	var tx types.Transaction
	if err := tx.UnmarshalBinary(data); err != nil {
		log.Error("Failed to decode transaction",
			"index", counter,
			"size", len(data),
			"error", err)
		return
	}

	log.Info("Transaction received",
		"index", counter,
		"hash", tx.Hash().Hex(),
	)
}
