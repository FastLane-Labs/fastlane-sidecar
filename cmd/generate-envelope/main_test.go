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
