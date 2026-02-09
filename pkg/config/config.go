package config

import (
	"flag"
	"fmt"
	"os"
	"time"
)

const (
	TxPoolSocketName = "mempool.sock" // Txpool IPC socket name
)

// FastlaneContractAddresses maps network names to their FastLaneAuctionHandler contract addresses
var FastlaneContractAddresses = map[string]string{
	"testnet": "0x11f34d16BB4B898c3a489B40cD1024d89A313b88",
	"mainnet": "0xD32EdF6642D917DbBE7B8BF8e5d6F5df6a9FFF58",
}

type Config struct {
	LogLevel         string
	Network          string
	HomePath         string
	TxPoolSocketPath string // Txpool IPC socket path (default: /home/monad/monad-bft/mempool.sock)
	PoolMaxDuration  time.Duration
	FastlaneContract string // Hex address of the fastlane auction contract

	// Monitoring configuration
	MonitoringPort    int  // HTTP port for monitoring endpoints (/health and /metrics)
	PrometheusEnabled bool // Enable Prometheus metrics endpoint at /prometheus/metrics
}

func NewConfig() *Config {
	var conf Config
	var poolMaxDurationMs int
	var contractOverride string
	var txpoolSocketPath string

	fs := flag.NewFlagSet("UserConfig", flag.ExitOnError)

	// Custom usage function
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Fastlane Sidecar - MEV sidecar for Monad validators\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "The sidecar runs alongside a Monad validator to enhance MEV capture capabilities.\n")
		fmt.Fprintf(os.Stderr, "It communicates with the validator via Unix sockets.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
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
	fs.StringVar(&txpoolSocketPath, "txpool-socket", "/home/monad/monad-bft/mempool.sock", "Txpool IPC socket path")
	fs.IntVar(&poolMaxDurationMs, "pool-max-duration-ms", 2500, "Maximum time to hold transactions in pool (ms)")
	fs.StringVar(&contractOverride, "fastlane-contract", "", "Override fastlane contract address (optional)")
	fs.IntVar(&conf.MonitoringPort, "monitoring-port", 8765, "HTTP port for monitoring endpoints (/health and /metrics)")
	fs.BoolVar(&conf.PrometheusEnabled, "prometheus", true, "Enable Prometheus metrics endpoint at /prometheus/metrics")

	fs.Parse(os.Args[1:])

	// Validate network parameter
	if conf.Network != "testnet" && conf.Network != "mainnet" {
		fmt.Fprintf(os.Stderr, "Error: network must be either 'testnet' or 'mainnet', got '%s'\n", conf.Network)
		os.Exit(1)
	}

	conf.PoolMaxDuration = time.Duration(poolMaxDurationMs) * time.Millisecond

	// Set txpool socket path
	conf.TxPoolSocketPath = txpoolSocketPath

	// Set fastlane contract address based on network
	if contractOverride != "" {
		conf.FastlaneContract = contractOverride
	} else {
		conf.FastlaneContract = FastlaneContractAddresses[conf.Network]
	}

	return &conf
}
