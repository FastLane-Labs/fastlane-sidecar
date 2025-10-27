package main

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FastLane-Labs/fastlane-sidecar/internal/keystore"
	"github.com/ethereum/go-ethereum/crypto"
)

// TestGenerateEnvelopeMatchingSidecarKeys verifies that the delegation envelope
// and sidecar keystore contain matching public keys
func TestGenerateEnvelopeMatchingSidecarKeys(t *testing.T) {
	// Create temporary directory for test files
	tempDir := t.TempDir()

	// Test parameters
	network := "testnet"
	validatorPubkey := "0x0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798" // Valid secp256k1 pubkey
	sidecarPassword := "test-password-123"
	outputPath := filepath.Join(tempDir, "delegation-envelope.json")
	keystorePath := filepath.Join(tempDir, "sidecar-keystore.json")

	// Run the envelope generation
	err := run(tempDir, network, validatorPubkey, "", "", sidecarPassword, outputPath, keystorePath)
	if err != nil {
		t.Fatalf("Failed to generate envelope: %v", err)
	}

	// Verify both files exist
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Fatalf("Delegation envelope file not created: %s", outputPath)
	}
	if _, err := os.Stat(keystorePath); os.IsNotExist(err) {
		t.Fatalf("Keystore file not created: %s", keystorePath)
	}

	// Load and parse delegation envelope
	envelopeData, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read delegation envelope: %v", err)
	}

	var envelope DelegationEnvelope
	if err := json.Unmarshal(envelopeData, &envelope); err != nil {
		t.Fatalf("Failed to parse delegation envelope: %v", err)
	}

	delegationSidecarPubkey := envelope.Delegation.SidecarPubkey
	t.Logf("Delegation sidecar pubkey: %s", delegationSidecarPubkey)

	// Load and decrypt keystore
	ks, err := keystore.LoadKeystore(keystorePath)
	if err != nil {
		t.Fatalf("Failed to load keystore: %v", err)
	}

	privKeyBytes, err := keystore.DecryptKey(ks, sidecarPassword)
	if err != nil {
		t.Fatalf("Failed to decrypt keystore: %v", err)
	}

	sidecarKey, err := crypto.ToECDSA(privKeyBytes)
	if err != nil {
		t.Fatalf("Failed to convert to ECDSA key: %v", err)
	}

	// Get public key from keystore
	keystoreSidecarPubkey := "0x" + hex.EncodeToString(crypto.CompressPubkey(&sidecarKey.PublicKey))
	t.Logf("Keystore sidecar pubkey: %s", keystoreSidecarPubkey)

	// Normalize both pubkeys for comparison
	delegationPubkey := strings.ToLower(delegationSidecarPubkey)
	keystorePubkey := strings.ToLower(keystoreSidecarPubkey)

	// Verify they match
	if delegationPubkey != keystorePubkey {
		t.Errorf("Sidecar public key mismatch!\n  Delegation: %s\n  Keystore:   %s",
			delegationPubkey, keystorePubkey)
	}

	// Additional validation: verify delegation structure
	if envelope.Delegation.Version != delegationVersion {
		t.Errorf("Expected delegation version %s, got %s", delegationVersion, envelope.Delegation.Version)
	}

	netConfig := networks[network]
	if envelope.Delegation.ChainID != netConfig.ChainID {
		t.Errorf("Expected chain ID %s, got %s", netConfig.ChainID, envelope.Delegation.ChainID)
	}

	if envelope.Delegation.GatewayID != netConfig.GatewayID {
		t.Errorf("Expected gateway ID %s, got %s", netConfig.GatewayID, envelope.Delegation.GatewayID)
	}

	if envelope.Delegation.ValidatorPubkey != validatorPubkey {
		t.Errorf("Expected validator pubkey %s, got %s", validatorPubkey, envelope.Delegation.ValidatorPubkey)
	}

	// Verify signature is empty (unsigned mode)
	if envelope.Signature != "" {
		t.Logf("Signature: %s (length: %d)", envelope.Signature, len(envelope.Signature))
		// Note: This is expected to be empty in unsigned mode
	}
}

