package auth

import (
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gowebpki/jcs"
	"lukechampine.com/blake3"
)

func TestLoadDelegationEnvelope(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a valid delegation envelope for testing
	validatorKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate validator key: %v", err)
	}

	sidecarKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate sidecar key: %v", err)
	}

	validatorPubkey := "0x" + hex.EncodeToString(crypto.CompressPubkey(&validatorKey.PublicKey))
	sidecarPubkey := "0x" + hex.EncodeToString(crypto.CompressPubkey(&sidecarKey.PublicKey))

	delegation := Delegation{
		Version:         "v1",
		ChainID:         "monad-testnet",
		GatewayID:       "gw-test-1",
		ValidatorPubkey: validatorPubkey,
		SidecarPubkey:   sidecarPubkey,
		Scopes:          []string{"tx_publish", "auth_refresh_inband"},
		NotBefore:       time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		Comment:         "test delegation",
	}

	// Sign the delegation
	delegationJSON, _ := json.Marshal(delegation)
	canonical, _ := jcs.Transform(delegationJSON)
	hasher := blake3.New(32, nil)
	hasher.Write([]byte("fastlane/delegation/v1"))
	hasher.Write(canonical)
	delegationHash := hasher.Sum(nil)
	signature, _ := crypto.Sign(delegationHash, validatorKey)

	envelope := DelegationEnvelope{
		Delegation: delegation,
		Signature:  "0x" + hex.EncodeToString(signature),
	}

	tests := []struct {
		name     string
		envelope *DelegationEnvelope
		modify   func(*DelegationEnvelope)
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "valid delegation with signature",
			envelope: &envelope,
			wantErr:  false,
		},
		{
			name:     "valid delegation without signature",
			envelope: &envelope,
			modify: func(e *DelegationEnvelope) {
				e.Signature = ""
			},
			wantErr: false,
		},
		{
			name:     "missing version",
			envelope: &envelope,
			modify: func(e *DelegationEnvelope) {
				e.Delegation.Version = ""
			},
			wantErr: true,
			errMsg:  "version is required",
		},
		{
			name:     "missing chain_id",
			envelope: &envelope,
			modify: func(e *DelegationEnvelope) {
				e.Delegation.ChainID = ""
			},
			wantErr: true,
			errMsg:  "chain_id is required",
		},
		{
			name:     "missing validator_pubkey",
			envelope: &envelope,
			modify: func(e *DelegationEnvelope) {
				e.Delegation.ValidatorPubkey = ""
			},
			wantErr: true,
			errMsg:  "validator_pubkey is required",
		},
		{
			name:     "missing sidecar_pubkey",
			envelope: &envelope,
			modify: func(e *DelegationEnvelope) {
				e.Delegation.SidecarPubkey = ""
			},
			wantErr: true,
			errMsg:  "sidecar_pubkey is required",
		},
		{
			name:     "unsigned delegation (all zeros)",
			envelope: &envelope,
			modify: func(e *DelegationEnvelope) {
				e.Signature = "0x" + strings.Repeat("00", 65) // 65 bytes all-zero signature is treated as unsigned
			},
			wantErr: false, // all-zero signatures are now valid (treated as unsigned)
		},
		{
			name:     "not_before in future",
			envelope: &envelope,
			modify: func(e *DelegationEnvelope) {
				e.Delegation.NotBefore = time.Now().Add(1 * time.Hour).Format(time.RFC3339)
				// Need to re-sign with new delegation
				delegationJSON, _ := json.Marshal(e.Delegation)
				canonical, _ := jcs.Transform(delegationJSON)
				hasher := blake3.New(32, nil)
				hasher.Write([]byte("fastlane/delegation/v1"))
				hasher.Write(canonical)
				hash := hasher.Sum(nil)
				sig, _ := crypto.Sign(hash, validatorKey)
				e.Signature = "0x" + hex.EncodeToString(sig)
			},
			wantErr: true,
			errMsg:  "not yet valid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copy of the envelope
			envCopy := *tt.envelope

			// Apply modification if provided
			if tt.modify != nil {
				tt.modify(&envCopy)
			}

			// Write to temp file
			filePath := filepath.Join(tmpDir, tt.name+".json")
			data, _ := json.MarshalIndent(envCopy, "", "  ")
			os.WriteFile(filePath, data, 0644)

			// Test loading
			_, err := LoadDelegationEnvelope(filePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadDelegationEnvelope() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errMsg, err)
				}
			}
		})
	}
}

