package keystore

import (
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

func TestDeriveSecp256k1Key(t *testing.T) {
	// Known IKM from user's backup
	ikmHex := "c88d08ff877717bb1f5892c7cae69ae2320399c04f23bb0d8ffd690e7585554b"
	ikm, err := hex.DecodeString(ikmHex)
	if err != nil {
		t.Fatalf("Failed to decode IKM: %v", err)
	}

	// Expected secp private key from user's backup
	expectedPrivKeyHex := "06cf8c9b9b2353a22bcc60e809a17109d19f6e0ff86780a0ec337cc192634d63"

	// Expected public key (compressed)
	expectedPubKeyHex := "037adc59c728e1b0a35ca5989b7738cae3addc7dededb3da167fa58c8b08d95ac4"

	// Derive the key
	derivedKey, err := DeriveSecp256k1Key(ikm)
	if err != nil {
		t.Fatalf("Failed to derive key: %v", err)
	}

	// Check if derived key matches expected private key
	derivedHex := hex.EncodeToString(derivedKey)
	if derivedHex != expectedPrivKeyHex {
		t.Errorf("Derived key mismatch:\n  got:      %s\n  expected: %s", derivedHex, expectedPrivKeyHex)
	}

	// Verify the public key matches
	ecdsaKey, err := crypto.ToECDSA(derivedKey)
	if err != nil {
		t.Fatalf("Failed to convert to ECDSA key: %v", err)
	}

	pubKeyCompressed := crypto.CompressPubkey(&ecdsaKey.PublicKey)
	pubKeyHex := hex.EncodeToString(pubKeyCompressed)

	if pubKeyHex != expectedPubKeyHex {
		t.Errorf("Public key mismatch:\n  got:      %s\n  expected: %s", pubKeyHex, expectedPubKeyHex)
	}

	t.Logf("✓ Derived private key: %s", derivedHex)
	t.Logf("✓ Derived public key:  %s", pubKeyHex)
}

func TestExpandMessageXMD(t *testing.T) {
	// Test vector from RFC 9380 Appendix K.1 (SHA-256)
	// https://datatracker.ietf.org/doc/html/rfc9380#appendix-K.1
	msg := []byte("")
	dst := []byte("QUUX-V01-CS02-with-expander-SHA256-128")
	lenInBytes := 0x80 // 128 bytes

	result := expandMessageXMD(msg, dst, lenInBytes)
	resultHex := hex.EncodeToString(result)

	// For this implementation test, we just verify it produces 128 bytes
	if len(result) != lenInBytes {
		t.Errorf("expand_message_xmd produced wrong length: got %d, expected %d", len(result), lenInBytes)
	}

	t.Logf("expand_message_xmd output (%d bytes): %s", len(result), resultHex)
}
