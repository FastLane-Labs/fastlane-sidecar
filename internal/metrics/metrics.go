package metrics

import (
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/internal/version"
)

// Metrics holds all metrics for the sidecar
type Metrics struct {
	// Transaction counters
	TxReceivedFromNode    atomic.Uint64
	TxReceivedFromGateway atomic.Uint64
	TxSentToNode          atomic.Uint64
	TxSentToGateway       atomic.Uint64

	// Transaction type counters
	TOBBidsProcessed     atomic.Uint64
	BackrunBidsProcessed atomic.Uint64
	NormalTxsProcessed   atomic.Uint64
	BackrunPairsMatched  atomic.Uint64
	TxDropped            atomic.Uint64

	// Pool metrics
	PoolSize       atomic.Uint64
	PoolCleanupOps atomic.Uint64
	TxExpired      atomic.Uint64

	// Latency tracking (microseconds for precision)
	TxProcessingLatencySum   atomic.Uint64
	TxProcessingLatencyCount atomic.Uint64
	NodeMessageLatencySum    atomic.Uint64
	NodeMessageLatencyCount  atomic.Uint64

	// Connection status
	NodeConnected        atomic.Uint64 // 1=connected, 0=disconnected
	GatewayConnected     atomic.Uint64
	GatewayAuthenticated atomic.Uint64
	GatewayReconnections atomic.Uint64
	NodeReconnections    atomic.Uint64

	// Error counters
	DecodeErrors  atomic.Uint64
	SendErrors    atomic.Uint64
	GatewayErrors atomic.Uint64

	// System metrics (updated periodically)
	CPUUsagePercent    atomic.Uint64 // stored as uint64 * 100 for precision
	MemoryUsageBytes   atomic.Uint64
	MemoryUsagePercent atomic.Uint64 // stored as uint64 * 100
	DiskReadBytes      atomic.Uint64
	DiskWriteBytes     atomic.Uint64
	NetworkRecvBytes   atomic.Uint64
	NetworkSentBytes   atomic.Uint64
	GoroutinesCount    atomic.Uint64

	// Heartbeat
	LastHeartbeatTimestamp atomic.Int64 // Unix timestamp in seconds

	// System metrics collector
	systemCollector *SystemMetricsCollector
}

// Global metrics instance
var globalMetrics *Metrics

// InitMetrics initializes the global metrics registry
func InitMetrics() *Metrics {
	m := &Metrics{}
	m.systemCollector = NewSystemMetricsCollector(m)
	globalMetrics = m
	return m
}

// GetMetrics returns the global metrics instance
func GetMetrics() *Metrics {
	if globalMetrics == nil {
		return InitMetrics()
	}
	return globalMetrics
}

// StartSystemMetricsCollection starts collecting system metrics
func (m *Metrics) StartSystemMetricsCollection() {
	m.systemCollector.Start()
}

// StopSystemMetricsCollection stops collecting system metrics
func (m *Metrics) StopSystemMetricsCollection() {
	m.systemCollector.Stop()
}

// RecordTxProcessingLatency records transaction processing latency in seconds
func (m *Metrics) RecordTxProcessingLatency(seconds float64) {
	micros := uint64(seconds * 1_000_000)
	m.TxProcessingLatencySum.Add(micros)
	m.TxProcessingLatencyCount.Add(1)
}

// RecordNodeMessageLatency records node message latency in seconds
func (m *Metrics) RecordNodeMessageLatency(seconds float64) {
	micros := uint64(seconds * 1_000_000)
	m.NodeMessageLatencySum.Add(micros)
	m.NodeMessageLatencyCount.Add(1)
}

// GetAverageTxProcessingLatency returns average processing latency in seconds
func (m *Metrics) GetAverageTxProcessingLatency() float64 {
	count := m.TxProcessingLatencyCount.Load()
	if count == 0 {
		return 0
	}
	sum := m.TxProcessingLatencySum.Load()
	return float64(sum) / float64(count) / 1_000_000
}