func TestVerifyDelegationSignature(t *testing.T) {
	validatorKey, _ := crypto.GenerateKey()
	wrongKey, _ := crypto.GenerateKey()

	validatorPubkey := "0x" + hex.EncodeToString(crypto.CompressPubkey(&validatorKey.PublicKey))
	sidecarPubkey := "0x" + hex.EncodeToString(crypto.CompressPubkey(&wrongKey.PublicKey))

	delegation := Delegation{
		Version:         "v1",
		ChainID:         "test",
		GatewayID:       "gw-1",
		ValidatorPubkey: validatorPubkey,
		SidecarPubkey:   sidecarPubkey,
		Scopes:          []string{"tx_publish"},
		NotBefore:       time.Now().Format(time.RFC3339),
	}

	// Sign with correct key
	delegationJSON, _ := json.Marshal(delegation)
	canonical, _ := jcs.Transform(delegationJSON)
	hasher := blake3.New(32, nil)
	hasher.Write([]byte("fastlane/delegation/v1"))
	hasher.Write(canonical)
	hash := hasher.Sum(nil)
	correctSig, _ := crypto.Sign(hash, validatorKey)

	// Sign with wrong key
	wrongSig, _ := crypto.Sign(hash, wrongKey)

	tests := []struct {
		name      string
		envelope  *DelegationEnvelope
		wantErr   bool
		errSubstr string
	}{
		{
			name: "valid signature",
			envelope: &DelegationEnvelope{
				Delegation: delegation,
				Signature:  "0x" + hex.EncodeToString(correctSig),
			},
			wantErr: false,
		},
		{
			name: "signature from wrong key",
			envelope: &DelegationEnvelope{
				Delegation: delegation,
				Signature:  "0x" + hex.EncodeToString(wrongSig),
			},
			wantErr:   true,
			errSubstr: "does not match validator address",
		},
		{
			name: "invalid signature length",
			envelope: &DelegationEnvelope{
				Delegation: delegation,
				Signature:  "0xabcd",
			},
			wantErr:   true,
			errSubstr: "must be 65 bytes",
		},
		{
			name: "invalid hex",
			envelope: &DelegationEnvelope{
				Delegation: delegation,
				Signature:  "0xZZZZ",
			},
			wantErr:   true,
			errSubstr: "invalid signature hex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyDelegationSignature(tt.envelope)
			if (err != nil) != tt.wantErr {
				t.Errorf("verifyDelegationSignature() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errSubstr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errSubstr, err)
				}
			}
		})
	}
}

func TestValidateSidecarPubkeyMatch(t *testing.T) {
	sidecarKey, _ := crypto.GenerateKey()
	wrongKey, _ := crypto.GenerateKey()

	sidecarPubkey := "0x" + hex.EncodeToString(crypto.CompressPubkey(&sidecarKey.PublicKey))
	wrongPubkey := "0x" + hex.EncodeToString(crypto.CompressPubkey(&wrongKey.PublicKey))

	tests := []struct {
		name     string
		envelope *DelegationEnvelope
		key      *ecdsa.PrivateKey
		wantErr  bool
	}{
		{
			name: "matching pubkey",
			envelope: &DelegationEnvelope{
				Delegation: Delegation{
					SidecarPubkey: sidecarPubkey,
				},
			},
			key:     sidecarKey,
			wantErr: false,
		},
		{
			name: "matching pubkey without 0x prefix",
			envelope: &DelegationEnvelope{
				Delegation: Delegation{
					SidecarPubkey: strings.TrimPrefix(sidecarPubkey, "0x"),
				},
			},
			key:     sidecarKey,
			wantErr: false,
		},
		{
			name: "mismatched pubkey",
			envelope: &DelegationEnvelope{
				Delegation: Delegation{
					SidecarPubkey: wrongPubkey,
				},
			},
			key:     sidecarKey,
			wantErr: true,
		},
		{
			name: "uppercase vs lowercase",
			envelope: &DelegationEnvelope{
				Delegation: Delegation{
					SidecarPubkey: strings.ToUpper(sidecarPubkey),
				},
			},
			key:     sidecarKey,
			wantErr: false, // Should be case-insensitive
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSidecarPubkeyMatch(tt.envelope, tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSidecarPubkeyMatch() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetSidecarPubkeyHex(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	pubkeyHex := GetSidecarPubkeyHex(key)

	// Verify format
	if !strings.HasPrefix(pubkeyHex, "0x") {
		t.Errorf("Public key should have 0x prefix, got: %s", pubkeyHex)
	}

	// Verify it's valid hex
	pubkeyBytes, err := hex.DecodeString(strings.TrimPrefix(pubkeyHex, "0x"))
	if err != nil {
		t.Errorf("Public key should be valid hex: %v", err)
	}

	// Verify it's 33 bytes (compressed)
	if len(pubkeyBytes) != 33 {
		t.Errorf("Compressed public key should be 33 bytes, got %d", len(pubkeyBytes))
	}

	// Verify we can decompress it
	_, err = crypto.DecompressPubkey(pubkeyBytes)
	if err != nil {
		t.Errorf("Should be able to decompress public key: %v", err)
	}
}
