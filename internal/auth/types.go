package auth

import (
	"crypto/ecdsa"
	"time"
)

// Delegation represents the delegation metadata signed by the validator
type Delegation struct {
	Version         string   `json:"version"`
	ChainID         string   `json:"chain_id"`
	GatewayID       string   `json:"gateway_id"`
	ValidatorPubkey string   `json:"validator_pubkey"`
	SidecarPubkey   string   `json:"sidecar_pubkey"`
	Scopes          []string `json:"scopes"`
	NotBefore       string   `json:"not_before"`
	Comment         string   `json:"comment,omitempty"`
}

// DelegationEnvelope wraps the delegation with the validator's signature
type DelegationEnvelope struct {
	Delegation Delegation `json:"delegation"`
	Signature  string     `json:"signature"` // 0x-prefixed hex, 65 bytes (R||S||V)
}

// ChallengeResponse is returned from GET /v1/auth/challenge
type ChallengeResponse struct {
	Challenge   string `json:"challenge"`
	GatewayID   string `json:"gateway_id"`
	ChainID     string `json:"chain_id"`
	AuthEnabled bool   `json:"auth_enabled"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

// RegisterRequest is sent to POST /v1/sidecars/register
type RegisterRequest struct {
	Challenge          string                 `json:"challenge"`
	SidecarPubkey      string                 `json:"sidecar_pubkey"`       // 0x-prefixed hex, 33 bytes compressed
	DelegationEnvelope DelegationEnvelope     `json:"delegation_envelope"`
	PopSignature       string                 `json:"pop_signature"`        // 0x-prefixed hex, 65 bytes
	ClientInfo         map[string]interface{} `json:"client_info,omitempty"`
}

// RegisterResponse is returned from successful registration
type RegisterResponse struct {
	SidecarID         string `json:"sidecar_id"`
	ValidatorPubkey   string `json:"validator_pubkey"`
	AccessToken       string `json:"access_token"`
	RefreshToken      string `json:"refresh_token"`
	ExpiresAt         string `json:"expires_at"` // RFC3339 timestamp
	WssURL            string `json:"wss_url,omitempty"`
	WsSubprotocol     string `json:"ws_subprotocol,omitempty"`
	HeartbeatInterval int    `json:"heartbeat_interval,omitempty"` // seconds
	MaxInflight       int    `json:"max_inflight,omitempty"`
}

// RefreshRequest is sent to POST /v1/auth/refresh
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
	Challenge    string `json:"challenge"`
	PopSignature string `json:"pop_signature"` // 0x-prefixed hex, 65 bytes
}

// RefreshResponse is returned from successful token refresh
type RefreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at"` // RFC3339 timestamp
}

// Credentials holds the sidecar's authentication state
type Credentials struct {
	SidecarKey         *ecdsa.PrivateKey
	DelegationEnvelope *DelegationEnvelope
	SidecarID          string
	AccessToken        string
	RefreshToken       string
	TokenExpiry        time.Time
	SessionNonce       string // From validator_register response
}
