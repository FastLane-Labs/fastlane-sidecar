package metrics

import (
	"fmt"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

// Metrics holds all metrics for the sidecar
type Metrics struct {
	// Transaction counters
	TxReceivedFromNode atomic.Uint64
	TxSentToNode       atomic.Uint64

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
	NodeConnected     atomic.Uint64 // 1=connected, 0=disconnected
	NodeReconnections atomic.Uint64

	// Error counters
	DecodeErrors atomic.Uint64
	SendErrors   atomic.Uint64

	// Distribution of (TX arrival - last commit) in milliseconds
	TxArrivalAfterCommitBuckets [8]atomic.Uint64 // bucket counts (non-cumulative)
	TxArrivalAfterCommitSum     atomic.Uint64    // sum of all values in microseconds
	TxArrivalAfterCommitCount   atomic.Uint64    // total observations

	// Distribution of priority round-trip latency in milliseconds
	// Measures: sidecar sends prioritized TX → node inserts it → echo Insert arrives back
	PriorityRoundTripBuckets [10]atomic.Uint64 // bucket counts (non-cumulative)
	PriorityRoundTripSum     atomic.Uint64     // sum in microseconds
	PriorityRoundTripCount   atomic.Uint64     // total observations

	// System metrics (updated periodically)
	CPUUsagePercent    atomic.Uint64 // stored as uint64 * 100 for precision
	MemoryUsageBytes   atomic.Uint64
	MemoryUsagePercent atomic.Uint64 // stored as uint64 * 100
	DiskReadBytes      atomic.Uint64
	DiskWriteBytes     atomic.Uint64
	NetworkRecvBytes   atomic.Uint64
	NetworkSentBytes   atomic.Uint64
	GoroutinesCount    atomic.Uint64

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

// TxArrivalAfterCommitBoundariesMs defines the upper bounds (in ms) for each histogram bucket.
// Bucket i counts observations <= BoundariesMs[i]. The last bucket is the overflow (>1000ms).
var TxArrivalAfterCommitBoundariesMs = [8]float64{5, 10, 20, 50, 100, 200, 500, 1000}

// RecordTxArrivalAfterCommit records how many milliseconds after the last commit a TX arrived.
func (m *Metrics) RecordTxArrivalAfterCommit(ms float64) {
	m.TxArrivalAfterCommitSum.Add(uint64(ms * 1000)) // store as microseconds
	m.TxArrivalAfterCommitCount.Add(1)
	for i, boundary := range TxArrivalAfterCommitBoundariesMs {
		if ms <= boundary {
			m.TxArrivalAfterCommitBuckets[i].Add(1)
			return
		}
	}
	m.TxArrivalAfterCommitBuckets[len(TxArrivalAfterCommitBoundariesMs)-1].Add(1) // overflow
}

// GetTxArrivalAfterCommitCumulativeBuckets returns cumulative bucket counts
// keyed by upper bound, suitable for Prometheus histogram exposition.
func (m *Metrics) GetTxArrivalAfterCommitCumulativeBuckets() (map[float64]uint64, uint64, float64) {
	totalCount := m.TxArrivalAfterCommitCount.Load()
	sumMicros := m.TxArrivalAfterCommitSum.Load()
	sumMs := float64(sumMicros) / 1000.0

	buckets := make(map[float64]uint64, len(TxArrivalAfterCommitBoundariesMs))
	var cumulative uint64
	for i, boundary := range TxArrivalAfterCommitBoundariesMs {
		cumulative += m.TxArrivalAfterCommitBuckets[i].Load()
		buckets[boundary] = cumulative
	}

	return buckets, totalCount, sumMs
}

// PriorityRoundTripBoundariesMs defines bucket upper bounds for the priority round-trip histogram.
var PriorityRoundTripBoundariesMs = [10]float64{2, 4, 6, 8, 10, 15, 20, 30, 50, 100}

// RecordPriorityRoundTrip records the round-trip latency (ms) from sending a
// prioritized TX to the node until the echo Insert event arrives back.
func (m *Metrics) RecordPriorityRoundTrip(ms float64) {
	m.PriorityRoundTripSum.Add(uint64(ms * 1000)) // store as microseconds
	m.PriorityRoundTripCount.Add(1)
	for i, boundary := range PriorityRoundTripBoundariesMs {
		if ms <= boundary {
			m.PriorityRoundTripBuckets[i].Add(1)
			return
		}
	}
	m.PriorityRoundTripBuckets[len(PriorityRoundTripBoundariesMs)-1].Add(1) // overflow
}

// GetPriorityRoundTripCumulativeBuckets returns cumulative bucket counts
// keyed by upper bound, suitable for Prometheus histogram exposition.
func (m *Metrics) GetPriorityRoundTripCumulativeBuckets() (map[float64]uint64, uint64, float64) {
	totalCount := m.PriorityRoundTripCount.Load()
	sumMicros := m.PriorityRoundTripSum.Load()
	sumMs := float64(sumMicros) / 1000.0

	buckets := make(map[float64]uint64, len(PriorityRoundTripBoundariesMs))
	var cumulative uint64
	for i, boundary := range PriorityRoundTripBoundariesMs {
		cumulative += m.PriorityRoundTripBuckets[i].Load()
		buckets[boundary] = cumulative
	}

	return buckets, totalCount, sumMs
}

// Snapshot represents a point-in-time snapshot of all metrics
type Snapshot struct {
	Timestamp       time.Time `json:"timestamp"`
	Version         string    `json:"version"`
	MonadBftVersion string    `json:"monad_bft_version,omitempty"`

	// Transaction metrics
	TxReceivedFromNode uint64 `json:"tx_received_from_node"`
	TxSentToNode       uint64 `json:"tx_sent_to_node"`

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
	NodeConnected     bool   `json:"node_connected"`
	NodeReconnections uint64 `json:"node_reconnections"`

	// Error metrics
	DecodeErrors uint64 `json:"decode_errors"`
	SendErrors   uint64 `json:"send_errors"`

	// TX arrival after commit distribution
	TxArrivalAfterCommitAvgMs   float64           `json:"tx_arrival_after_commit_avg_ms"`
	TxArrivalAfterCommitCount   uint64            `json:"tx_arrival_after_commit_count"`
	TxArrivalAfterCommitBuckets map[string]uint64 `json:"tx_arrival_after_commit_buckets"`

	// Priority round-trip distribution
	PriorityRoundTripAvgMs   float64           `json:"priority_round_trip_avg_ms"`
	PriorityRoundTripCount   uint64            `json:"priority_round_trip_count"`
	PriorityRoundTripBuckets map[string]uint64 `json:"priority_round_trip_buckets"`

	// System metrics
	CPUUsagePercent    float64 `json:"cpu_usage_percent"`
	MemoryUsageMB      float64 `json:"memory_usage_mb"`
	MemoryUsagePercent float64 `json:"memory_usage_percent"`
	DiskReadMB         float64 `json:"disk_read_mb"`
	DiskWriteMB        float64 `json:"disk_write_mb"`
	NetworkRecvMB      float64 `json:"network_recv_mb"`
	NetworkSentMB      float64 `json:"network_sent_mb"`
	GoroutinesCount    uint64  `json:"goroutines_count"`
}

// GetSnapshot returns a point-in-time snapshot of all metrics
func (m *Metrics) GetSnapshot() interface{} {
	return Snapshot{
		Timestamp:       time.Now().UTC(),
		Version:         getSidecarVersion(),
		MonadBftVersion: getMonadBftVersion(),

		// Transaction metrics
		TxReceivedFromNode: m.TxReceivedFromNode.Load(),
		TxSentToNode:       m.TxSentToNode.Load(),

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
		NodeConnected:     m.NodeConnected.Load() == 1,
		NodeReconnections: m.NodeReconnections.Load(),

		// Error metrics
		DecodeErrors: m.DecodeErrors.Load(),
		SendErrors:   m.SendErrors.Load(),

		// TX arrival after commit distribution
		TxArrivalAfterCommitAvgMs:   m.getAvgTxArrivalAfterCommitMs(),
		TxArrivalAfterCommitCount:   m.TxArrivalAfterCommitCount.Load(),
		TxArrivalAfterCommitBuckets: m.getArrivalBucketsSnapshot(),

		// Priority round-trip distribution
		PriorityRoundTripAvgMs:   m.getAvgPriorityRoundTripMs(),
		PriorityRoundTripCount:   m.PriorityRoundTripCount.Load(),
		PriorityRoundTripBuckets: m.getRoundTripBucketsSnapshot(),

		// System metrics (convert bytes to MB)
		CPUUsagePercent:    m.GetCPUUsagePercent(),
		MemoryUsageMB:      float64(m.MemoryUsageBytes.Load()) / 1024.0 / 1024.0,
		MemoryUsagePercent: m.GetMemoryUsagePercent(),
		DiskReadMB:         float64(m.DiskReadBytes.Load()) / 1024.0 / 1024.0,
		DiskWriteMB:        float64(m.DiskWriteBytes.Load()) / 1024.0 / 1024.0,
		NetworkRecvMB:      float64(m.NetworkRecvBytes.Load()) / 1024.0 / 1024.0,
		NetworkSentMB:      float64(m.NetworkSentBytes.Load()) / 1024.0 / 1024.0,
		GoroutinesCount:    m.GoroutinesCount.Load(),
	}
}

func (m *Metrics) getAvgTxArrivalAfterCommitMs() float64 {
	count := m.TxArrivalAfterCommitCount.Load()
	if count == 0 {
		return 0
	}
	sumMicros := m.TxArrivalAfterCommitSum.Load()
	return float64(sumMicros) / float64(count) / 1000.0
}

func (m *Metrics) getArrivalBucketsSnapshot() map[string]uint64 {
	buckets := make(map[string]uint64, len(TxArrivalAfterCommitBoundariesMs))
	for i, boundary := range TxArrivalAfterCommitBoundariesMs {
		label := fmt.Sprintf("le_%.0fms", boundary)
		buckets[label] = m.TxArrivalAfterCommitBuckets[i].Load()
	}
	return buckets
}

func (m *Metrics) getAvgPriorityRoundTripMs() float64 {
	count := m.PriorityRoundTripCount.Load()
	if count == 0 {
		return 0
	}
	sumMicros := m.PriorityRoundTripSum.Load()
	return float64(sumMicros) / float64(count) / 1000.0
}

func (m *Metrics) getRoundTripBucketsSnapshot() map[string]uint64 {
	buckets := make(map[string]uint64, len(PriorityRoundTripBoundariesMs))
	for i, boundary := range PriorityRoundTripBoundariesMs {
		label := fmt.Sprintf("le_%.0fms", boundary)
		buckets[label] = m.PriorityRoundTripBuckets[i].Load()
	}
	return buckets
}

// getSidecarVersion returns the sidecar version from dpkg
func getSidecarVersion() string {
	cmd := exec.Command("dpkg-query", "-W", "-f=${Version}", "fastlane-sidecar")
	output, err := cmd.Output()
	if err == nil && len(output) > 0 {
		return strings.TrimSpace(string(output))
	}
	return "unknown"
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