// GetAverageNodeMessageLatency returns average node message latency in seconds
func (m *Metrics) GetAverageNodeMessageLatency() float64 {
	count := m.NodeMessageLatencyCount.Load()
	if count == 0 {
		return 0
	}
	sum := m.NodeMessageLatencySum.Load()
	return float64(sum) / float64(count) / 1_000_000
}

// SetCPUUsagePercent sets CPU usage with decimal precision
func (m *Metrics) SetCPUUsagePercent(percent float64) {
	m.CPUUsagePercent.Store(uint64(percent * 100))
}

// GetCPUUsagePercent gets CPU usage as float
func (m *Metrics) GetCPUUsagePercent() float64 {
	return float64(m.CPUUsagePercent.Load()) / 100.0
}

// SetMemoryUsagePercent sets memory usage with decimal precision
func (m *Metrics) SetMemoryUsagePercent(percent float64) {
	m.MemoryUsagePercent.Store(uint64(percent * 100))
}

// GetMemoryUsagePercent gets memory usage as float
func (m *Metrics) GetMemoryUsagePercent() float64 {
	return float64(m.MemoryUsagePercent.Load()) / 100.0
}

// Snapshot represents a point-in-time snapshot of all metrics
type Snapshot struct {
	Timestamp       time.Time `json:"timestamp"`
	Version         string    `json:"version"`
	MonadBftVersion string    `json:"monad_bft_version,omitempty"`

	// Transaction metrics
	TxReceivedFromNode    uint64 `json:"tx_received_from_node"`
	TxReceivedFromGateway uint64 `json:"tx_received_from_gateway"`
	TxSentToNode          uint64 `json:"tx_sent_to_node"`
	TxSentToGateway       uint64 `json:"tx_sent_to_gateway"`

	// Transaction types
	TOBBidsProcessed     uint64 `json:"tob_bids_processed"`
	BackrunBidsProcessed uint64 `json:"backrun_bids_processed"`
	NormalTxsProcessed   uint64 `json:"normal_txs_processed"`
	BackrunPairsMatched  uint64 `json:"backrun_pairs_matched"`
	TxDropped            uint64 `json:"tx_dropped"`

	// Pool metrics
	PoolSize       uint64 `json:"pool_size"`
	PoolCleanupOps uint64 `json:"pool_cleanup_ops"`
	TxExpired      uint64 `json:"tx_expired"`

	// Latency metrics (in seconds)
	AvgTxProcessingLatency float64 `json:"avg_tx_processing_latency_seconds"`
	AvgNodeMessageLatency  float64 `json:"avg_node_message_latency_seconds"`
	TxProcessingCount      uint64  `json:"tx_processing_count"`
	NodeMessageCount       uint64  `json:"node_message_count"`

	// Connection status
	NodeConnected        bool   `json:"node_connected"`
	GatewayConnected     bool   `json:"gateway_connected"`
	GatewayAuthenticated bool   `json:"gateway_authenticated"`
	GatewayReconnections uint64 `json:"gateway_reconnections"`
	NodeReconnections    uint64 `json:"node_reconnections"`

	// Error metrics
	DecodeErrors  uint64 `json:"decode_errors"`
	SendErrors    uint64 `json:"send_errors"`
	GatewayErrors uint64 `json:"gateway_errors"`

	// System metrics
	CPUUsagePercent    float64 `json:"cpu_usage_percent"`
	MemoryUsageMB      float64 `json:"memory_usage_mb"`
	MemoryUsagePercent float64 `json:"memory_usage_percent"`
	DiskReadMB         float64 `json:"disk_read_mb"`
	DiskWriteMB        float64 `json:"disk_write_mb"`
	NetworkRecvMB      float64 `json:"network_recv_mb"`
	NetworkSentMB      float64 `json:"network_sent_mb"`
	GoroutinesCount    uint64  `json:"goroutines_count"`

	// Heartbeat
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

// GetSnapshot returns a point-in-time snapshot of all metrics
func (m *Metrics) GetSnapshot() interface{} {
	lastHB := m.LastHeartbeatTimestamp.Load()
	var lastHeartbeat time.Time
	if lastHB > 0 {
		lastHeartbeat = time.Unix(lastHB, 0)
	}

	return Snapshot{
		Timestamp:       time.Now().UTC(),
		Version:         getSidecarVersion(),
		MonadBftVersion: getMonadBftVersion(),

		// Transaction metrics
		TxReceivedFromNode:    m.TxReceivedFromNode.Load(),
		TxReceivedFromGateway: m.TxReceivedFromGateway.Load(),
		TxSentToNode:          m.TxSentToNode.Load(),
		TxSentToGateway:       m.TxSentToGateway.Load(),

		// Transaction types
		TOBBidsProcessed:     m.TOBBidsProcessed.Load(),
		BackrunBidsProcessed: m.BackrunBidsProcessed.Load(),
		NormalTxsProcessed:   m.NormalTxsProcessed.Load(),
		BackrunPairsMatched:  m.BackrunPairsMatched.Load(),
		TxDropped:            m.TxDropped.Load(),

		// Pool metrics
		PoolSize:       m.PoolSize.Load(),
		PoolCleanupOps: m.PoolCleanupOps.Load(),
		TxExpired:      m.TxExpired.Load(),

		// Latency metrics
		AvgTxProcessingLatency: m.GetAverageTxProcessingLatency(),
		AvgNodeMessageLatency:  m.GetAverageNodeMessageLatency(),
		TxProcessingCount:      m.TxProcessingLatencyCount.Load(),
		NodeMessageCount:       m.NodeMessageLatencyCount.Load(),

		// Connection status
		NodeConnected:        m.NodeConnected.Load() == 1,
		GatewayConnected:     m.GatewayConnected.Load() == 1,
		GatewayAuthenticated: m.GatewayAuthenticated.Load() == 1,
		GatewayReconnections: m.GatewayReconnections.Load(),
		NodeReconnections:    m.NodeReconnections.Load(),

		// Error metrics
		DecodeErrors:  m.DecodeErrors.Load(),
		SendErrors:    m.SendErrors.Load(),
		GatewayErrors: m.GatewayErrors.Load(),

		// System metrics (convert bytes to MB)
		CPUUsagePercent:    m.GetCPUUsagePercent(),
		MemoryUsageMB:      float64(m.MemoryUsageBytes.Load()) / 1024.0 / 1024.0,
		MemoryUsagePercent: m.GetMemoryUsagePercent(),
		DiskReadMB:         float64(m.DiskReadBytes.Load()) / 1024.0 / 1024.0,
		DiskWriteMB:        float64(m.DiskWriteBytes.Load()) / 1024.0 / 1024.0,
		NetworkRecvMB:      float64(m.NetworkRecvBytes.Load()) / 1024.0 / 1024.0,
		NetworkSentMB:      float64(m.NetworkSentBytes.Load()) / 1024.0 / 1024.0,
		GoroutinesCount:    m.GoroutinesCount.Load(),

		// Heartbeat
		LastHeartbeat: lastHeartbeat,
	}
}

// getSidecarVersion returns the sidecar version combining hardcoded and dpkg versions
func getSidecarVersion() string {
	hardcoded := version.Version

	// Try to get dpkg package version
	cmd := exec.Command("dpkg-query", "-W", "-f=${Version}", "fastlane-sidecar")
	output, err := cmd.Output()
	if err == nil && len(output) > 0 {
		dpkgVersion := strings.TrimSpace(string(output))
		return hardcoded + "-" + dpkgVersion
	}

	// If dpkg query fails, return hardcoded-unknown
	return hardcoded + "-unknown"
}

// getMonadBftVersion attempts to get the monad package version
func getMonadBftVersion() string {
	// Try monad-fastlane first (FastLane version)
	cmd := exec.Command("dpkg-query", "-W", "-f=${Version}", "monad-fastlane")
	output, err := cmd.Output()
	if err == nil && len(output) > 0 {
		version := strings.TrimSpace(string(output))
		return "monad-fastlane: " + version
	}

	// Try monad (standard version)
	cmd = exec.Command("dpkg-query", "-W", "-f=${Version}", "monad")
	output, err = cmd.Output()
	if err == nil && len(output) > 0 {
		version := strings.TrimSpace(string(output))
		return "monad: " + version
	}

	// If both fail, return unknown
	return "unknown"
}
