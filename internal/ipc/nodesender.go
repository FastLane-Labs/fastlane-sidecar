package ipc

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/types"
)

// NodeSender sends priority transactions to the node
type NodeSender struct {
	socketPath string
	conn       net.Conn
	ctx        context.Context
}

// NewNodeSender creates a new node sender
func NewNodeSender(ctx context.Context, socketPath string) *NodeSender {
	return &NodeSender{
		socketPath: socketPath,
		ctx:        ctx,
	}
}

// Connect establishes connection to the node
func (ns *NodeSender) Connect() error {
	// Retry connection with backoff
	maxRetries := 10
	backoff := 100 * time.Millisecond

	for i := 0; i < maxRetries; i++ {
		conn, err := net.Dial("unix", ns.socketPath)
		if err == nil {
			ns.conn = conn
			log.Info("Connected to node", "socket", ns.socketPath)
			return nil
		}

		log.Info("Failed to connect to node, retrying", "attempt", i+1, "error", err)
		select {
		case <-ns.ctx.Done():
			return fmt.Errorf("context cancelled while connecting")
		case <-time.After(backoff):
			backoff *= 2 // exponential backoff
		}
	}

	return fmt.Errorf("failed to connect to node after %d attempts", maxRetries)
}

// Close closes the connection
func (ns *NodeSender) Close() error {
	if ns.conn != nil {
		return ns.conn.Close()
	}
	return nil
}

// SendTxWithPriority sends a transaction with priority to the node
func (ns *NodeSender) SendTxWithPriority(txWithPriority types.TxWithPriority) error {
	if ns.conn == nil {
		return fmt.Errorf("not connected to node")
	}

	// Serialize using bincode-compatible format
	data := types.SerializeTxWithPriority(txWithPriority)

	// Send length-delimited message
	msgLen := uint32(len(data))
	if err := binary.Write(ns.conn, binary.BigEndian, msgLen); err != nil {
		return fmt.Errorf("failed to write message length: %w", err)
	}

	if _, err := ns.conn.Write(data); err != nil {
		return fmt.Errorf("failed to send priority tx to node: %w", err)
	}

	log.Info("Sent transaction with priority to node", "tx_bytes", len(txWithPriority.TxBytes), "priority", txWithPriority.Priority[:4])
	return nil
}

// IsConnected returns true if connected to the node
func (ns *NodeSender) IsConnected() bool {
	return ns.conn != nil
}
