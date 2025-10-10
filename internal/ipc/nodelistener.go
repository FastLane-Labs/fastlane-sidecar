package ipc

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
)

// NodeListener listens for transactions from the node
type NodeListener struct {
	socketPath string
	listener   net.Listener
	ctx        context.Context
	txChan     chan []byte
}

// NewNodeListener creates a new node listener
func NewNodeListener(ctx context.Context, socketPath string) *NodeListener {
	return &NodeListener{
		socketPath: socketPath,
		ctx:        ctx,
		txChan:     make(chan []byte, 100),
	}
}

// Start starts listening for transactions from the node
func (nl *NodeListener) Start() error {
	// Remove existing socket file
	if err := removeSocketFile(nl.socketPath); err != nil {
		log.Error("Error removing socket file", "error", err, "path", nl.socketPath)
	}

	listener, err := net.Listen("unix", nl.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket %s: %w", nl.socketPath, err)
	}

	nl.listener = listener
	log.Info("Node listener started", "socket", nl.socketPath)

	go nl.acceptConnections()
	return nil
}

// Stop stops the listener
func (nl *NodeListener) Stop() error {
	if nl.listener != nil {
		return nl.listener.Close()
	}
	return nil
}

// GetTransactionChannel returns the channel for receiving transactions
func (nl *NodeListener) GetTransactionChannel() <-chan []byte {
	return nl.txChan
}

// acceptConnections accepts incoming connections from the node
func (nl *NodeListener) acceptConnections() {
	defer nl.listener.Close()

	for {
		select {
		case <-nl.ctx.Done():
			return
		default:
			conn, err := nl.listener.Accept()
			if err != nil {
				log.Error("Failed to accept connection", "error", err)
				continue
			}

			go nl.handleNodeConnection(conn)
		}
	}
}

// handleNodeConnection handles a connection from the node
func (nl *NodeListener) handleNodeConnection(conn net.Conn) {
	defer conn.Close()
	log.Info("Node connected", "socket", nl.socketPath)

	msgCount := 0
	for {
		select {
		case <-nl.ctx.Done():
			return
		default:
			// Read length-delimited message
			var msgLen uint32
			if err := binary.Read(conn, binary.BigEndian, &msgLen); err != nil {
				if err != io.EOF {
					log.Error("Error reading message length", "error", err)
				}
				return
			}

			// Read message data (raw transaction bytes)
			msgData := make([]byte, msgLen)
			if _, err := io.ReadFull(conn, msgData); err != nil {
				log.Error("Error reading message data", "error", err)
				return
			}

			msgCount++
			log.Debug("Message received from node", "count", msgCount, "bytes", len(msgData))

			// Send to processing channel (raw transaction bytes)
			select {
			case nl.txChan <- msgData:
			default:
				log.Error("Message channel full, dropping message", "count", msgCount)
			}
		}
	}
}

// removeSocketFile removes an existing socket file
func removeSocketFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return os.Remove(path)
	}
	return nil
}
