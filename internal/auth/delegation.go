package auth

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"os"

	"github.com/FastLane-Labs/fastlane-sidecar/internal/keystore"
	"github.com/ethereum/go-ethereum/crypto"
)

// LoadDelegationEnvelope reads and parses a delegation envelope from a file
func LoadDelegationEnvelope(path string) (*DelegationEnvelope, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read delegation file: %w", err)
	}

	var envelope DelegationEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse delegation envelope: %w", err)
	}

	// Validate required fields
	if envelope.Delegation.Version == "" {
		return nil, fmt.Errorf("delegation version is required")
	}
	if envelope.Delegation.ChainID == "" {
		return nil, fmt.Errorf("delegation chain_id is required")
	}
	if envelope.Delegation.ValidatorPubkey == "" {
		return nil, fmt.Errorf("delegation validator_pubkey is required")
	}
	if envelope.Delegation.SidecarPubkey == "" {
		return nil, fmt.Errorf("delegation sidecar_pubkey is required")
	}

	return &envelope, nil
}

// LoadSidecarKey loads and decrypts a sidecar keystore file
func LoadSidecarKey(keystorePath, password string) (*ecdsa.PrivateKey, error) {
	// Load keystore
	ks, err := keystore.LoadKeystore(keystorePath)
	if err != nil {
		return nil, fmt.Errorf("failed to load keystore: %w", err)
	}

	// Decrypt key
	privKeyBytes, err := keystore.DecryptKey(ks, password)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt keystore: %w", err)
	}

	// Convert to ECDSA private key
	privateKey, err := crypto.ToECDSA(privKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to ECDSA key: %w", err)
	}

	return privateKey, nil
}

// GetSidecarPubkeyHex returns the compressed public key as 0x-prefixed hex
func GetSidecarPubkeyHex(key *ecdsa.PrivateKey) string {
	pubKey := crypto.CompressPubkey(&key.PublicKey)
	return "0x" + fmt.Sprintf("%x", pubKey)
}
