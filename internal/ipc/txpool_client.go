package ipc

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
	"github.com/ethereum/go-ethereum/core/types"
)

// TxPoolIPCClient handles bidirectional communication with monad txpool via Unix socket
type TxPoolIPCClient struct {
	socketPath    string
	conn          net.Conn
	connMu        sync.RWMutex
	ctx           context.Context
	eventChan     chan EthTxPoolEvent
	reconnectChan chan struct{}
	connected     atomic.Bool
}

// NewTxPoolIPCClient creates a new txpool IPC client
func NewTxPoolIPCClient(ctx context.Context, socketPath string) *TxPoolIPCClient {
	client := &TxPoolIPCClient{
		socketPath:    socketPath,
		ctx:           ctx,
		eventChan:     make(chan EthTxPoolEvent, 1000), // Buffer for events
		reconnectChan: make(chan struct{}, 1),
	}

	// Start background reconnection loop
	go client.reconnectionLoop()

	// Trigger initial connection attempt
	client.triggerReconnect()

	return client
}

// reconnectionLoop continuously tries to maintain connection to the txpool
func (c *TxPoolIPCClient) reconnectionLoop() {
	backoff := 100 * time.Millisecond
	maxBackoff := 5 * time.Second
	attempt := 0

	for {
		select {
		case <-c.ctx.Done():
			log.Info("TxPool IPC reconnection loop stopped")
			return
		case <-c.reconnectChan:
			// Close old connection if exists
			c.connMu.Lock()
			if c.conn != nil {
				c.conn.Close()
				c.conn = nil
			}
			c.connMu.Unlock()

			for {
				// Try to connect
				conn, err := net.Dial("unix", c.socketPath)
				if err == nil {
					c.connMu.Lock()
					c.conn = conn
					c.connMu.Unlock()
					c.connected.Store(true)
					log.Info("Connected to txpool IPC", "socket", c.socketPath)
					backoff = 100 * time.Millisecond
					attempt = 0

					// Start reading events in background
					go c.readEvents(conn)
					break
				}

				attempt++
				if attempt == 10 {
					log.Error("Failed to connect to txpool IPC after 10 attempts", "socket", c.socketPath, "error", err)
				} else if attempt > 10 && attempt%10 == 0 {
					log.Error("Still unable to connect to txpool IPC", "socket", c.socketPath, "attempts", attempt, "error", err)
				} else {
					log.Info("Failed to connect to txpool IPC, retrying", "attempt", attempt, "error", err)
				}

				// Wait with exponential backoff
				select {
				case <-c.ctx.Done():
					return
				case <-time.After(backoff):
					if backoff < maxBackoff {
						backoff *= 2
						if backoff > maxBackoff {
							backoff = maxBackoff
						}
					}
				}
			}
		}
	}
}

// readEvents reads events from the connection
func (c *TxPoolIPCClient) readEvents(conn net.Conn) {
	defer func() {
		c.connected.Store(false)
		c.triggerReconnect()
	}()

	// First message is always the snapshot
	if err := c.readSnapshot(conn); err != nil {
		log.Error("Failed to read initial snapshot", "error", err)
		return
	}

	// Then read events in a loop
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			// Read length-delimited message (4 bytes big-endian length prefix)
			var msgLen uint32
			if err := binary.Read(conn, binary.BigEndian, &msgLen); err != nil {
				if err != io.EOF {
					log.Error("Error reading message length from txpool", "error", err)
				}
				return
			}

			// Read message data
			msgData := make([]byte, msgLen)
			if _, err := io.ReadFull(conn, msgData); err != nil {
				log.Error("Error reading message data from txpool", "error", err)
				return
			}

			// Decode bincode-encoded Vec<EthTxPoolEvent>
			events, err := DecodeEthTxPoolEvents(msgData)
			if err != nil {
				log.Error("Failed to decode txpool events", "error", err)
				continue
			}

			// Send events to channel
			for _, event := range events {
				select {
				case c.eventChan <- event:
				default:
					log.Error("Event channel full, dropping event")
				}
			}

			log.Debug("Received txpool events", "count", len(events))
		}
	}
}

// readSnapshot reads and processes the initial snapshot from the txpool
func (c *TxPoolIPCClient) readSnapshot(conn net.Conn) error {
	// Read length-delimited message (4 bytes big-endian length prefix)
	var msgLen uint32
	if err := binary.Read(conn, binary.BigEndian, &msgLen); err != nil {
		return fmt.Errorf("failed to read snapshot length: %w", err)
	}

	// Read message data
	msgData := make([]byte, msgLen)
	if _, err := io.ReadFull(conn, msgData); err != nil {
		return fmt.Errorf("failed to read snapshot data: %w", err)
	}

	// Decode bincode-encoded EthTxPoolSnapshot
	snapshot, err := DecodeEthTxPoolSnapshot(msgData)
	if err != nil {
		return fmt.Errorf("failed to decode snapshot: %w", err)
	}

	log.Info("Received txpool snapshot", "tx_count", len(snapshot.TxHashes))
	return nil
}

