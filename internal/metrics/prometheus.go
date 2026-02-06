package metrics

import (
	"runtime"

	"github.com/prometheus/client_golang/prometheus"
)

// HealthStatsForPrometheus is the subset of health stats the collector needs.
type HealthStatsForPrometheus struct {
	TxReceived     uint64
	TxStreamed     uint64
	PoolSize       uint64
	LastReceivedAt float64 // unix seconds, 0 if never
	LastSentAt     float64 // unix seconds, 0 if never
}

// HealthStatsProvider supplies health stats to the Prometheus collector.
type HealthStatsProvider interface {
	GetHealthStatsForPrometheus() HealthStatsForPrometheus
}

// SidecarCollector implements prometheus.Collector by reading existing atomic
// metric fields on every scrape — no duplicate recording logic needed.
type SidecarCollector struct {
	m             *Metrics
	healthStats   HealthStatsProvider
	descs         []*prometheus.Desc
	counterDescs  map[string]*prometheus.Desc
	gaugeDescs    map[string]*prometheus.Desc
	infoDesc      *prometheus.Desc
}

func NewSidecarCollector(m *Metrics, hp HealthStatsProvider) *SidecarCollector {
	c := &SidecarCollector{
		m:            m,
		healthStats:  hp,
		counterDescs: make(map[string]*prometheus.Desc),
		gaugeDescs:   make(map[string]*prometheus.Desc),
	}
	c.initDescs()
	return c
}

// helper to register a counter descriptor
func (c *SidecarCollector) counter(name, help string) {
	d := prometheus.NewDesc(name, help, nil, nil)
	c.counterDescs[name] = d
	c.descs = append(c.descs, d)
}

// helper to register a gauge descriptor
func (c *SidecarCollector) gauge(name, help string) {
	d := prometheus.NewDesc(name, help, nil, nil)
	c.gaugeDescs[name] = d
	c.descs = append(c.descs, d)
}

func (c *SidecarCollector) initDescs() {
	// --- Counters (monotonically increasing) ---
	c.counter("sidecar_tx_received_from_node_total", "Transactions received from node")
	c.counter("sidecar_tx_received_from_gateway_total", "Transactions received from gateway")
	c.counter("sidecar_tx_sent_to_node_total", "Transactions sent to node")
	c.counter("sidecar_tx_sent_to_gateway_total", "Transactions sent to gateway")
	c.counter("sidecar_tob_bids_processed_total", "TOB bids processed")
	c.counter("sidecar_backrun_bids_processed_total", "Backrun bids processed")
	c.counter("sidecar_normal_txs_processed_total", "Normal transactions processed")
	c.counter("sidecar_backrun_pairs_matched_total", "Backrun pairs matched")
	c.counter("sidecar_tx_dropped_total", "Transactions dropped")
	c.counter("sidecar_pool_cleanup_ops_total", "Pool cleanup operations")
	c.counter("sidecar_tx_expired_total", "Transactions expired")
	c.counter("sidecar_tx_processing_count_total", "Total tx processing count")
	c.counter("sidecar_node_message_count_total", "Total node message count")
	c.counter("sidecar_gateway_reconnections_total", "Gateway reconnections")
	c.counter("sidecar_node_reconnections_total", "Node reconnections")
	c.counter("sidecar_decode_errors_total", "Decode errors")
	c.counter("sidecar_send_errors_total", "Send errors")
	c.counter("sidecar_gateway_errors_total", "Gateway errors")
	c.counter("sidecar_disk_read_bytes_total", "Disk bytes read")
	c.counter("sidecar_disk_write_bytes_total", "Disk bytes written")
	c.counter("sidecar_network_recv_bytes_total", "Network bytes received")
	c.counter("sidecar_network_sent_bytes_total", "Network bytes sent")

	// --- Gauges (point-in-time) ---
	c.gauge("sidecar_pool_size", "Current transaction pool size")
	c.gauge("sidecar_avg_tx_processing_latency_seconds", "Average tx processing latency in seconds")
	c.gauge("sidecar_avg_node_message_latency_seconds", "Average node message latency in seconds")
	c.gauge("sidecar_node_connected", "Node connection status (0/1)")
	c.gauge("sidecar_gateway_connected", "Gateway connection status (0/1)")
	c.gauge("sidecar_gateway_authenticated", "Gateway authentication status (0/1)")
	c.gauge("sidecar_cpu_usage_percent", "CPU usage percent")
	c.gauge("sidecar_memory_usage_bytes", "Memory usage in bytes")
	c.gauge("sidecar_memory_usage_percent", "Memory usage percent")
	c.gauge("sidecar_goroutines_count", "Number of goroutines")

	// --- Health-specific gauges ---
	c.gauge("sidecar_health_tx_received_total", "Health: total tx received")
	c.gauge("sidecar_health_tx_streamed_total", "Health: total tx streamed")
	c.gauge("sidecar_health_pool_size", "Health: pool size")
	c.gauge("sidecar_health_last_received_at_seconds", "Health: unix timestamp of last received tx")
	c.gauge("sidecar_health_last_sent_at_seconds", "Health: unix timestamp of last sent tx")

	// --- Go runtime gauges ---
	c.gauge("sidecar_go_heap_alloc_bytes", "Go heap allocated bytes")
	c.gauge("sidecar_go_heap_sys_bytes", "Go heap system bytes")
	c.gauge("sidecar_go_heap_idle_bytes", "Go heap idle bytes")
	c.gauge("sidecar_go_heap_inuse_bytes", "Go heap in-use bytes")
	c.gauge("sidecar_go_heap_released_bytes", "Go heap released bytes")
	c.gauge("sidecar_go_gc_runs_total", "Go GC runs")
	c.gauge("sidecar_go_goroutines", "Go goroutine count")

	// --- Info metric ---
	c.infoDesc = prometheus.NewDesc(
		"sidecar_info",
		"Sidecar version information",
		[]string{"version", "monad_bft_version"}, nil,
	)
	c.descs = append(c.descs, c.infoDesc)
}

