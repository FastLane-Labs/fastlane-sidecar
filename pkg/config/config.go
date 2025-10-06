package config

import (
	"flag"
	"os"
	"path/filepath"
	"time"
)

const (
	NodeToSidecarSuffix = "node_to_sidecar"
	SidecarToNodeSuffix = "sidecar_to_node"
)

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
	TOBMethodSig            string // Hex signature of the TOB bid method (e.g., "0x12345678")
	BackrunMethodSig        string // Hex signature of the backrun bid method (e.g., "0x87654321")

	// Authentication parameters
	DelegationPath string // Path to delegation envelope JSON file
	KeystorePath   string // Path to sidecar keystore file
	KeystorePass   string // Password for sidecar keystore

	// Gateway control
	DisableGateway bool // Disable gateway connection
}

func NewConfig() *Config {
	var conf Config
	var poolMaxDurationMs int
	var auctionCycleMs int
	var streamingDelayMs int

	fs := flag.NewFlagSet("UserConfig", flag.ExitOnError)
	fs.StringVar(&conf.LogLevel, "log-level", "debug", "Log level")
	fs.StringVar(&conf.HomePath, "home", "/home/monad/fastlane/", "Fastlane home directory")
	fs.StringVar(&conf.GatewayURL, "gateway-url", "http://localhost:8080", "HTTP URL for MEV gateway (will be converted to WebSocket)")
	fs.IntVar(&poolMaxDurationMs, "pool-max-duration-ms", 60000, "Maximum time to hold transactions in pool (ms)")
	fs.IntVar(&auctionCycleMs, "auction-cycle-ms", 200, "Auction cycle interval (ms)")
	fs.IntVar(&streamingDelayMs, "streaming-delay-ms", 100, "Delay before streaming auction results (ms)")
	fs.StringVar(&conf.FastlaneContract, "fastlane-contract", "0x0000000000000000000000000000000000000000", "Fastlane auction contract address (hex)")
	fs.StringVar(&conf.TOBMethodSig, "tob-method-sig", "0x00000000", "TOB bid method signature (hex, e.g., 0x12345678)")
	fs.StringVar(&conf.BackrunMethodSig, "backrun-method-sig", "0x00000000", "Backrun bid method signature (hex, e.g., 0x87654321)")
	fs.StringVar(&conf.DelegationPath, "delegation", "delegation-envelope.json", "Delegation envelope JSON filename (relative to home)")
	fs.StringVar(&conf.KeystorePath, "keystore", "sidecar-keystore.json", "Sidecar keystore filename (relative to home)")
	fs.StringVar(&conf.KeystorePass, "password", "", "Password for sidecar keystore (or set SIDECAR_PASSWORD env var)")
	fs.BoolVar(&conf.DisableGateway, "disable-gateway", false, "Disable gateway connection")

	fs.Parse(os.Args[1:])

	// Allow password to be set via environment variable for security
	if conf.KeystorePass == "" {
		conf.KeystorePass = os.Getenv("SIDECAR_PASSWORD")
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

	return &conf
}