// triggerReconnect signals the reconnection loop to attempt connection
func (c *TxPoolIPCClient) triggerReconnect() {
	select {
	case c.reconnectChan <- struct{}{}:
	default:
		// Channel already has a signal
	}
}

// GetEventChannel returns the channel for receiving txpool events
func (c *TxPoolIPCClient) GetEventChannel() <-chan EthTxPoolEvent {
	return c.eventChan
}

// SendTxWithPriorityRLP sends a transaction with priority to the txpool using original RLP bytes
func (c *TxPoolIPCClient) SendTxWithPriorityRLP(txRLP []byte, priority *big.Int, extraData []byte) error {
	// Check if connected
	if !c.connected.Load() {
		log.Error("Not connected to txpool IPC - reconnection in progress")
		return fmt.Errorf("not connected to txpool")
	}

	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		log.Error("Not connected to txpool IPC - reconnection in progress")
		return fmt.Errorf("not connected to txpool")
	}

	// Create IPC message with original RLP bytes
	ipcTx := &EthTxPoolIpcTx{
		TxRLP:     txRLP,
		Priority:  priority,
		ExtraData: extraData,
	}

	// Encode to RLP
	data, err := ipcTx.EncodeRLP()
	if err != nil {
		log.Error("Failed to encode IPC transaction", "error", err)
		return fmt.Errorf("failed to encode tx: %w", err)
	}

	// Send length-delimited message (4 bytes big-endian length prefix)
	msgLen := uint32(len(data))
	if err := binary.Write(conn, binary.BigEndian, msgLen); err != nil {
		log.Info("TxPool IPC connection lost, triggering reconnect", "error", err)
		c.connected.Store(false)
		c.connMu.Lock()
		c.conn = nil
		c.connMu.Unlock()
		c.triggerReconnect()
		return fmt.Errorf("failed to write message length: %w", err)
	}

	if _, err := conn.Write(data); err != nil {
		log.Info("TxPool IPC connection lost, triggering reconnect", "error", err)
		c.connected.Store(false)
		c.connMu.Lock()
		c.conn = nil
		c.connMu.Unlock()
		c.triggerReconnect()
		return fmt.Errorf("failed to send tx to txpool: %w", err)
	}

	log.Info("Sent transaction with priority to txpool", "tx_rlp_len", len(txRLP), "priority", priority.String())
	return nil
}

// SendTxWithPriority sends a transaction with priority to the txpool (deprecated - use SendTxWithPriorityRLP)
func (c *TxPoolIPCClient) SendTxWithPriority(tx *types.Transaction, priority *big.Int, extraData []byte) error {
	// Check if connected
	if !c.connected.Load() {
		log.Error("Not connected to txpool IPC - reconnection in progress")
		return fmt.Errorf("not connected to txpool")
	}

	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		log.Error("Not connected to txpool IPC - reconnection in progress")
		return fmt.Errorf("not connected to txpool")
	}

	// Create IPC message
	ipcTx := &EthTxPoolIpcTx{
		Tx:        tx,
		Priority:  priority,
		ExtraData: extraData,
	}

	// Encode to RLP
	data, err := ipcTx.EncodeRLP()
	if err != nil {
		log.Error("Failed to encode IPC transaction", "error", err)
		return fmt.Errorf("failed to encode tx: %w", err)
	}

	// Send length-delimited message (4 bytes big-endian length prefix)
	msgLen := uint32(len(data))
	if err := binary.Write(conn, binary.BigEndian, msgLen); err != nil {
		log.Info("TxPool IPC connection lost, triggering reconnect", "error", err)
		c.connected.Store(false)
		c.connMu.Lock()
		c.conn = nil
		c.connMu.Unlock()
		c.triggerReconnect()
		return fmt.Errorf("failed to write message length: %w", err)
	}

	if _, err := conn.Write(data); err != nil {
		log.Info("TxPool IPC connection lost, triggering reconnect", "error", err)
		c.connected.Store(false)
		c.connMu.Lock()
		c.conn = nil
		c.connMu.Unlock()
		c.triggerReconnect()
		return fmt.Errorf("failed to send tx to txpool: %w", err)
	}

	log.Info("Sent transaction with priority to txpool", "tx_hash", tx.Hash().Hex(), "priority", priority.String())
	return nil
}

// IsConnected returns true if connected to the txpool
func (c *TxPoolIPCClient) IsConnected() bool {
	return c.connected.Load()
}

// Close closes the connection
func (c *TxPoolIPCClient) Close() error {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
