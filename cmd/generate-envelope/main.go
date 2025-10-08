package main

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/internal/keystore"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gowebpki/jcs"
	"golang.org/x/term"
	"lukechampine.com/blake3"
)

const (
	defaultNetwork      = "testnet"
	defaultHomeDir      = "/home/monad/fastlane"
	defaultOutputFile   = "delegation-envelope.json"
	defaultKeystoreFile = "sidecar-keystore.json"
	delegationVersion   = "v1"
	unsignedSigLength   = 130
)

type Delegation struct {
	Version         string   `json:"version"`
	ChainID         string   `json:"chain_id"`
	GatewayID       string   `json:"gateway_id"`
	ValidatorPubkey string   `json:"validator_pubkey"`
	SidecarPubkey   string   `json:"sidecar_pubkey"`
	Scopes          []string `json:"scopes"`
	NotBefore       string   `json:"not_before"`
	Comment         string   `json:"comment"`
}

type DelegationEnvelope struct {
	Delegation Delegation `json:"delegation"`
	Signature  string     `json:"signature"`
}

type NetworkConfig struct {
	ChainID   string
	GatewayID string
}

var (
	defaultScopes = []string{"tx_publish", "auth_refresh_inband", "inclusions_report"}

	networks = map[string]NetworkConfig{
		"testnet":   {ChainID: "10143", GatewayID: "monad-testnet"},
		"testnet-2": {ChainID: "10143", GatewayID: "monad-testnet"}, // only for generate envelope
		"mainnet":   {ChainID: "143", GatewayID: "mainnet"},
	}
)

