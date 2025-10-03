package gateway

import (
	"encoding/json"
	"fmt"
)

type MessageHandler struct {
	txChan chan []byte
}

func NewMessageHandler(txChan chan []byte) *MessageHandler {
	return &MessageHandler{
		txChan: txChan,
	}
}

// HandleMessage processes a message received from the MEV gateway
func (h *MessageHandler) HandleMessage(data []byte) error {
	var msg GatewayMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("failed to unmarshal gateway message: %w", err)
	}

	switch msg.Type {
	case MessageTypeTransactionSubmission:
		return h.handleTransactionSubmission(msg.Payload)
	default:
		return fmt.Errorf("unknown message type: %s", msg.Type)
	}
}

func (h *MessageHandler) handleTransactionSubmission(payload interface{}) error {
	// Convert payload to TransactionSubmissionMessage
	payloadBytes, err := json.Marshal(payload)
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
			case h.txChan <- txBytes:
				// Successfully sent
			default:
				return fmt.Errorf("transaction channel full")
			}
		}
		return nil
	}

	// Handle single transaction (backward compatibility)
	if len(txSubmission.TxBytes) > 0 {
		select {
		case h.txChan <- txSubmission.TxBytes:
			return nil
		default:
			return fmt.Errorf("transaction channel full")
		}
	}

	return fmt.Errorf("no transactions in submission")
}
