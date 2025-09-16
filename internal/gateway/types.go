package gateway

// TODO: Define message types for communication with MEV gateway
// These types will be determined based on the MEV gateway API specification

type GatewayMessage struct {
	// Placeholder for gateway message structure
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}
