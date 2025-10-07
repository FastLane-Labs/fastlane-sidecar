package auth

import (
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/internal/keystore"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gowebpki/jcs"
	"lukechampine.com/blake3"
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

	// Verify signature if provided (not empty)
	if envelope.Signature != "" {
		if err := verifyDelegationSignature(&envelope); err != nil {
			return nil, fmt.Errorf("delegation signature verification failed: %w", err)
		}
	}

	// Check not_before timestamp
	if envelope.Delegation.NotBefore != "" {
		notBefore, err := time.Parse(time.RFC3339, envelope.Delegation.NotBefore)
		if err != nil {
			return nil, fmt.Errorf("invalid not_before timestamp: %w", err)
		}
		if time.Now().Before(notBefore) {
			return nil, fmt.Errorf("delegation not yet valid (not_before: %s)", envelope.Delegation.NotBefore)
		}
	}

	return &envelope, nil
}

// verifyDelegationSignature verifies that the validator signed the delegation
func verifyDelegationSignature(envelope *DelegationEnvelope) error {
	// Parse validator public key
	validatorPubkeyHex := strings.TrimPrefix(envelope.Delegation.ValidatorPubkey, "0x")
	validatorPubkeyBytes, err := hex.DecodeString(validatorPubkeyHex)
	if err != nil {
		return fmt.Errorf("invalid validator pubkey hex: %w", err)
	}
	if len(validatorPubkeyBytes) != 33 {
		return fmt.Errorf("validator pubkey must be 33 bytes (compressed), got %d bytes", len(validatorPubkeyBytes))
	}

	validatorPubkey, err := crypto.DecompressPubkey(validatorPubkeyBytes)
	if err != nil {
		return fmt.Errorf("failed to decompress validator pubkey: %w", err)
	}

	// Compute delegation hash using JCS canonicalization + BLAKE3
	delegationJSON, err := json.Marshal(envelope.Delegation)
	if err != nil {
		return fmt.Errorf("failed to marshal delegation: %w", err)
	}

	canonical, err := jcs.Transform(delegationJSON)
	if err != nil {
		return fmt.Errorf("failed to canonicalize delegation: %w", err)
	}

	// Compute hash: BLAKE3("fastlane/delegation/v1" || JCS(delegation))
	hasher := blake3.New(32, nil)
	if _, err := hasher.Write([]byte("fastlane/delegation/v1")); err != nil {
		return fmt.Errorf("failed to hash domain: %w", err)
	}
	if _, err := hasher.Write(canonical); err != nil {
		return fmt.Errorf("failed to hash canonical: %w", err)
	}
	delegationHash := hasher.Sum(nil)

	// Parse signature
	signatureHex := strings.TrimPrefix(envelope.Signature, "0x")
	signatureBytes, err := hex.DecodeString(signatureHex)
	if err != nil {
		return fmt.Errorf("invalid signature hex: %w", err)
	}
	if len(signatureBytes) != 65 {
		return fmt.Errorf("signature must be 65 bytes, got %d bytes", len(signatureBytes))
	}

	// Verify signature
	sigPublicKey, err := crypto.SigToPub(delegationHash, signatureBytes)
	if err != nil {
		return fmt.Errorf("failed to recover public key from signature: %w", err)
	}

	// Compare recovered public key with validator public key
	recoveredAddr := crypto.PubkeyToAddress(*sigPublicKey)
	validatorAddr := crypto.PubkeyToAddress(*validatorPubkey)

	if recoveredAddr != validatorAddr {
		return fmt.Errorf("signature verification failed: recovered address %s does not match validator address %s",
			recoveredAddr.Hex(), validatorAddr.Hex())
	}

	return nil
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

// ValidateSidecarPubkeyMatch verifies that the sidecar public key matches the delegation
func ValidateSidecarPubkeyMatch(envelope *DelegationEnvelope, sidecarKey *ecdsa.PrivateKey) error {
	// Get compressed public key from sidecar key
	sidecarPubkeyBytes := crypto.CompressPubkey(&sidecarKey.PublicKey)
	sidecarPubkeyHex := "0x" + hex.EncodeToString(sidecarPubkeyBytes)

	// Normalize both for comparison (lowercase, with 0x prefix)
	delegationPubkey := strings.ToLower(envelope.Delegation.SidecarPubkey)
	if !strings.HasPrefix(delegationPubkey, "0x") {
		delegationPubkey = "0x" + delegationPubkey
	}
	sidecarPubkeyHex = strings.ToLower(sidecarPubkeyHex)

	if delegationPubkey != sidecarPubkeyHex {
		return fmt.Errorf("sidecar public key mismatch: delegation has %s, keystore has %s",
			delegationPubkey, sidecarPubkeyHex)
	}

	return nil
}

// GetSidecarPubkeyHex returns the compressed public key as 0x-prefixed hex
func GetSidecarPubkeyHex(key *ecdsa.PrivateKey) string {
	pubKey := crypto.CompressPubkey(&key.PublicKey)
	return "0x" + fmt.Sprintf("%x", pubKey)
}
