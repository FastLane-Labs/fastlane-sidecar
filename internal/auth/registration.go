package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
)

// RegistrationClient handles sidecar registration with the MEV gateway
type RegistrationClient struct {
	gatewayURL string
	httpClient *http.Client
}

// NewRegistrationClient creates a new registration client
func NewRegistrationClient(gatewayURL string) *RegistrationClient {
	return &RegistrationClient{
		gatewayURL: gatewayURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Preserve the original HTTP method on redirects
				// By default, Go converts POST to GET on 301/302 redirects
				if len(via) > 0 {
					req.Method = via[0].Method
				}
				// Allow up to 10 redirects
				if len(via) >= 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				return nil
			},
		},
	}
}

// GetChallenge fetches an authentication challenge from the gateway
func (rc *RegistrationClient) GetChallenge(ctx context.Context) (*ChallengeResponse, error) {
	url := rc.gatewayURL + "/v1/auth/challenge"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create challenge request: %w", err)
	}

	resp, err := rc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get challenge: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read challenge response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("challenge request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Log raw challenge response for debugging
	log.Debug("Raw challenge response", "body", string(body))

	var challengeResp ChallengeResponse
	if err := json.Unmarshal(body, &challengeResp); err != nil {
		return nil, fmt.Errorf("failed to parse challenge response: %w", err)
	}

	// Log challenge safely (truncate only if longer than 16 chars)
	challengePreview := challengeResp.Challenge
	if len(challengePreview) > 16 {
		challengePreview = challengePreview[:16] + "..."
	}
	log.Debug("Got challenge from gateway",
		"challenge", challengePreview,
		"gateway_id", challengeResp.GatewayID,
		"chain_id", challengeResp.ChainID)
	return &challengeResp, nil
}

// Register performs the complete registration flow
func (rc *RegistrationClient) Register(ctx context.Context, creds *Credentials) (*RegisterResponse, error) {
	// Step 1: Get challenge
	challengeResp, err := rc.GetChallenge(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get challenge: %w", err)
	}

	// Step 2: Get sidecar public key
	sidecarPubkey := GetSidecarPubkeyHex(creds.SidecarKey)

	// Step 3: Build register request body (without pop_signature)
	bodyObj := map[string]interface{}{
		"challenge":           challengeResp.Challenge,
		"sidecar_pubkey":      sidecarPubkey,
		"delegation_envelope": creds.DelegationEnvelope,
		"client_info": map[string]interface{}{
			"name":    "fastlane-sidecar",
			"version": "1.0.0",
		},
	}

	// Step 4: Compute body hash
	bodyHash, err := ComputeBodyHash(bodyObj)
	if err != nil {
		return nil, fmt.Errorf("failed to compute body hash: %w", err)
	}

	// Step 5: Create PoP signature
	popSignature, err := CreateRegisterPoP(challengeResp.Challenge, bodyHash, creds.SidecarKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create PoP: %w", err)
	}

	// Step 6: Build final register request
	registerReq := RegisterRequest{
		Challenge:          challengeResp.Challenge,
		SidecarPubkey:      sidecarPubkey,
		DelegationEnvelope: *creds.DelegationEnvelope,
		PopSignature:       popSignature,
		ClientInfo: map[string]interface{}{
			"name":    "fastlane-sidecar",
			"version": "1.0.0",
		},
	}

	reqBody, err := json.Marshal(registerReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal register request: %w", err)
	}

	// Step 7: POST register
	url := rc.gatewayURL + "/v1/sidecars/register"

	// Log request details for debugging
	log.Debug("Registration request details",
		"challenge_len", len(registerReq.Challenge),
		"sidecar_pubkey", registerReq.SidecarPubkey,
		"validator_pubkey", registerReq.DelegationEnvelope.Delegation.ValidatorPubkey,
		"delegation_gateway_id", registerReq.DelegationEnvelope.Delegation.GatewayID,
		"delegation_chain_id", registerReq.DelegationEnvelope.Delegation.ChainID,
		"delegation_signature_len", len(registerReq.DelegationEnvelope.Signature),
		"pop_signature_len", len(registerReq.PopSignature),
		"body_size", len(reqBody))

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create register request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	log.Debug("Sending registration request", "method", req.Method, "url", url)

	resp, err := rc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send register request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read register response: %w", err)
	}

	log.Debug("Received registration response", "status_code", resp.StatusCode)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("registration failed with status %d: %s", resp.StatusCode, string(body))
	}

	var registerResp RegisterResponse
	if err := json.Unmarshal(body, &registerResp); err != nil {
		return nil, fmt.Errorf("failed to parse register response: %w", err)
	}

	log.Info("Successfully registered with gateway", "sidecar_id", registerResp.SidecarID)
	return &registerResp, nil
}

// RefreshTokens refreshes the JWT access and refresh tokens
func (rc *RegistrationClient) RefreshTokens(ctx context.Context, creds *Credentials) (*RefreshResponse, error) {
	// Step 1: Get fresh challenge
	challengeResp, err := rc.GetChallenge(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get challenge for refresh: %w", err)
	}

	// Step 2: Create refresh PoP (without session_nonce for HTTP refresh)
	popSignature, err := CreateRefreshPoP(challengeResp.Challenge, creds.RefreshToken, "", creds.SidecarKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh PoP: %w", err)
	}

	// Step 3: Build refresh request
	refreshReq := RefreshRequest{
		RefreshToken: creds.RefreshToken,
		Challenge:    challengeResp.Challenge,
		PopSignature: popSignature,
	}

	reqBody, err := json.Marshal(refreshReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal refresh request: %w", err)
	}

	// Step 4: POST refresh
	url := rc.gatewayURL + "/v1/auth/refresh"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := rc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh failed with status %d: %s", resp.StatusCode, string(body))
	}

	var refreshResp RefreshResponse
	if err := json.Unmarshal(body, &refreshResp); err != nil {
		return nil, fmt.Errorf("failed to parse refresh response: %w", err)
	}

	log.Info("Successfully refreshed tokens")
	return &refreshResp, nil
}

// ParseExpiryTime parses RFC3339 timestamp to time.Time
func ParseExpiryTime(expiresAt string) (time.Time, error) {
	if expiresAt == "" {
		// Default to 10 minutes if not provided
		return time.Now().Add(10 * time.Minute), nil
	}

	t, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse expires_at: %w", err)
	}

	return t, nil
}

// GetWebSocketURL converts HTTP gateway URL to WebSocket URL
func GetWebSocketURL(gatewayURL, wssURLOverride string) string {
	if wssURLOverride != "" {
		return wssURLOverride
	}

	// Convert HTTP(S) to WS(S)
	wsURL := strings.Replace(gatewayURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)

	// Add /ws path if not present
	if !strings.HasSuffix(wsURL, "/ws") {
		wsURL += "/ws"
	}

	return wsURL
}
