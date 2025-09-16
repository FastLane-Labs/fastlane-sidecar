package ipc

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

const (
	MaxMessageSize = 10 * 1024 * 1024
	HeaderSize     = 4
)

func ReadFrame(conn net.Conn) ([]byte, error) {
	lengthBuf := make([]byte, HeaderSize)
	_, err := io.ReadFull(conn, lengthBuf)
	if err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("connection closed")
		}
		return nil, fmt.Errorf("reading length header: %w", err)
	}

	messageLen := binary.BigEndian.Uint32(lengthBuf)

	if messageLen > MaxMessageSize {
		_, err = io.CopyN(io.Discard, conn, int64(messageLen))
		if err != nil {
			return nil, fmt.Errorf("discarding oversized message: %w", err)
		}
		return nil, fmt.Errorf("message too large: %d bytes", messageLen)
	}

	data := make([]byte, messageLen)
	_, err = io.ReadFull(conn, data)
	if err != nil {
		return nil, fmt.Errorf("reading message data: %w", err)
	}

	return data, nil
}

func WriteFrame(conn net.Conn, data []byte) error {
	if len(data) > MaxMessageSize {
		return fmt.Errorf("message too large: %d bytes", len(data))
	}

	lengthBuf := make([]byte, HeaderSize)
	binary.BigEndian.PutUint32(lengthBuf, uint32(len(data)))

	if _, err := conn.Write(lengthBuf); err != nil {
		return fmt.Errorf("writing length header: %w", err)
	}

	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("writing message data: %w", err)
	}

	return nil
}