func main() {
	log.SetFlags(0) // Remove timestamp from log output

	homeDir := flag.String("home", defaultHomeDir, "FastLane home directory for output files")
	network := flag.String("network", defaultNetwork, "Network (testnet, mainnet)")
	validatorPubkey := flag.String("validator-pubkey", "", "Validator public key (compressed, 33 bytes, 0x-prefixed)")
	validatorKeystore := flag.String("validator-keystore", "", "Path to validator keystore file (for signing)")
	validatorPassword := flag.String("validator-password", "", "Password for validator keystore (will prompt if not provided)")
	sidecarPassword := flag.String("sidecar-password", "", "Password for sidecar keystore (required)")
	output := flag.String("output", "", "Output delegation envelope file (default: <home>/delegation-envelope.json)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Generate a delegation envelope for a Monad validator with auto-generated sidecar keystore.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Generate unsigned delegation for testnet\n")
		fmt.Fprintf(os.Stderr, "  %s --validator-pubkey 0x03abc...\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Generate signed delegation with keystore\n")
		fmt.Fprintf(os.Stderr, "  %s --validator-keystore validator.json\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Generate for mainnet with custom home directory\n")
		fmt.Fprintf(os.Stderr, "  %s --network mainnet --home /var/lib/fastlane --validator-keystore validator.json --sidecar-password mypass\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Output Files (default in %s):\n", defaultHomeDir)
		fmt.Fprintf(os.Stderr, "  - %s\n", defaultOutputFile)
		fmt.Fprintf(os.Stderr, "  - %s\n\n", defaultKeystoreFile)
	}

	flag.Parse()

	// Set default output if not specified
	outputPath := *output
	if outputPath == "" {
		outputPath = filepath.Join(*homeDir, defaultOutputFile)
	}

	keystorePath := filepath.Join(*homeDir, defaultKeystoreFile)

	if err := run(*homeDir, *network, *validatorPubkey, *validatorKeystore, *validatorPassword, *sidecarPassword, outputPath, keystorePath); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func run(homeDir, network, validatorPubkey, validatorKeystore, validatorPassword, sidecarPassword, output, keystorePath string) error {
	log.Println("Monad Delegation Setup Script v1.0.0")
	fmt.Println()

	// Create home directory if it doesn't exist
	if err := os.MkdirAll(homeDir, 0755); err != nil {
		return fmt.Errorf("failed to create home directory: %w", err)
	}

	// Validate inputs
	if validatorPubkey == "" && validatorKeystore == "" {
		return fmt.Errorf("either --validator-pubkey or --validator-keystore must be provided")
	}

	if validatorPubkey != "" && validatorKeystore != "" {
		return fmt.Errorf("cannot specify both --validator-pubkey and --validator-keystore")
	}

	// Get network configuration
	netConfig, ok := networks[network]
	if !ok {
		return fmt.Errorf("invalid network: %s (valid: testnet, mainnet)", network)
	}

	// Load validator key (signed mode) or use public key (unsigned mode)
	validatorKey, validatorPubStr, err := loadValidatorKey(validatorKeystore, validatorPubkey, validatorPassword)
	if err != nil {
		return fmt.Errorf("failed to load validator key: %w", err)
	}
	signed := validatorKey != nil
	log.Printf("✓ Validator public key: %s\n", validatorPubStr)

	// Validate sidecar password is provided
	if sidecarPassword == "" {
		return fmt.Errorf("--sidecar-password is required")
	}

	var sidecarPubStr string

	// Check if sidecar keystore already exists
	if _, err := os.Stat(keystorePath); err == nil {
		// Keystore exists - load it and derive the public key
		log.Printf("Loading existing sidecar keystore: %s\n", keystorePath)

		ks, err := keystore.LoadKeystore(keystorePath)
		if err != nil {
			return fmt.Errorf("failed to load existing keystore: %w", err)
		}

		sidecarPrivKeyBytes, err := keystore.DecryptKey(ks, sidecarPassword)
		if err != nil {
			return fmt.Errorf("failed to decrypt existing keystore (wrong password?): %w", err)
		}

		sidecarKey, err := crypto.ToECDSA(sidecarPrivKeyBytes)
		if err != nil {
			return fmt.Errorf("failed to convert existing key to ECDSA: %w", err)
		}

		sidecarPubStr = "0x" + hex.EncodeToString(crypto.CompressPubkey(&sidecarKey.PublicKey))
		log.Printf("✓ Sidecar public key (from existing keystore): %s\n", sidecarPubStr)
	} else {
		// Keystore doesn't exist - generate new IKM and create keystore
		log.Printf("Generating new sidecar keystore: %s\n", keystorePath)

		// Generate random IKM (Input Keying Material) for sidecar key
		ikm := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, ikm); err != nil {
			return fmt.Errorf("failed to generate IKM: %w", err)
		}

		// Derive the sidecar private key from IKM (same as DecryptKey does)
		sidecarPrivKeyBytes, err := keystore.DeriveSecp256k1Key(ikm)
		if err != nil {
			return fmt.Errorf("failed to derive sidecar key from IKM: %w", err)
		}

		sidecarKey, err := crypto.ToECDSA(sidecarPrivKeyBytes)
		if err != nil {
			return fmt.Errorf("failed to convert to ECDSA key: %w", err)
		}

		sidecarPubStr = "0x" + hex.EncodeToString(crypto.CompressPubkey(&sidecarKey.PublicKey))
		log.Printf("✓ Sidecar public key (newly generated): %s\n", sidecarPubStr)

		// Create encrypted sidecar keystore - encrypt the IKM, not the derived private key
		sidecarKS, err := keystore.EncryptKey(ikm, sidecarPassword)
		if err != nil {
			return fmt.Errorf("failed to encrypt sidecar key: %w", err)
		}

		if err := keystore.SaveKeystore(sidecarKS, keystorePath); err != nil {
			return fmt.Errorf("failed to save sidecar keystore: %w", err)
		}
		log.Printf("✓ Sidecar keystore saved: %s\n", keystorePath)
	}

	// Create delegation envelope
	delegation := Delegation{
		Version:         delegationVersion,
		ChainID:         netConfig.ChainID,
		GatewayID:       netConfig.GatewayID,
		ValidatorPubkey: validatorPubStr,
		SidecarPubkey:   sidecarPubStr,
		Scopes:          defaultScopes,
		NotBefore:       time.Now().UTC().Format(time.RFC3339),
		Comment:         "Generated by delegation script",
	}

	signature, err := signDelegation(delegation, validatorKey)
	if err != nil {
		return fmt.Errorf("failed to sign delegation: %w", err)
	}

	envelope := DelegationEnvelope{
		Delegation: delegation,
		Signature:  signature,
	}

	envelopeJSON, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal envelope: %w", err)
	}

	if err := os.WriteFile(output, envelopeJSON, 0600); err != nil {
		return fmt.Errorf("failed to save delegation envelope: %w", err)
	}
	log.Printf("✓ Delegation envelope: %s\n", output)

	// Print summary
	fmt.Println()
	log.Println("✓ Delegation setup complete!")
	fmt.Println()
	fmt.Printf("Network:   %s (chain_id: %s, gateway: %s)\n", network, netConfig.ChainID, netConfig.GatewayID)
	if signed {
		fmt.Println("Signed:    YES (with validator keystore)")
	} else {
		fmt.Println("Signed:    NO (requires gateway verification)")
	}
	fmt.Println()
	fmt.Println("Output files:")
	fmt.Printf("  - %s\n", output)
	fmt.Printf("  - %s\n", keystorePath)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Keep these files secure")
	fmt.Println("  2. Configure sidecar with environment variables:")
	fmt.Printf("       DELEGATION_PATH=%s\n", output)
	fmt.Printf("       SIDECAR_KEYSTORE_PATH=%s\n", keystorePath)
	fmt.Printf("       SIDECAR_PASSWORD=%s\n", sidecarPassword)

	if network == "mainnet" {
		fmt.Println()
		log.Println("WARNING: Generated MAINNET delegation - ensure keys are production-ready!")
	}

	if !signed {
		fmt.Println()
		log.Println("NOTE: Unsigned delegation requires gateway verification (whitelist/test mode)")
		fmt.Println()
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println("⚠️  MANUAL APPROVAL REQUIRED")
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println()
		fmt.Println("Please send the following information to FastLane for approval:")
		fmt.Println()
		fmt.Printf("Validator Public Key:\n%s\n\n", validatorPubStr)
		fmt.Printf("Sidecar Public Key:\n%s\n", sidecarPubStr)
		fmt.Println()
		fmt.Println("═══════════════════════════════════════════════════════════════")
	}

	return nil
}

