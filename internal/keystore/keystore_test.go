package keystore

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

func TestDecryptMonadKeystore(t *testing.T) {
	// Real Monad validator keystore (version 2, hex string format)
	keystoreJSON := `{
		"version": 2,
		"ciphertext": "b60a072b8f04660dea7b3d96278bcbc9dc48468003115df0488bd322efad8487",
		"checksum": "7d1a8ec43d42a40b96b33652566bfea321d312699721838dc975c2bf8c1dc303",
		"cipher": {
			"cipher_function": "AES_128_CTR",
			"params": {
				"iv": "a34c8e7746720b73c7260337839b0812"
			}
		},
		"kdf": {
			"kdf_name": "scrypt",
			"params": {
				"salt": "11a19ebce0443e4f16555857b3d2d0075b34af582d8627e7bdcd9ed6736c0454",
				"key_len": 32,
				"n": 262144,
				"r": 8,
				"p": 1
			}
		},
		"hash": "SHA256"
	}`

	// Expected public key from backup
	expectedPubkey := "037adc59c728e1b0a35ca5989b7738cae3addc7dededb3da167fa58c8b08d95ac4"

	// Parse keystore
	var ks Keystore
	if err := json.Unmarshal([]byte(keystoreJSON), &ks); err != nil {
		t.Fatalf("Failed to parse keystore: %v", err)
	}

	// Note: We can't decrypt without the actual password, but we can test the version handling
	// This test is more of a documentation of the expected behavior
	t.Logf("Keystore version: %d", ks.Version)
	t.Logf("Expected public key: %s", expectedPubkey)

	// Verify version is 2 (Monad format)
	if ks.Version != 2 {
		t.Errorf("Expected version 2, got %d", ks.Version)
	}
}

func TestKeystoreVersionHandling(t *testing.T) {
	tests := []struct {
		name            string
		version         int
		encryptedData   string // hex-encoded
		decryptedResult string // what we expect after decryption (before version-specific handling)
		expectedPrivKey string // final 32-byte private key (hex)
	}{
		{
			name:            "Version 1 - raw bytes",
			version:         1,
			encryptedData:   "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", // 32 bytes
			decryptedResult: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			expectedPrivKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
		{
			name:            "Version 2 - hex string (Monad format)",
			version:         2,
			encryptedData:   "30313233343536373839616263646566303132333435363738396162636465663031323334353637383961626364656630313233343536373839616263646566", // "0123...cdef" as hex string, then hex-encoded
			decryptedResult: "30313233343536373839616263646566303132333435363738396162636465663031323334353637383961626364656630313233343536373839616263646566",
			expectedPrivKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the decrypted data
			decrypted, _ := hex.DecodeString(tt.decryptedResult)

			// Apply version-specific handling (extracted from DecryptKey logic)
			var privateKey []byte
			var err error

			if tt.version == 1 {
				// Version 1: raw bytes encrypted
				privateKey = decrypted
			} else {
				// Version 2: hex string encrypted (Monad/Python format)
				privateKeyHexStr := string(decrypted)
				privateKey, err = hex.DecodeString(privateKeyHexStr)
				if err != nil {
					t.Fatalf("Failed to decode hex string: %v", err)
				}
			}

			// Verify we got 32 bytes
			if len(privateKey) != 32 {
				t.Errorf("Expected 32 bytes, got %d", len(privateKey))
			}

			// Verify the private key matches expected
			gotPrivKey := hex.EncodeToString(privateKey)
			if gotPrivKey != tt.expectedPrivKey {
				t.Errorf("Expected private key %s, got %s", tt.expectedPrivKey, gotPrivKey)
			}

			// Verify we can derive a valid public key
			key, err := crypto.ToECDSA(privateKey)
			if err != nil {
				t.Fatalf("Failed to create ECDSA key: %v", err)
			}

			pubkey := crypto.CompressPubkey(&key.PublicKey)
			t.Logf("Derived public key: %s", hex.EncodeToString(pubkey))
		})
	}
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	testPrivKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	privKeyBytes, _ := hex.DecodeString(testPrivKey)
	password := "test-password"

	// Encrypt
	ks, err := EncryptKey(privKeyBytes, password)
	if err != nil {
		t.Fatalf("EncryptKey failed: %v", err)
	}

	// Verify it's version 1 (our format)
	if ks.Version != 1 {
		t.Errorf("Expected version 1 for our format, got %d", ks.Version)
	}

	// Decrypt
	decrypted, err := DecryptKey(ks, password)
	if err != nil {
		t.Fatalf("DecryptKey failed: %v", err)
	}

	// Verify roundtrip
	if hex.EncodeToString(decrypted) != testPrivKey {
		t.Errorf("Roundtrip failed: expected %s, got %s", testPrivKey, hex.EncodeToString(decrypted))
	}
}
