package gateway

// MessageType represents the type of message sent to/from gateway
type MessageType string

const (
	// Messages from sidecar to gateway
	MessageTypeTransaction        MessageType = "transaction"
	MessageTypeTransactionDropped MessageType = "transaction_dropped"

	// Messages from gateway to sidecar
	MessageTypeTransactionSubmission MessageType = "transaction_submission"
)

// GatewayMessage represents a message sent to/from the MEV gateway
type GatewayMessage struct {
	Type    MessageType `json:"type"`
	Payload interface{} `json:"payload"`
}

// TransactionMessage represents a transaction being sent to the gateway
type TransactionMessage struct {
	TxBytes   []byte `json:"tx_bytes"`  // RLP-encoded transaction
	Timestamp int64  `json:"timestamp"` // Unix timestamp
	Source    string `json:"source"`    // "node" or other source identifier
}

// TransactionDroppedMessage represents a transaction drop notification
type TransactionDroppedMessage struct {
	TxHash    string `json:"tx_hash"`   // Transaction hash (hex)
	Timestamp int64  `json:"timestamp"` // Unix timestamp
}

// TransactionSubmissionMessage represents a transaction or batch of transactions received from gateway
type TransactionSubmissionMessage struct {
	TxBytes      []byte   `json:"tx_bytes,omitempty"`     // RLP-encoded single transaction (deprecated, use Transactions)
	Transactions [][]byte `json:"transactions,omitempty"` // Array of RLP-encoded transactions
	Timestamp    int64    `json:"timestamp"`              // Unix timestamp
}
