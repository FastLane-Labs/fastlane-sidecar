package types

// ValidatorMessage represents messages sent from sidecar to validator IPC
// TODO: Define actual structure based on validator requirements
type ValidatorMessage struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

// GatewayResponse represents messages received from MEV gateway
// TODO: Define actual structure based on MEV gateway API
type GatewayResponse struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

// TransactionSubmission represents a transaction being sent to MEV gateway
// TODO: Extend with additional metadata as needed
type TransactionSubmission struct {
	Transaction []byte `json:"transaction"`
	Timestamp   int64  `json:"timestamp"`
	// Add more fields as needed (e.g., priority, metadata)
}
