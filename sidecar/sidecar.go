package sidecar

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
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

	// Read loop with binary protocol
	for {
		select {
		case <-s.ctx.Done():
			return nil
		default:
			// Set read deadline to check context periodically
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))

			// Read 4-byte length header (big-endian)
			lengthBuf := make([]byte, 4)
			_, err := io.ReadFull(conn, lengthBuf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue // Timeout is expected, continue checking context
				}
				if err == io.EOF {
					return fmt.Errorf("connection closed")
				}
				return fmt.Errorf("reading length header: %w", err)
			}

			// Parse message length
			messageLen := binary.BigEndian.Uint32(lengthBuf)

			// Sanity check - skip messages larger than 10MB
			if messageLen > 10*1024*1024 {
				log.Error("Message too large, skipping", "size", messageLen)
				// Discard the message by reading and discarding the bytes
				_, err = io.CopyN(io.Discard, conn, int64(messageLen))
				if err != nil {
					return fmt.Errorf("discarding oversized message: %w", err)
				}
				continue
			}

			// Read the RLP-encoded transaction data
			txBytes := make([]byte, messageLen)
			_, err = io.ReadFull(conn, txBytes)
			if err != nil {
				return fmt.Errorf("reading transaction data: %w", err)
			}

			// Update statistics
			s.bytesTotal.Add(uint64(4 + messageLen)) // 4 bytes for length + message

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