// loadValidatorKey loads the validator private key from keystore (signed mode)
// or returns nil key with the provided public key string (unsigned mode)
func loadValidatorKey(keystorePath, pubkey, password string) (*ecdsa.PrivateKey, string, error) {
	if keystorePath != "" {
		// Signed mode: load and decrypt keystore
		ks, err := keystore.LoadKeystore(keystorePath)
		if err != nil {
			return nil, "", fmt.Errorf("load keystore: %w", err)
		}

		if password == "" {
			fmt.Print("Enter validator keystore password: ")
			passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Println()
			if err != nil {
				return nil, "", fmt.Errorf("read password: %w", err)
			}
			password = string(passwordBytes)
		}

		privateKeyBytes, err := keystore.DecryptKey(ks, password)
		if err != nil {
			return nil, "", fmt.Errorf("decrypt keystore: %w", err)
		}

		key, err := crypto.ToECDSA(privateKeyBytes)
		if err != nil {
			return nil, "", fmt.Errorf("invalid private key: %w", err)
		}

		pubStr := "0x" + hex.EncodeToString(crypto.CompressPubkey(&key.PublicKey))
		return key, pubStr, nil
	}

	// Unsigned mode: validate and use provided public key
	if err := validatePublicKey(pubkey); err != nil {
		return nil, "", fmt.Errorf("invalid validator public key: %w", err)
	}

	// Ensure 0x prefix
	if !hasHexPrefix(pubkey) {
		pubkey = "0x" + pubkey
	}

	return nil, pubkey, nil
}

// validatePublicKey validates that a public key is a valid compressed secp256k1 public key
func validatePublicKey(pubkey string) error {
	// Remove 0x prefix if present
	pubkeyHex := pubkey
	if hasHexPrefix(pubkey) {
		pubkeyHex = pubkey[2:]
	}

	// Must be valid hex
	pubkeyBytes, err := hex.DecodeString(pubkeyHex)
	if err != nil {
		return fmt.Errorf("not valid hex: %w", err)
	}

	// Must be 33 bytes (compressed secp256k1 public key)
	if len(pubkeyBytes) != 33 {
		return fmt.Errorf("invalid length: expected 33 bytes (compressed), got %d bytes", len(pubkeyBytes))
	}

	// First byte must be 0x02 or 0x03 (compressed format)
	if pubkeyBytes[0] != 0x02 && pubkeyBytes[0] != 0x03 {
		return fmt.Errorf("invalid format: first byte must be 0x02 or 0x03 (compressed format), got 0x%02x", pubkeyBytes[0])
	}

	// Validate it's a valid point on the curve by decompressing it
	_, err = crypto.DecompressPubkey(pubkeyBytes)
	if err != nil {
		return fmt.Errorf("not a valid secp256k1 public key: %w", err)
	}

	return nil
}

// hasHexPrefix checks if a string has the 0x prefix
func hasHexPrefix(str string) bool {
	return len(str) >= 2 && str[0] == '0' && (str[1] == 'x' || str[1] == 'X')
}

// signDelegation signs the delegation if a private key is provided, otherwise returns empty signature
func signDelegation(delegation Delegation, key *ecdsa.PrivateKey) (string, error) {
	if key == nil {
		// Unsigned delegation (empty for whitelisted validators)
		return "", nil
	}

	// Marshal delegation to JSON
	delegationJSON, err := json.Marshal(delegation)
	if err != nil {
		return "", fmt.Errorf("marshal delegation: %w", err)
	}

	// Canonicalize using JCS (RFC 8785)
	canonical, err := jcs.Transform(delegationJSON)
	if err != nil {
		return "", fmt.Errorf("canonicalize delegation: %w", err)
	}

	// Compute hash: BLAKE3("fastlane/delegation/v1" || JCS(delegation))
	hasher := blake3.New(32, nil)
	if _, err := hasher.Write([]byte("fastlane/delegation/v1")); err != nil {
		return "", fmt.Errorf("hash domain: %w", err)
	}
	if _, err := hasher.Write(canonical); err != nil {
		return "", fmt.Errorf("hash canonical: %w", err)
	}
	delegationHash := hasher.Sum(nil)

	// Sign the hash
	sig, err := crypto.Sign(delegationHash, key)
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}

	return "0x" + hex.EncodeToString(sig), nil
}
