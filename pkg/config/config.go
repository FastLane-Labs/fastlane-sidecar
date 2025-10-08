package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	NodeToSidecarSuffix = "node_to_sidecar"
	SidecarToNodeSuffix = "sidecar_to_node"
)

// FastlaneContractAddresses maps network names to their fastlane contract addresses
// TODO: Update these with actual deployed contract addresses
var FastlaneContractAddresses = map[string]string{
	"testnet":   "0x0000000000000000000000000000000000000000",
	"testnet-2": "0x0000000000000000000000000000000000000000",
	"mainnet":   "0x0000000000000000000000000000000000000000",
}

type Config struct {
	LogLevel                string
	HomePath                string
	NodeToSidecarSocketPath string // Derived from HomePath + ".node_to_sidecar"
	SidecarToNodeSocketPath string // Derived from HomePath + ".sidecar_to_node"
	GatewayURL              string
	PoolMaxDuration         time.Duration
	AuctionCycleTime        time.Duration
	StreamingDelay          time.Duration
	FastlaneContract        string // Hex address of the fastlane auction contract

	// Authentication parameters
	DelegationPath   string // Path to delegation envelope JSON file
	KeystorePath     string // Path to sidecar keystore file
	KeystorePass     string // Password for sidecar keystore (loaded from env var or file)
	PasswordFilePath string // Path to file containing keystore password

	// Network configuration
	Network string // Network name (e.g., "testnet", "testnet-2", "mainnet")

	// Gateway control
	DisableGateway bool // Disable gateway connection
}

func NewConfig() *Config {
	var conf Config
	var poolMaxDurationMs int
	var auctionCycleMs int
	var streamingDelayMs int
	var contractOverride string

	fs := flag.NewFlagSet("UserConfig", flag.ExitOnError)
	fs.StringVar(&conf.LogLevel, "log-level", "debug", "Log level")
	fs.StringVar(&conf.HomePath, "home", "/home/monad/fastlane/", "Fastlane home directory")
	fs.StringVar(&conf.GatewayURL, "gateway-url", "http://localhost:8080", "HTTP URL for MEV gateway (will be converted to WebSocket)")
	fs.IntVar(&poolMaxDurationMs, "pool-max-duration-ms", 60000, "Maximum time to hold transactions in pool (ms)")
	fs.IntVar(&auctionCycleMs, "auction-cycle-ms", 200, "Auction cycle interval (ms)")
	fs.IntVar(&streamingDelayMs, "streaming-delay-ms", 100, "Delay before streaming auction results (ms)")
	fs.StringVar(&conf.DelegationPath, "delegation", "delegation-envelope.json", "Delegation envelope JSON filename (relative to home)")
	fs.StringVar(&conf.KeystorePath, "keystore", "sidecar-keystore.json", "Sidecar keystore filename (relative to home)")
	fs.StringVar(&conf.PasswordFilePath, "password-file", "", "Path to file containing keystore password")
	fs.StringVar(&conf.Network, "network", "testnet", "Network name (testnet, testnet-2, mainnet)")
	fs.StringVar(&contractOverride, "fastlane-contract", "", "Override fastlane contract address (optional, uses network default if not set)")
	fs.BoolVar(&conf.DisableGateway, "disable-gateway", false, "Disable gateway connection")

	fs.Parse(os.Args[1:])

	// Load password in order of preference:
	// 1. From password file (most secure for production)
	// 2. From SIDECAR_PASSWORD environment variable
	// 3. Empty (will fail if credentials are needed)
	if conf.PasswordFilePath != "" {
		passwordBytes, err := os.ReadFile(conf.PasswordFilePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read password file %s: %v\n", conf.PasswordFilePath, err)
			return nil
		}
		// Trim whitespace/newlines from password file
		conf.KeystorePass = strings.TrimSpace(string(passwordBytes))
		fmt.Fprintf(os.Stderr, "Loaded password from file: %s (length: %d)\n", conf.PasswordFilePath, len(conf.KeystorePass))
	} else if envPass := os.Getenv("SIDECAR_PASSWORD"); envPass != "" {
		conf.KeystorePass = envPass
		fmt.Fprintf(os.Stderr, "Loaded password from SIDECAR_PASSWORD env var (length: %d)\n", len(conf.KeystorePass))
	} else {
		fmt.Fprintf(os.Stderr, "WARNING: No password configured (no --password-file and no SIDECAR_PASSWORD env var)\n")
	}

	conf.PoolMaxDuration = time.Duration(poolMaxDurationMs) * time.Millisecond
	conf.AuctionCycleTime = time.Duration(auctionCycleMs) * time.Millisecond
	conf.StreamingDelay = time.Duration(streamingDelayMs) * time.Millisecond

	// Derive socket paths from home directory
	conf.NodeToSidecarSocketPath = filepath.Join(conf.HomePath, NodeToSidecarSuffix)
	conf.SidecarToNodeSocketPath = filepath.Join(conf.HomePath, SidecarToNodeSuffix)

	// Build full paths for delegation and keystore files
	conf.DelegationPath = filepath.Join(conf.HomePath, conf.DelegationPath)
	conf.KeystorePath = filepath.Join(conf.HomePath, conf.KeystorePath)

	// Set fastlane contract address based on network or override
	if contractOverride != "" {
		conf.FastlaneContract = contractOverride
	} else {
		addr, ok := FastlaneContractAddresses[conf.Network]
		if !ok {
			// If network not found, list available networks and exit
			availableNetworks := make([]string, 0, len(FastlaneContractAddresses))
			for net := range FastlaneContractAddresses {
				availableNetworks = append(availableNetworks, net)
			}
			fmt.Fprintf(os.Stderr, "Unknown network '%s'. Available networks: %v\n", conf.Network, availableNetworks)
			os.Exit(1)
		}
		conf.FastlaneContract = addr
	}

	return &conf
}
