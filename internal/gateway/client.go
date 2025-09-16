package gateway

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/core/types"
)

type Client struct {
	url string
	ctx context.Context
	// TODO: Add WebSocket connection
}

func NewClient(url string, ctx context.Context) *Client {
	return &Client{
		url: url,
		ctx: ctx,
	}
}

func (c *Client) Connect() error {
	// TODO: Implement WebSocket connection to MEV gateway
	return fmt.Errorf("not implemented")
}

func (c *Client) SendTransaction(tx *types.Transaction) error {
	// TODO: Implement transaction sending to MEV gateway
	return fmt.Errorf("not implemented")
}

func (c *Client) Close() error {
	// TODO: Implement connection cleanup
	return nil
}
