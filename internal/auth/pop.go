package auth

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gowebpki/jcs"
	"lukechampine.com/blake3"
)

const (
	RegisterContext = "fastlane/register/v1"
	RefreshContext  = "fastlane/refresh/v1"
)

// CreateRegisterPoP creates a Proof-of-Possession signature for registration
// Following the spec from monad-mev-gateway/docs/guides/sidecar-integration.md
func CreateRegisterPoP(challenge string, bodyHashHex string, sidecarKey *ecdsa.PrivateKey) (string, error) {
	// Create PoP object per spec: {ctx, challenge, body_hash}
	popObj := map[string]interface{}{
		"ctx":       RegisterContext,
		"challenge": challenge,
		"body_hash": strings.ToLower(bodyHashHex),
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(popObj)
	if err != nil {
		return "", fmt.Errorf("failed to marshal PoP: %w", err)
	}

	// Canonicalize using JCS (RFC 8785)
	canonical, err := jcs.Transform(jsonData)
	if err != nil {
		return "", fmt.Errorf("failed to canonicalize PoP: %w", err)
	}

	// Compute digest: BLAKE3(ctx || JCS(pop-object))
	hasher := blake3.New(32, nil)
	if _, err := hasher.Write([]byte(RegisterContext)); err != nil {
		return "", fmt.Errorf("failed to hash domain: %w", err)
	}
	if _, err := hasher.Write(canonical); err != nil {
		return "", fmt.Errorf("failed to hash canonical: %w", err)
	}
	hash := hasher.Sum(nil)

	// Sign the hash with recoverable signature (65 bytes: R||S||V)
	sig, err := crypto.Sign(hash, sidecarKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign PoP: %w", err)
	}

	// Ensure low-S value (malleability fix)
	sig = normalizeLowS(sig)

	return "0x" + hex.EncodeToString(sig), nil
}

// CreateRefreshPoP creates a Proof-of-Possession signature for token refresh
func CreateRefreshPoP(challenge, refreshToken, sessionNonce string, sidecarKey *ecdsa.PrivateKey) (string, error) {
	// Hash the refresh token with SHA256
	tokenHash := sha256.Sum256([]byte(refreshToken))
	tokenHashHex := strings.ToLower(hex.EncodeToString(tokenHash[:]))

	// Create refresh PoP object
	popObj := map[string]interface{}{
		"ctx":       RefreshContext,
		"challenge": challenge,
		"token_hash": tokenHashHex,
	}

	// Add session_nonce if available (for in-band refresh)
	if sessionNonce != "" {
		popObj["session_nonce"] = sessionNonce
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(popObj)
	if err != nil {
		return "", fmt.Errorf("failed to marshal refresh PoP: %w", err)
	}

	// Canonicalize using JCS
	canonical, err := jcs.Transform(jsonData)
	if err != nil {
		return "", fmt.Errorf("failed to canonicalize refresh PoP: %w", err)
	}

	// Compute digest: BLAKE3(ctx || canonical)
	hasher := blake3.New(32, nil)
	if _, err := hasher.Write([]byte(RefreshContext)); err != nil {
		return "", fmt.Errorf("failed to hash domain: %w", err)
	}
	if _, err := hasher.Write(canonical); err != nil {
		return "", fmt.Errorf("failed to hash canonical: %w", err)
	}
	digest := hasher.Sum(nil)

	// Sign the digest
	sig, err := crypto.Sign(digest, sidecarKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign refresh PoP: %w", err)
	}

	// Ensure low-S value
	sig = normalizeLowS(sig)

	return "0x" + hex.EncodeToString(sig), nil
}

// ComputeBodyHash computes the BLAKE3 hash of canonicalized JSON
func ComputeBodyHash(bodyObj interface{}) (string, error) {
	// Marshal to JSON
	jsonData, err := json.Marshal(bodyObj)
	if err != nil {
		return "", fmt.Errorf("failed to marshal body: %w", err)
	}

	// Canonicalize using JCS
	canonical, err := jcs.Transform(jsonData)
	if err != nil {
		return "", fmt.Errorf("failed to canonicalize body: %w", err)
	}

	// Compute BLAKE3 hash
	hash := blake3.Sum256(canonical)

	return hex.EncodeToString(hash[:]), nil
}

// normalizeLowS ensures the signature has low-S value to prevent malleability
func normalizeLowS(sig []byte) []byte {
	if len(sig) != 65 {
		return sig
	}

	// Extract R, S, V
	var r, s big.Int
	r.SetBytes(sig[:32])
	s.SetBytes(sig[32:64])
	v := sig[64]

	// Get curve order and half order
	curveOrder := crypto.S256().Params().N
	halfOrder := new(big.Int).Rsh(curveOrder, 1)

	// If S > N/2, compute S' = N - S
	if s.Cmp(halfOrder) > 0 {
		s.Sub(curveOrder, &s)

		// Pad s to 32 bytes
		sBytes := s.Bytes()
		sPadded := make([]byte, 32)
		copy(sPadded[32-len(sBytes):], sBytes)

		// Update signature
		copy(sig[32:64], sPadded)
	}

	// Keep V as is (already set by crypto.Sign to be recoverable)
	sig[64] = v

	return sig
}
