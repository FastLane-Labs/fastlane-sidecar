package config

import (
	"flag"
	"os"
	"time"
)

const (
	NodeToSidecarSuffix = ".node_to_sidecar"
	SidecarToNodeSuffix = ".sidecar_to_node"
)

type Config struct {
	LogLevel                string
	SocketBasePath          string
	NodeToSidecarSocketPath string // Derived from SocketBasePath + ".node_to_sidecar"
	SidecarToNodeSocketPath string // Derived from SocketBasePath + ".sidecar_to_node"
	GatewayURL              string
	PoolMaxDuration         time.Duration
	AuctionCycleTime        time.Duration
	StreamingDelay          time.Duration
	// TODO: Add when implementing transaction classification
	// FastlaneContract   common.Address
	// TOBMethodSig       [4]byte
	// BackrunMethodSig   [4]byte
}

func NewConfig() *Config {
	var conf Config
	var poolMaxDurationMs int
	var auctionCycleMs int
	var streamingDelayMs int

	fs := flag.NewFlagSet("UserConfig", flag.ExitOnError)
	fs.StringVar(&conf.LogLevel, "log-level", "debug", "Log level")
	fs.StringVar(&conf.SocketBasePath, "socket-base-path", "/tmp/fastlane_test", "Base path for Unix sockets (will append suffixes)")
	fs.StringVar(&conf.GatewayURL, "gateway-url", "ws://localhost:8080", "WebSocket URL for MEV gateway")
	fs.IntVar(&poolMaxDurationMs, "pool-max-duration-ms", 60000, "Maximum time to hold transactions in pool (ms)")
	fs.IntVar(&auctionCycleMs, "auction-cycle-ms", 200, "Auction cycle interval (ms)")
	fs.IntVar(&streamingDelayMs, "streaming-delay-ms", 100, "Delay before streaming auction results (ms)")

	fs.Parse(os.Args[1:])

	conf.PoolMaxDuration = time.Duration(poolMaxDurationMs) * time.Millisecond
	conf.AuctionCycleTime = time.Duration(auctionCycleMs) * time.Millisecond
	conf.StreamingDelay = time.Duration(streamingDelayMs) * time.Millisecond

	// Derive socket paths from base path
	conf.NodeToSidecarSocketPath = conf.SocketBasePath + NodeToSidecarSuffix
	conf.SidecarToNodeSocketPath = conf.SocketBasePath + SidecarToNodeSuffix

	return &conf
}
