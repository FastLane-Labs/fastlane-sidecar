package ipc

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
	"github.com/ethereum/go-ethereum/core/types"
)

type Client struct {
	path string
	conn net.Conn
	ctx  context.Context
}

func NewClient(path string, ctx context.Context) *Client {
	return &Client{
		path: path,
		ctx:  ctx,
	}
}

func (c *Client) Connect() error {
	conn, err := net.Dial("unix", c.path)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	c.conn = conn
	log.Info("IPC connected", "endpoint", c.path)
	return nil
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) ReadTransaction() (*types.Transaction, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	data, err := ReadFrame(c.conn)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return nil, nil
		}
		return nil, err
	}

	var tx types.Transaction
	if err := tx.UnmarshalBinary(data); err != nil {
		return nil, fmt.Errorf("failed to decode transaction: %w", err)
	}

	return &tx, nil
}

func (c *Client) WriteData(data []byte) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	return WriteFrame(c.conn, data)
}
