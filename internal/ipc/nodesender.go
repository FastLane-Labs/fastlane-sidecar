package ipc

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/types"
)

// NodeSender sends priority transactions to the node
type NodeSender struct {
	socketPath    string
	conn          net.Conn
	connMu        sync.RWMutex
	ctx           context.Context
	reconnectChan chan struct{}
	connected     atomic.Bool
}

// NewNodeSender creates a new node sender and starts background reconnection loop
func NewNodeSender(ctx context.Context, socketPath string) *NodeSender {
	ns := &NodeSender{
		socketPath:    socketPath,
		ctx:           ctx,
		reconnectChan: make(chan struct{}, 1),
	}

	// Start background reconnection loop
	go ns.reconnectionLoop()

	// Start background heartbeat sender
	go ns.heartbeatLoop()

	// Trigger initial connection attempt
	ns.triggerReconnect()

	return ns
}

// reconnectionLoop continuously tries to maintain connection to the node
func (ns *NodeSender) reconnectionLoop() {
	backoff := 100 * time.Millisecond
	maxBackoff := 5 * time.Second
	attempt := 0

	for {
		select {
		case <-ns.ctx.Done():
			log.Info("Node sender reconnection loop stopped")
			return
		case <-ns.reconnectChan:
			// Reconnection triggered
			// First, ensure old connection is fully closed
			ns.connMu.Lock()
			if ns.conn != nil {
				ns.conn.Close()
				ns.conn = nil
			}
			ns.connMu.Unlock()

			for {
				// Try to connect
				conn, err := net.Dial("unix", ns.socketPath)
				if err == nil {
					ns.connMu.Lock()
					ns.conn = conn
					ns.connMu.Unlock()
					ns.connected.Store(true)
					log.Info("Connected to node", "socket", ns.socketPath)
					backoff = 100 * time.Millisecond // Reset backoff
					attempt = 0
					break // Exit retry loop, wait for next trigger
				}

				attempt++
				if attempt == 10 {
					log.Error("Failed to connect to node after 10 attempts - please restart the node", "socket", ns.socketPath, "error", err)
				} else if attempt > 10 && attempt%10 == 0 {
					log.Error("Still unable to connect to node - please restart the node", "socket", ns.socketPath, "attempts", attempt, "error", err)
				} else {
					log.Info("Failed to connect to node, retrying", "attempt", attempt, "error", err)
				}

				// Wait with exponential backoff
				select {
				case <-ns.ctx.Done():
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

// heartbeatLoop sends heartbeat messages to the node every 30 seconds
func (ns *NodeSender) heartbeatLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ns.ctx.Done():
			log.Info("Node sender heartbeat loop stopped")
			return
		case <-ticker.C:
			// Only send heartbeat if connected to avoid spamming logs
			if !ns.connected.Load() {
				continue
			}

			// Send heartbeat - if it fails, it will trigger reconnection
			if err := ns.SendHeartbeat(); err != nil {
				log.Debug("Failed to send heartbeat", "error", err)
				// Reconnection already triggered by SendHeartbeat
			}
		}
	}
}

// triggerReconnect signals the reconnection loop to attempt connection
func (ns *NodeSender) triggerReconnect() {
	select {
	case ns.reconnectChan <- struct{}{}:
	default:
		// Channel already has a signal, no need to add another
	}
}

// Connect is now deprecated - connection happens automatically in background
func (ns *NodeSender) Connect() error {
	// Just trigger reconnect and return immediately
	ns.triggerReconnect()
	return nil
}

// Close closes the connection
func (ns *NodeSender) Close() error {
	ns.connMu.Lock()
	defer ns.connMu.Unlock()
	if ns.conn != nil {
		return ns.conn.Close()
	}
	return nil
}

// SendTxWithPriority sends a transaction with priority to the node
func (ns *NodeSender) SendTxWithPriority(txWithPriority types.TxWithPriority) error {
	// Check if connected
	if !ns.connected.Load() {
		log.Error("Not connected to node - reconnection in progress")
		return fmt.Errorf("not connected to node")
	}

	ns.connMu.RLock()
	conn := ns.conn
	ns.connMu.RUnlock()

	if conn == nil {
		log.Error("Not connected to node - reconnection in progress")
		return fmt.Errorf("not connected to node")
	}

	// Serialize as SidecarMessage::TxWithPriority variant
	data := types.SerializeSidecarMessageTxWithPriority(txWithPriority)

	// Send length-delimited message
	msgLen := uint32(len(data))
	if err := binary.Write(conn, binary.BigEndian, msgLen); err != nil {
		log.Info("Node connection lost, triggering reconnect", "error", err)
		ns.connected.Store(false)
		ns.connMu.Lock()
		ns.conn = nil
		ns.connMu.Unlock()
		ns.triggerReconnect()
		return fmt.Errorf("failed to write message length: %w", err)
	}

	if _, err := conn.Write(data); err != nil {
		log.Info("Node connection lost, triggering reconnect", "error", err)
		ns.connected.Store(false)
		ns.connMu.Lock()
		ns.conn = nil
		ns.connMu.Unlock()
		ns.triggerReconnect()
		return fmt.Errorf("failed to send priority tx to node: %w", err)
	}

	log.Info("Sent transaction with priority to node", "tx_bytes", len(txWithPriority.TxBytes), "priority", txWithPriority.Priority[:4])
	return nil
}

// SendHeartbeat sends a heartbeat message to the node
func (ns *NodeSender) SendHeartbeat() error {
	// Check if connected
	if !ns.connected.Load() {
		log.Debug("Not connected to node - skipping heartbeat")
		return fmt.Errorf("not connected to node")
	}

	ns.connMu.RLock()
	conn := ns.conn
	ns.connMu.RUnlock()

	if conn == nil {
		log.Debug("Not connected to node - skipping heartbeat")
		return fmt.Errorf("not connected to node")
	}

	// Serialize as SidecarMessage::Heartbeat variant
	data := types.SerializeSidecarMessageHeartbeat()

	// Send length-delimited message
	msgLen := uint32(len(data))
	if err := binary.Write(conn, binary.BigEndian, msgLen); err != nil {
		log.Info("Node connection lost during heartbeat, triggering reconnect", "error", err)
		ns.connected.Store(false)
		ns.connMu.Lock()
		ns.conn = nil
		ns.connMu.Unlock()
		ns.triggerReconnect()
		return fmt.Errorf("failed to write heartbeat message length: %w", err)
	}

	if _, err := conn.Write(data); err != nil {
		log.Info("Node connection lost during heartbeat, triggering reconnect", "error", err)
		ns.connected.Store(false)
		ns.connMu.Lock()
		ns.conn = nil
		ns.connMu.Unlock()
		ns.triggerReconnect()
		return fmt.Errorf("failed to send heartbeat to node: %w", err)
	}

	log.Debug("Sent heartbeat to node")
	return nil
}

// IsConnected returns true if connected to the node
func (ns *NodeSender) IsConnected() bool {
	return ns.conn != nil
}
