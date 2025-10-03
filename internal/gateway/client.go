package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/websocket"
)

type Client struct {
	url    string
	ctx    context.Context
	conn   *websocket.Conn
	connMu sync.RWMutex
	txChan chan []byte // Channel for receiving transactions from gateway

	// Reconnection state
	reconnectDelay    time.Duration
	maxReconnectDelay time.Duration
}

func NewClient(url string, ctx context.Context) *Client {
	return &Client{
		url:               url,
		ctx:               ctx,
		txChan:            make(chan []byte, 100), // Buffer for incoming transactions
		reconnectDelay:    1 * time.Second,
		maxReconnectDelay: 60 * time.Second,
	}
}

func (c *Client) Connect() error {
	conn, _, err := websocket.DefaultDialer.Dial(c.url, nil)
	if err != nil {
		log.Warn("Failed to connect to gateway, will retry", "url", c.url, "error", err)
		// Don't return error - we'll keep retrying in background
		go c.reconnectLoop()
		return nil
	}

	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()

	log.Info("Connected to gateway", "url", c.url)

	// Start message reader
	go c.readMessages()

	return nil
}

func (c *Client) reconnectLoop() {
	delay := c.reconnectDelay

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-time.After(delay):
			conn, _, err := websocket.DefaultDialer.Dial(c.url, nil)
			if err != nil {
				log.Debug("Gateway reconnection failed", "error", err, "retry_in", delay)
				// Exponential backoff
				delay *= 2
				if delay > c.maxReconnectDelay {
					delay = c.maxReconnectDelay
				}
				continue
			}

			c.connMu.Lock()
			c.conn = conn
			c.connMu.Unlock()

			log.Info("Reconnected to gateway", "url", c.url)

			// Start reading messages
			go c.readMessages()
			return
		}
	}
}

func (c *Client) readMessages() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			c.connMu.RLock()
			conn := c.conn
			c.connMu.RUnlock()

			if conn == nil {
				return
			}

			var msg GatewayMessage
			err := conn.ReadJSON(&msg)
			if err != nil {
				log.Error("Error reading from gateway", "error", err)
				c.connMu.Lock()
				c.conn = nil
				c.connMu.Unlock()
				// Trigger reconnection
				go c.reconnectLoop()
				return
			}

			if err := c.handleMessage(msg); err != nil {
				log.Error("Error handling gateway message", "error", err, "type", msg.Type)
			}
		}
	}
}

func (c *Client) handleMessage(msg GatewayMessage) error {
	switch msg.Type {
	case MessageTypeTransactionSubmission:
		// Parse payload as TransactionSubmissionMessage
		payloadBytes, err := json.Marshal(msg.Payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}

		var txSubmission TransactionSubmissionMessage
		if err := json.Unmarshal(payloadBytes, &txSubmission); err != nil {
			return fmt.Errorf("failed to unmarshal transaction submission: %w", err)
		}

		// Handle array of transactions
		if len(txSubmission.Transactions) > 0 {
			for _, txBytes := range txSubmission.Transactions {
				select {
				case c.txChan <- txBytes:
					log.Info("Received transaction from gateway", "bytes", len(txBytes))
				default:
					log.Warn("Transaction channel full, dropping transaction from gateway")
				}
			}
		} else if len(txSubmission.TxBytes) > 0 {
			// Handle single transaction (backward compatibility)
			select {
			case c.txChan <- txSubmission.TxBytes:
				log.Info("Received transaction from gateway", "bytes", len(txSubmission.TxBytes))
			default:
				log.Warn("Transaction channel full, dropping transaction from gateway")
			}
		}

	default:
		log.Warn("Unknown message type from gateway", "type", msg.Type)
	}

	return nil
}

func (c *Client) SendTransactionBytes(txBytes []byte) error {
	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		log.Debug("Not connected to gateway, skipping transaction send")
		return fmt.Errorf("not connected to gateway")
	}

	msg := GatewayMessage{
		Type: MessageTypeTransaction,
		Payload: TransactionMessage{
			TxBytes:   txBytes,
			Timestamp: time.Now().Unix(),
			Source:    "node",
		},
	}

	if err := conn.WriteJSON(msg); err != nil {
		log.Error("Failed to send transaction to gateway", "error", err)
		return err
	}

	log.Debug("Sent transaction to gateway", "bytes", len(txBytes))
	return nil
}

func (c *Client) NotifyTransactionDropped(txHash common.Hash) error {
	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		log.Debug("Not connected to gateway, skipping drop notification")
		return fmt.Errorf("not connected to gateway")
	}

	msg := GatewayMessage{
		Type: MessageTypeTransactionDropped,
		Payload: TransactionDroppedMessage{
			TxHash:    txHash.Hex(),
			Timestamp: time.Now().Unix(),
		},
	}

	if err := conn.WriteJSON(msg); err != nil {
		log.Error("Failed to notify gateway of dropped transaction", "error", err)
		return err
	}

	log.Debug("Notified gateway of dropped transaction", "hash", txHash.Hex())
	return nil
}

func (c *Client) GetTransactionChannel() <-chan []byte {
	return c.txChan
}

func (c *Client) Close() error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn != nil {
		// Send close message
		err := c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		if err != nil {
			log.Warn("Error sending close message", "error", err)
		}
		err = c.conn.Close()
		c.conn = nil
		close(c.txChan)
		return err
	}

	close(c.txChan)
	return nil
}
