package gateway

import (
	"context"
	"fmt"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
	"github.com/ethereum/go-ethereum/common"
)

type Client struct {
	url    string
	ctx    context.Context
	txChan chan []byte // Channel for receiving transactions from gateway
	// TODO: Add WebSocket connection
}

func NewClient(url string, ctx context.Context) *Client {
	return &Client{
		url:    url,
		ctx:    ctx,
		txChan: make(chan []byte, 100), // Buffer for incoming transactions
	}
}

func (c *Client) Connect() error {
	// TODO: Implement WebSocket connection to MEV gateway
	return fmt.Errorf("not implemented")
}

func (c *Client) SendTransactionBytes(txBytes []byte) error {
	// TODO: Implement transaction sending to MEV gateway
	// For now, just log that we would send it
	log.Info("Would send transaction to gateway", "bytes", len(txBytes))
	return nil
}

func (c *Client) NotifyTransactionDropped(txHash common.Hash) error {
	// TODO: Implement transaction drop notification to MEV gateway
	// For now, just log that we would notify it
	log.Info("Would notify gateway of dropped transaction", "hash", txHash.Hex())
	return nil
}

func (c *Client) GetTransactionChannel() <-chan []byte {
	return c.txChan
}

func (c *Client) Close() error {
	// TODO: Implement connection cleanup
	close(c.txChan)
	return nil
}
