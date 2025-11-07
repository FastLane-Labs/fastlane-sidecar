package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	NodeToSidecarSuffix = "node_to_sidecar"
	SidecarToNodeSuffix = "sidecar_to_node"
)

// FastlaneContractAddresses maps network names to their FastLaneAuctionHandler contract addresses
var FastlaneContractAddresses = map[string]string{
	"testnet": "0xb3688810bbd755808979BDaB1592bFb69b78A033",
	"mainnet": "0x0000000000000000000000000000000000000000",
}

type Config struct {
	LogLevel                string
	Network                 string
	HomePath                string
	NodeToSidecarSocketPath string // Derived from HomePath + ".node_to_sidecar"
	SidecarToNodeSocketPath string // Derived from HomePath + ".sidecar_to_node"
	PoolMaxDuration         time.Duration
	AuctionCycleTime        time.Duration
	FastlaneContract        string // Hex address of the fastlane auction contract

	// Monitoring configuration
	MonitoringPort int // HTTP port for monitoring endpoints (/health and /metrics)
}

func NewConfig() *Config {
	var conf Config
	var poolMaxDurationMs int
	var auctionCycleMs int
	var contractOverride string

	fs := flag.NewFlagSet("UserConfig", flag.ExitOnError)

	// Custom usage function
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Fastlane Sidecar - MEV sidecar for Monad validators\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "The sidecar runs alongside a Monad validator to enhance MEV capture capabilities.\n")
		fmt.Fprintf(os.Stderr, "It communicates with the validator via Unix sockets.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nIPC Socket Paths (derived from -home):\n")
		fmt.Fprintf(os.Stderr, "  Node → Sidecar: <home>/%s\n", NodeToSidecarSuffix)
		fmt.Fprintf(os.Stderr, "  Sidecar → Node: <home>/%s\n", SidecarToNodeSuffix)
		fmt.Fprintf(os.Stderr, "\nHealth Monitoring:\n")
		fmt.Fprintf(os.Stderr, "  Health endpoint: http://localhost:8765/health\n")
		fmt.Fprintf(os.Stderr, "  Metrics endpoint: http://localhost:8765/metrics\n")
		fmt.Fprintf(os.Stderr, "  Check status:    curl http://localhost:8765/health\n")
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Run with default settings\n")
		fmt.Fprintf(os.Stderr, "  %s\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Run with custom home directory\n")
		fmt.Fprintf(os.Stderr, "  %s -home=/var/lib/fastlane/\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Run on mainnet\n")
		fmt.Fprintf(os.Stderr, "  %s -network=mainnet\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Run with info logging\n")
		fmt.Fprintf(os.Stderr, "  %s -log-level=info\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "For more information, visit: https://github.com/FastLane-Labs/fastlane-sidecar\n")
	}

	fs.StringVar(&conf.Network, "network", "testnet", "Network name: testnet, mainnet")
	fs.StringVar(&conf.LogLevel, "log-level", "debug", "Log level (debug, info, warn, error)")
	fs.StringVar(&conf.HomePath, "home", "/home/monad/fastlane/", "Fastlane home directory")
	fs.IntVar(&poolMaxDurationMs, "pool-max-duration-ms", 2500, "Maximum time to hold transactions in pool (ms)")
	fs.IntVar(&auctionCycleMs, "auction-cycle-ms", 200, "Auction cycle interval (ms)")
	fs.StringVar(&contractOverride, "fastlane-contract", "", "Override fastlane contract address (optional)")
	fs.IntVar(&conf.MonitoringPort, "monitoring-port", 8765, "HTTP port for monitoring endpoints (/health and /metrics)")

	fs.Parse(os.Args[1:])

	// Validate network parameter
	if conf.Network != "testnet" && conf.Network != "mainnet" {
		fmt.Fprintf(os.Stderr, "Error: network must be either 'testnet' or 'mainnet', got '%s'\n", conf.Network)
		os.Exit(1)
	}

	conf.PoolMaxDuration = time.Duration(poolMaxDurationMs) * time.Millisecond
	conf.AuctionCycleTime = time.Duration(auctionCycleMs) * time.Millisecond

	// Derive socket paths from home directory
	conf.NodeToSidecarSocketPath = filepath.Join(conf.HomePath, NodeToSidecarSuffix)
	conf.SidecarToNodeSocketPath = filepath.Join(conf.HomePath, SidecarToNodeSuffix)

	// Set fastlane contract address based on network
	if contractOverride != "" {
		conf.FastlaneContract = contractOverride
	} else {
		conf.FastlaneContract = FastlaneContractAddresses[conf.Network]
	}

	return &conf
}