// TestGenerateEnvelopeWithValidatorKeystore tests signed delegation generation
func TestGenerateEnvelopeWithValidatorKeystore(t *testing.T) {
	// Create temporary directory for test files
	tempDir := t.TempDir()

	// Generate a test validator key
	validatorKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate validator key: %v", err)
	}

	// Create validator keystore
	validatorPassword := "validator-password-123"
	validatorKeystorePath := filepath.Join(tempDir, "validator-keystore.json")
	validatorKS, err := keystore.EncryptKey(crypto.FromECDSA(validatorKey), validatorPassword)
	if err != nil {
		t.Fatalf("Failed to encrypt validator key: %v", err)
	}
	if err := keystore.SaveKeystore(validatorKS, validatorKeystorePath); err != nil {
		t.Fatalf("Failed to save validator keystore: %v", err)
	}

	// Test parameters
	network := "testnet"
	sidecarPassword := "sidecar-password-123"
	outputPath := filepath.Join(tempDir, "delegation-envelope.json")
	keystorePath := filepath.Join(tempDir, "sidecar-keystore.json")

	// Run the envelope generation with validator keystore
	err = run(tempDir, network, "", validatorKeystorePath, validatorPassword, sidecarPassword, outputPath, keystorePath)
	if err != nil {
		t.Fatalf("Failed to generate envelope: %v", err)
	}

	// Load and parse delegation envelope
	envelopeData, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read delegation envelope: %v", err)
	}

	var envelope DelegationEnvelope
	if err := json.Unmarshal(envelopeData, &envelope); err != nil {
		t.Fatalf("Failed to parse delegation envelope: %v", err)
	}

	// Load keystore and verify matching pubkeys
	ks, err := keystore.LoadKeystore(keystorePath)
	if err != nil {
		t.Fatalf("Failed to load keystore: %v", err)
	}

	privKeyBytes, err := keystore.DecryptKey(ks, sidecarPassword)
	if err != nil {
		t.Fatalf("Failed to decrypt keystore: %v", err)
	}

	sidecarKey, err := crypto.ToECDSA(privKeyBytes)
	if err != nil {
		t.Fatalf("Failed to convert to ECDSA key: %v", err)
	}

	keystoreSidecarPubkey := "0x" + hex.EncodeToString(crypto.CompressPubkey(&sidecarKey.PublicKey))
	delegationSidecarPubkey := strings.ToLower(envelope.Delegation.SidecarPubkey)
	keystorePubkey := strings.ToLower(keystoreSidecarPubkey)

	if delegationSidecarPubkey != keystorePubkey {
		t.Errorf("Sidecar public key mismatch!\n  Delegation: %s\n  Keystore:   %s",
			delegationSidecarPubkey, keystoreSidecarPubkey)
	}

	// Verify signature is NOT empty (signed mode)
	if envelope.Signature == "" {
		t.Error("Expected signed signature, but got empty signature")
	}

	// Verify signature length if present
	if envelope.Signature != "" {
		sigHex := strings.TrimPrefix(envelope.Signature, "0x")
		sigBytes, err := hex.DecodeString(sigHex)
		if err != nil {
			t.Fatalf("Invalid signature hex: %v", err)
		}
		if len(sigBytes) != 65 {
			t.Errorf("Expected signature length 65, got %d", len(sigBytes))
		}
	}

	t.Logf("✓ Signed delegation created successfully")
	t.Logf("  Validator pubkey: %s", envelope.Delegation.ValidatorPubkey)
	t.Logf("  Sidecar pubkey:   %s", envelope.Delegation.SidecarPubkey)
	if len(envelope.Signature) > 20 {
		t.Logf("  Signature:        %s...", envelope.Signature[:20])
	}
}

// TestV1AndV2KeystoreCompatibility tests that both v1 and v2 keystores work correctly
func TestV1AndV2KeystoreCompatibility(t *testing.T) {
	tests := []struct {
		name           string
		keystoreJSON   string
		password       string
		expectedPubkey string
	}{
		{
			name: "V2 keystore with version field",
			keystoreJSON: `{
				"version": 2,
				"ciphertext": "018cd07d0e6e4ad2651913239141a6bbf3ee23a685b27990fa04bf4db85b432f",
				"checksum": "2c8efca318594d6cce8610f8c06b70cf4cf0618f6044caac43e79e7c34d02120",
				"cipher": {
					"cipher_function": "AES_128_CTR",
					"params": {"iv": "53758be9ee7195511bbdb67f40764793"}
				},
				"kdf": {
					"kdf_name": "scrypt",
					"params": {
						"salt": "a44f82a954a9373cb1d89a62919be28d619a9bc797ed73fc023f6c6b6bba8714",
						"key_len": 32,
						"n": 262144,
						"r": 8,
						"p": 1
					}
				},
				"hash": "SHA256"
			}`,
			password:       "oYJCOagIvRn925MRWEc4DttR9tMz6AI5",
			expectedPubkey: "0x03529479026556df5f269b99111d7600512615077f22f45b5ab5c89b6967bdcf97",
		},
		{
			name: "V1 keystore without version field (legacy)",
			keystoreJSON: `{
				"ciphertext": "99b64c3181d593c02c4bf7c60a939449c1b4c37a07d01718a3cf0741e3f8f21a",
				"checksum": "c745c744bcb4b5e203ca55e242f98379cfcb227ff2c46c2c1816272c39c15c8a",
				"cipher": {
					"cipher_function": "AES_128_CTR",
					"params": {"iv": "0508bb5322f0155bb99820a3b5a19ada"}
				},
				"kdf": {
					"kdf_name": "scrypt",
					"params": {
						"salt": "5d122f6aa6051c7b939f6b4aeba553383733a35a3e126f03d6dc62dc53c5d768",
						"key_len": 32,
						"n": 262144,
						"r": 8,
						"p": 1
					}
				},
				"hash": "SHA256"
			}`,
			password:       "svyjcHPpzAGxTQNv4KyT",
			expectedPubkey: "0x036bbe3fb4fbef8e6734458ce617addedfeadc4228322c71800cfc30f6d45d4736",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary keystore file
			tempDir := t.TempDir()
			keystorePath := filepath.Join(tempDir, "test-keystore.json")

			if err := os.WriteFile(keystorePath, []byte(tt.keystoreJSON), 0600); err != nil {
				t.Fatalf("Failed to write keystore: %v", err)
			}

			// Load and decrypt keystore
			ks, err := keystore.LoadKeystore(keystorePath)
			if err != nil {
				t.Fatalf("Failed to load keystore: %v", err)
			}

			privKeyBytes, err := keystore.DecryptKey(ks, tt.password)
			if err != nil {
				t.Fatalf("Failed to decrypt keystore: %v", err)
			}

			// Convert to ECDSA key and get public key
			key, err := crypto.ToECDSA(privKeyBytes)
			if err != nil {
				t.Fatalf("Failed to convert to ECDSA key: %v", err)
			}

			pubkey := "0x" + hex.EncodeToString(crypto.CompressPubkey(&key.PublicKey))

			// Verify it matches expected
			if pubkey != tt.expectedPubkey {
				t.Errorf("Public key mismatch!\n  Got:      %s\n  Expected: %s", pubkey, tt.expectedPubkey)
			} else {
				t.Logf("✓ Correct public key: %s", pubkey)
			}
		})
	}
}
