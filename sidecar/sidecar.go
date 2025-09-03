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

	// Read loop
	for {
		select {
		case <-s.ctx.Done():
			return nil
		default:
			// Read frame length (4 bytes, big endian)
			lengthBytes := make([]byte, 4)
			if _, err := io.ReadFull(conn, lengthBytes); err != nil {
				if err == io.EOF {
					return fmt.Errorf("connection closed")
				}
				return fmt.Errorf("reading frame length: %w", err)
			}

			frameLength := binary.BigEndian.Uint32(lengthBytes)
			if frameLength == 0 {
				continue // Skip empty frames
			}

			// Skip frames that are too large (likely the initial snapshot)
			if frameLength > 10*1024*1024 { // 10MB limit
				log.Info("Skipping large frame", "size", frameLength)

				// Read and discard the frame data in chunks
				const chunkSize = 1024 * 1024 // 1MB chunks
				remaining := int64(frameLength)
				discardBuf := make([]byte, chunkSize)

				for remaining > 0 {
					toRead := int64(chunkSize)
					if remaining < toRead {
						toRead = remaining
					}
					n, err := conn.Read(discardBuf[:toRead])
					if err != nil {
						return fmt.Errorf("reading large frame data: %w", err)
					}
					remaining -= int64(n)
				}

				continue // Skip to next frame
			}

			// Read frame data
			data := make([]byte, frameLength)
			if _, err := io.ReadFull(conn, data); err != nil {
				return fmt.Errorf("reading frame data: %w", err)
			}

			// Update statistics
			s.bytesTotal.Add(uint64(frameLength) + 4)

			// Process transaction
			s.handleTransaction(data)
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
