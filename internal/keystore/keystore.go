package keystore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/scrypt"
)

// Keystore represents an encrypted Ethereum-compatible keystore
type Keystore struct {
	Version    int          `json:"version,omitempty"` // Version 1: hex string encrypted, Version 2: raw bytes encrypted
	Ciphertext string       `json:"ciphertext"`
	Checksum   string       `json:"checksum"`
	Cipher     CipherParams `json:"cipher"`
	KDF        KDFParams    `json:"kdf"`
	Hash       string       `json:"hash"`
}

type CipherParams struct {
	CipherFunction string       `json:"cipher_function"`
	Params         CipherConfig `json:"params"`
}

type CipherConfig struct {
	IV string `json:"iv"`
}

type KDFParams struct {
	KDFName string    `json:"kdf_name"`
	Params  KDFConfig `json:"params"`
}

type KDFConfig struct {
	KeyLen int    `json:"key_len"`
	N      int    `json:"n"`
	P      int    `json:"p"`
	R      int    `json:"r"`
	Salt   string `json:"salt"`
}

// EncryptKey encrypts a private key with the given password (Version 2 format: raw bytes)
func EncryptKey(privateKey []byte, password string) (*Keystore, error) {
	// Generate salt and IV
	salt := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	iv := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, fmt.Errorf("failed to generate IV: %w", err)
	}

	// Derive key using scrypt
	derivedKey, err := scrypt.Key([]byte(password), salt, 262144, 8, 1, 32)
	if err != nil {
		return nil, fmt.Errorf("scrypt key derivation failed: %w", err)
	}

	// Encrypt raw private key bytes using AES-128-CTR
	block, err := aes.NewCipher(derivedKey[:16])
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	stream := cipher.NewCTR(block, iv)
	ciphertext := make([]byte, len(privateKey))
	stream.XORKeyStream(ciphertext, privateKey)

	// Create MAC/checksum
	h := sha256.New()
	h.Write(derivedKey[16:32])
	h.Write(ciphertext)
	checksum := h.Sum(nil)

	return &Keystore{
		Version:    2, // Version 2: raw bytes encrypted
		Ciphertext: hex.EncodeToString(ciphertext),
		Checksum:   hex.EncodeToString(checksum),
		Cipher: CipherParams{
			CipherFunction: "AES_128_CTR",
			Params: CipherConfig{
				IV: hex.EncodeToString(iv),
			},
		},
		KDF: KDFParams{
			KDFName: "scrypt",
			Params: KDFConfig{
				KeyLen: 32,
				N:      262144,
				P:      1,
				R:      8,
				Salt:   hex.EncodeToString(salt),
			},
		},
		Hash: "SHA256",
	}, nil
}

// DecryptKey decrypts a keystore with the given password
func DecryptKey(ks *Keystore, password string) ([]byte, error) {
	// Decode parameters
	salt, err := hex.DecodeString(ks.KDF.Params.Salt)
	if err != nil {
		return nil, fmt.Errorf("invalid salt: %w", err)
	}

	iv, err := hex.DecodeString(ks.Cipher.Params.IV)
	if err != nil {
		return nil, fmt.Errorf("invalid IV: %w", err)
	}

	ciphertext, err := hex.DecodeString(ks.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("invalid ciphertext: %w", err)
	}

	checksum, err := hex.DecodeString(ks.Checksum)
	if err != nil {
		return nil, fmt.Errorf("invalid checksum: %w", err)
	}

	// Derive key using scrypt
	derivedKey, err := scrypt.Key([]byte(password), salt, ks.KDF.Params.N, ks.KDF.Params.R, ks.KDF.Params.P, ks.KDF.Params.KeyLen)
	if err != nil {
		return nil, fmt.Errorf("scrypt key derivation failed: %w", err)
	}

	// Verify checksum
	h := sha256.New()
	h.Write(derivedKey[16:32])
	h.Write(ciphertext)
	expectedChecksum := h.Sum(nil)

	if hex.EncodeToString(expectedChecksum) != hex.EncodeToString(checksum) {
		return nil, fmt.Errorf("invalid password: checksum mismatch")
	}

	// Decrypt using AES-128-CTR
	block, err := aes.NewCipher(derivedKey[:16])
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	stream := cipher.NewCTR(block, iv)
	decrypted := make([]byte, len(ciphertext))
	stream.XORKeyStream(decrypted, ciphertext)

	// Handle different keystore versions
	var privateKey []byte
	if ks.Version == 2 || ks.Version == 0 {
		// Version 2 (or no version): raw bytes encrypted
		privateKey = decrypted
	} else {
		// Version 1 (Python script format): hex string encrypted
		privateKeyHexStr := string(decrypted)
		privateKey, err = hex.DecodeString(privateKeyHexStr)
		if err != nil {
			return nil, fmt.Errorf("invalid private key format (version %d, expected hex string): %w", ks.Version, err)
		}
	}

	// Validate it's 32 bytes
	if len(privateKey) != 32 {
		return nil, fmt.Errorf("invalid private key length: expected 32 bytes, got %d", len(privateKey))
	}

	return privateKey, nil
}

// LoadKeystore loads a keystore from a file
func LoadKeystore(path string) (*Keystore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read keystore: %w", err)
	}

	var ks Keystore
	if err := json.Unmarshal(data, &ks); err != nil {
		return nil, fmt.Errorf("failed to parse keystore: %w", err)
	}

	return &ks, nil
}

// SaveKeystore saves a keystore to a file
func SaveKeystore(ks *Keystore, path string) error {
	data, err := json.MarshalIndent(ks, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal keystore: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write keystore: %w", err)
	}

	return nil
}
