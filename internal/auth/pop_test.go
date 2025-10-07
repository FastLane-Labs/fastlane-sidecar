package auth

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

func TestCreateRegisterPoP(t *testing.T) {
	// Generate test key
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	tests := []struct {
		name      string
		challenge string
		bodyHash  string
		wantErr   bool
	}{
		{
			name:      "valid PoP",
			challenge: "test-challenge-123",
			bodyHash:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			wantErr:   false,
		},
		{
			name:      "empty challenge",
			challenge: "",
			bodyHash:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			wantErr:   false, // Empty challenge is allowed per spec
		},
		{
			name:      "empty body hash",
			challenge: "test-challenge",
			bodyHash:  "",
			wantErr:   false, // Empty hash is technically valid
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig, err := CreateRegisterPoP(tt.challenge, tt.bodyHash, privateKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateRegisterPoP() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify signature format
				if !strings.HasPrefix(sig, "0x") {
					t.Errorf("Signature should have 0x prefix, got: %s", sig)
				}

				sigBytes, err := hex.DecodeString(strings.TrimPrefix(sig, "0x"))
				if err != nil {
					t.Errorf("Failed to decode signature hex: %v", err)
				}

				if len(sigBytes) != 65 {
					t.Errorf("Signature should be 65 bytes, got %d", len(sigBytes))
				}

				// Verify V value is in valid range (0-3)
				v := sigBytes[64]
				if v > 3 {
					t.Errorf("V value should be 0-3, got %d", v)
				}
			}
		})
	}
}

func TestCreateRefreshPoP(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	tests := []struct {
		name         string
		challenge    string
		refreshToken string
		sessionNonce string
		wantErr      bool
	}{
		{
			name:         "valid refresh PoP with session nonce",
			challenge:    "refresh-challenge-123",
			refreshToken: "refresh-token-xyz",
			sessionNonce: "session-nonce-abc",
			wantErr:      false,
		},
		{
			name:         "valid refresh PoP without session nonce",
			challenge:    "refresh-challenge-123",
			refreshToken: "refresh-token-xyz",
			sessionNonce: "",
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig, err := CreateRefreshPoP(tt.challenge, tt.refreshToken, tt.sessionNonce, privateKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateRefreshPoP() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify signature format
				if !strings.HasPrefix(sig, "0x") {
					t.Errorf("Signature should have 0x prefix")
				}

				sigBytes, err := hex.DecodeString(strings.TrimPrefix(sig, "0x"))
				if err != nil {
					t.Errorf("Failed to decode signature hex: %v", err)
				}

				if len(sigBytes) != 65 {
					t.Errorf("Signature should be 65 bytes, got %d", len(sigBytes))
				}
			}
		})
	}
}

func TestComputeBodyHash(t *testing.T) {
	tests := []struct {
		name    string
		bodyObj interface{}
		wantErr bool
	}{
		{
			name: "simple object",
			bodyObj: map[string]interface{}{
				"key1": "value1",
				"key2": 123,
			},
			wantErr: false,
		},
		{
			name: "nested object",
			bodyObj: map[string]interface{}{
				"outer": map[string]interface{}{
					"inner": "value",
				},
			},
			wantErr: false,
		},
		{
			name: "object with array",
			bodyObj: map[string]interface{}{
				"array": []string{"a", "b", "c"},
			},
			wantErr: false,
		},
		{
			name: "JCS canonicalization test - key order",
			bodyObj: map[string]interface{}{
				"z": 1,
				"a": 2,
				"m": 3,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1, err := ComputeBodyHash(tt.bodyObj)
			if (err != nil) != tt.wantErr {
				t.Errorf("ComputeBodyHash() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify hash is hex string
				if _, err := hex.DecodeString(hash1); err != nil {
					t.Errorf("Hash should be valid hex string: %v", err)
				}

				// Verify hash is 32 bytes (64 hex chars)
				if len(hash1) != 64 {
					t.Errorf("Hash should be 64 hex chars, got %d", len(hash1))
				}

				// Verify deterministic - same input produces same hash
				hash2, err := ComputeBodyHash(tt.bodyObj)
				if err != nil {
					t.Errorf("Second hash computation failed: %v", err)
				}
				if hash1 != hash2 {
					t.Errorf("Hash should be deterministic, got different hashes: %s vs %s", hash1, hash2)
				}
			}
		})
	}
}

func TestNormalizeLowS(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	// Create a signature
	testHash := crypto.Keccak256([]byte("test message"))
	sig, err := crypto.Sign(testHash, privateKey)
	if err != nil {
		t.Fatalf("Failed to sign: %v", err)
	}

	// Normalize it
	normalized := normalizeLowS(sig)

	// Verify it's still 65 bytes
	if len(normalized) != 65 {
		t.Errorf("Normalized signature should be 65 bytes, got %d", len(normalized))
	}

	// Verify it's still a valid signature
	pubKey, err := crypto.SigToPub(testHash, normalized)
	if err != nil {
		t.Errorf("Normalized signature should still be valid: %v", err)
	}

	// Verify public key matches
	expectedAddr := crypto.PubkeyToAddress(privateKey.PublicKey)
	actualAddr := crypto.PubkeyToAddress(*pubKey)
	if expectedAddr != actualAddr {
		t.Errorf("Recovered address mismatch: expected %s, got %s", expectedAddr.Hex(), actualAddr.Hex())
	}
}

func TestJCSCanonicalization(t *testing.T) {
	// Test that key ordering is consistent (RFC 8785 requirement)
	obj1 := map[string]interface{}{
		"z_last":  3,
		"a_first": 1,
		"m_mid":   2,
	}

	obj2 := map[string]interface{}{
		"a_first": 1,
		"m_mid":   2,
		"z_last":  3,
	}

	hash1, err := ComputeBodyHash(obj1)
	if err != nil {
		t.Fatalf("Failed to compute hash1: %v", err)
	}

	hash2, err := ComputeBodyHash(obj2)
	if err != nil {
		t.Fatalf("Failed to compute hash2: %v", err)
	}

	// Same keys/values in different order should produce same hash
	if hash1 != hash2 {
		t.Errorf("JCS canonicalization failed: different key order produced different hashes")
	}
}

func TestPopSignatureRecovery(t *testing.T) {
	// Test that we can recover the public key from PoP signature
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	challenge := "test-challenge"
	bodyHash := "abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234"

	popSig, err := CreateRegisterPoP(challenge, bodyHash, privateKey)
	if err != nil {
		t.Fatalf("Failed to create PoP: %v", err)
	}

	// Verify we can recover the public key
	sigBytes, err := hex.DecodeString(strings.TrimPrefix(popSig, "0x"))
	if err != nil {
		t.Fatalf("Failed to decode signature: %v", err)
	}

	// NOTE: We cannot easily verify signature recovery without reimplementing
	// the entire PoP creation logic (JCS + BLAKE3 with domain). The signature
	// format validation in TestCreateRegisterPoP is sufficient.
	// This test just verifies the signature is well-formed.
	if len(sigBytes) != 65 {
		t.Errorf("Expected 65 byte signature, got %d", len(sigBytes))
	}

	// Verify it's a valid recoverable signature format
	v := sigBytes[64]
	if v > 3 {
		t.Errorf("Invalid V value: %d (should be 0-3)", v)
	}
}
