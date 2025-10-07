package gateway

// JSON-RPC types for MEV Gateway communication

// JSONRPCRequest represents a JSON-RPC 2.0 request
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      int64       `json:"id"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
	ID      int64       `json:"id"`
}

// JSONRPCNotification represents a JSON-RPC 2.0 notification (no ID)
type JSONRPCNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// RPCError represents a JSON-RPC error
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Legacy message types (deprecated, kept for backward compatibility)
type MessageType string

const (
	// Messages from sidecar to gateway
	MessageTypeTransaction        MessageType = "transaction"
	MessageTypeTransactionDropped MessageType = "transaction_dropped"

	// Messages from gateway to sidecar
	MessageTypeTransactionSubmission MessageType = "transaction_submission"
)

// GatewayMessage represents a message sent to/from the MEV gateway (legacy)
type GatewayMessage struct {
	Type    MessageType `json:"type"`
	Payload interface{} `json:"payload"`
}

// TransactionMessage represents a transaction being sent to the gateway (legacy)
type TransactionMessage struct {
	TxBytes   []byte `json:"tx_bytes"`  // RLP-encoded transaction
	Timestamp int64  `json:"timestamp"` // Unix timestamp
	Source    string `json:"source"`    // "node" or other source identifier
}

// TransactionDroppedMessage represents a transaction drop notification (legacy)
type TransactionDroppedMessage struct {
	TxHash    string `json:"tx_hash"`   // Transaction hash (hex)
	Timestamp int64  `json:"timestamp"` // Unix timestamp
}

// TransactionSubmissionMessage represents a transaction or batch of transactions received from gateway (legacy)
type TransactionSubmissionMessage struct {
	TxBytes      []byte   `json:"tx_bytes,omitempty"`     // RLP-encoded single transaction (deprecated, use Transactions)
	Transactions [][]byte `json:"transactions,omitempty"` // Array of RLP-encoded transactions
	Timestamp    int64    `json:"timestamp"`              // Unix timestamp
}