// Describe implements prometheus.Collector.
func (c *SidecarCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range c.descs {
		ch <- d
	}
}

// Collect implements prometheus.Collector. Reads atomics on every scrape.
func (c *SidecarCollector) Collect(ch chan<- prometheus.Metric) {
	m := c.m

	// --- Counters ---
	emitCounter := func(name string, v uint64) {
		ch <- prometheus.MustNewConstMetric(c.counterDescs[name], prometheus.CounterValue, float64(v))
	}
	emitCounter("sidecar_tx_received_from_node_total", m.TxReceivedFromNode.Load())
	emitCounter("sidecar_tx_received_from_gateway_total", m.TxReceivedFromGateway.Load())
	emitCounter("sidecar_tx_sent_to_node_total", m.TxSentToNode.Load())
	emitCounter("sidecar_tx_sent_to_gateway_total", m.TxSentToGateway.Load())
	emitCounter("sidecar_tob_bids_processed_total", m.TOBBidsProcessed.Load())
	emitCounter("sidecar_backrun_bids_processed_total", m.BackrunBidsProcessed.Load())
	emitCounter("sidecar_normal_txs_processed_total", m.NormalTxsProcessed.Load())
	emitCounter("sidecar_backrun_pairs_matched_total", m.BackrunPairsMatched.Load())
	emitCounter("sidecar_tx_dropped_total", m.TxDropped.Load())
	emitCounter("sidecar_pool_cleanup_ops_total", m.PoolCleanupOps.Load())
	emitCounter("sidecar_tx_expired_total", m.TxExpired.Load())
	emitCounter("sidecar_tx_processing_count_total", m.TxProcessingLatencyCount.Load())
	emitCounter("sidecar_node_message_count_total", m.NodeMessageLatencyCount.Load())
	emitCounter("sidecar_gateway_reconnections_total", m.GatewayReconnections.Load())
	emitCounter("sidecar_node_reconnections_total", m.NodeReconnections.Load())
	emitCounter("sidecar_decode_errors_total", m.DecodeErrors.Load())
	emitCounter("sidecar_send_errors_total", m.SendErrors.Load())
	emitCounter("sidecar_gateway_errors_total", m.GatewayErrors.Load())
	emitCounter("sidecar_disk_read_bytes_total", m.DiskReadBytes.Load())
	emitCounter("sidecar_disk_write_bytes_total", m.DiskWriteBytes.Load())
	emitCounter("sidecar_network_recv_bytes_total", m.NetworkRecvBytes.Load())
	emitCounter("sidecar_network_sent_bytes_total", m.NetworkSentBytes.Load())

	// --- Gauges ---
	emitGauge := func(name string, v float64) {
		ch <- prometheus.MustNewConstMetric(c.gaugeDescs[name], prometheus.GaugeValue, v)
	}
	emitGauge("sidecar_pool_size", float64(m.PoolSize.Load()))
	emitGauge("sidecar_avg_tx_processing_latency_seconds", m.GetAverageTxProcessingLatency())
	emitGauge("sidecar_avg_node_message_latency_seconds", m.GetAverageNodeMessageLatency())
	emitGauge("sidecar_node_connected", float64(m.NodeConnected.Load()))
	emitGauge("sidecar_gateway_connected", float64(m.GatewayConnected.Load()))
	emitGauge("sidecar_gateway_authenticated", float64(m.GatewayAuthenticated.Load()))
	emitGauge("sidecar_cpu_usage_percent", m.GetCPUUsagePercent())
	emitGauge("sidecar_memory_usage_bytes", float64(m.MemoryUsageBytes.Load()))
	emitGauge("sidecar_memory_usage_percent", m.GetMemoryUsagePercent())
	emitGauge("sidecar_goroutines_count", float64(m.GoroutinesCount.Load()))

	// --- Health stats ---
	if c.healthStats != nil {
		hs := c.healthStats.GetHealthStatsForPrometheus()
		emitGauge("sidecar_health_tx_received_total", float64(hs.TxReceived))
		emitGauge("sidecar_health_tx_streamed_total", float64(hs.TxStreamed))
		emitGauge("sidecar_health_pool_size", float64(hs.PoolSize))
		emitGauge("sidecar_health_last_received_at_seconds", hs.LastReceivedAt)
		emitGauge("sidecar_health_last_sent_at_seconds", hs.LastSentAt)
	}

	// --- Go runtime ---
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	emitGauge("sidecar_go_heap_alloc_bytes", float64(mem.Alloc))
	emitGauge("sidecar_go_heap_sys_bytes", float64(mem.HeapSys))
	emitGauge("sidecar_go_heap_idle_bytes", float64(mem.HeapIdle))
	emitGauge("sidecar_go_heap_inuse_bytes", float64(mem.HeapInuse))
	emitGauge("sidecar_go_heap_released_bytes", float64(mem.HeapReleased))
	emitGauge("sidecar_go_gc_runs_total", float64(mem.NumGC))
	emitGauge("sidecar_go_goroutines", float64(runtime.NumGoroutine()))

	// --- Info metric ---
	ch <- prometheus.MustNewConstMetric(
		c.infoDesc, prometheus.GaugeValue, 1,
		getSidecarVersion(), getMonadBftVersion(),
	)
}
