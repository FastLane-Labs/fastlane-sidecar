package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// mockHealthStats implements HealthStatsProvider for testing
type mockHealthStats struct {
	stats HealthStatsForPrometheus
}

func (m *mockHealthStats) GetHealthStatsForPrometheus() HealthStatsForPrometheus {
	return m.stats
}

func TestSidecarCollector_RegistersAndCollects(t *testing.T) {
	m := &Metrics{}

	// Set known counter values
	m.TxReceivedFromNode.Store(42)
	m.TxSentToNode.Store(35)
	m.TOBBidsProcessed.Store(8)
	m.BackrunBidsProcessed.Store(3)
	m.NormalTxsProcessed.Store(20)
	m.BackrunPairsMatched.Store(2)
	m.TxDropped.Store(1)
	m.PoolCleanupOps.Store(7)
	m.TxExpired.Store(4)
	m.DecodeErrors.Store(2)
	m.SendErrors.Store(1)
	m.NodeReconnections.Store(1)
	m.DiskReadBytes.Store(1024)
	m.DiskWriteBytes.Store(2048)
	m.NetworkRecvBytes.Store(4096)
	m.NetworkSentBytes.Store(8192)

	// Set known gauge values
	m.PoolSize.Store(15)
	m.NodeConnected.Store(1)
	m.MemoryUsageBytes.Store(104857600) // 100 MB
	m.SetCPUUsagePercent(23.5)
	m.SetMemoryUsagePercent(45.2)
	m.GoroutinesCount.Store(50)

	// Set latency values: 3 samples totaling 300000 micros = 0.1s average
	m.TxProcessingLatencySum.Store(300000)
	m.TxProcessingLatencyCount.Store(3)
	m.NodeMessageLatencySum.Store(600000)
	m.NodeMessageLatencyCount.Store(6)

	hp := &mockHealthStats{
		stats: HealthStatsForPrometheus{
			TxReceived:     99,
			TxStreamed:     77,
			PoolSize:       12,
			LastReceivedAt: 1700000000.0,
			LastSentAt:     1700000001.5,
		},
	}

	collector := NewSidecarCollector(m, hp)

	reg := prometheus.NewRegistry()
	if err := reg.Register(collector); err != nil {
		t.Fatalf("Failed to register collector: %v", err)
	}

	// Scrape via HTTP
	handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	req := httptest.NewRequest(http.MethodGet, "/prometheus/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read body: %v", err)
	}
	output := string(body)

	// Verify counters with exact values
	expectMetric(t, output, "sidecar_tx_received_from_node_total", "42")
	expectMetric(t, output, "sidecar_tx_sent_to_node_total", "35")
	expectMetric(t, output, "sidecar_tob_bids_processed_total", "8")
	expectMetric(t, output, "sidecar_backrun_bids_processed_total", "3")
	expectMetric(t, output, "sidecar_normal_txs_processed_total", "20")
	expectMetric(t, output, "sidecar_backrun_pairs_matched_total", "2")
	expectMetric(t, output, "sidecar_tx_dropped_total", "1")
	expectMetric(t, output, "sidecar_pool_cleanup_ops_total", "7")
	expectMetric(t, output, "sidecar_tx_expired_total", "4")
	expectMetric(t, output, "sidecar_tx_processing_count_total", "3")
	expectMetric(t, output, "sidecar_node_message_count_total", "6")
	expectMetric(t, output, "sidecar_node_reconnections_total", "1")
	expectMetric(t, output, "sidecar_decode_errors_total", "2")
	expectMetric(t, output, "sidecar_send_errors_total", "1")
	expectMetric(t, output, "sidecar_disk_read_bytes_total", "1024")
	expectMetric(t, output, "sidecar_disk_write_bytes_total", "2048")
	expectMetric(t, output, "sidecar_network_recv_bytes_total", "4096")
	expectMetric(t, output, "sidecar_network_sent_bytes_total", "8192")

	// Verify gauges
	expectMetric(t, output, "sidecar_pool_size", "15")
	expectMetric(t, output, "sidecar_node_connected", "1")
	expectMetric(t, output, "sidecar_memory_usage_bytes", "1.048576e+08")
	expectMetric(t, output, "sidecar_goroutines_count", "50")
	// avg latency: 300000 micros / 3 / 1e6 = 0.1s
	expectMetric(t, output, "sidecar_avg_tx_processing_latency_seconds", "0.1")
	expectMetric(t, output, "sidecar_avg_node_message_latency_seconds", "0.1")

	// Verify health stats
	expectMetric(t, output, "sidecar_health_tx_received_total", "99")
	expectMetric(t, output, "sidecar_health_tx_streamed_total", "77")
	expectMetric(t, output, "sidecar_health_pool_size", "12")
	expectMetric(t, output, "sidecar_health_last_received_at_seconds", "1.7e+09")
	expectMetric(t, output, "sidecar_health_last_sent_at_seconds", "1.7000000015e+09")

	// Verify Go runtime gauges are present (values are dynamic, just check presence)
	expectMetricPresent(t, output, "sidecar_go_heap_alloc_bytes")
	expectMetricPresent(t, output, "sidecar_go_heap_sys_bytes")
	expectMetricPresent(t, output, "sidecar_go_heap_idle_bytes")
	expectMetricPresent(t, output, "sidecar_go_heap_inuse_bytes")
	expectMetricPresent(t, output, "sidecar_go_heap_released_bytes")
	expectMetricPresent(t, output, "sidecar_go_gc_runs_total")
	expectMetricPresent(t, output, "sidecar_go_goroutines")

	// Verify info metric has labels
	expectMetricPresent(t, output, "sidecar_info{")

	// Verify arrival-after-commit histogram is present (no observations, so count=0)
	expectContains(t, output, "# TYPE sidecar_tx_arrival_after_commit_ms histogram")
	expectContains(t, output, "sidecar_tx_arrival_after_commit_ms_count 0")
	expectContains(t, output, "sidecar_tx_arrival_after_commit_ms_sum 0")

	// Verify priority round-trip histogram is present (no observations, so count=0)
	expectContains(t, output, "# TYPE sidecar_priority_round_trip_ms histogram")
	expectContains(t, output, "sidecar_priority_round_trip_ms_count 0")
	expectContains(t, output, "sidecar_priority_round_trip_ms_sum 0")

	// Verify HELP and TYPE lines exist for a sample of metrics
	expectContains(t, output, "# HELP sidecar_tx_received_from_node_total")
	expectContains(t, output, "# TYPE sidecar_tx_received_from_node_total counter")
	expectContains(t, output, "# HELP sidecar_pool_size")
	expectContains(t, output, "# TYPE sidecar_pool_size gauge")
	expectContains(t, output, "# TYPE sidecar_info gauge")
}

func TestSidecarCollector_NilHealthStats(t *testing.T) {
	m := &Metrics{}
	collector := NewSidecarCollector(m, nil)

	reg := prometheus.NewRegistry()
	if err := reg.Register(collector); err != nil {
		t.Fatalf("Failed to register collector: %v", err)
	}

	handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	req := httptest.NewRequest(http.MethodGet, "/prometheus/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read body: %v", err)
	}
	output := string(body)

	// Core metrics should still be present
	expectMetricPresent(t, output, "sidecar_tx_received_from_node_total")
	expectMetricPresent(t, output, "sidecar_pool_size")
	expectMetricPresent(t, output, "sidecar_info{")

	// Health metrics should be absent since provider is nil
	if strings.Contains(output, "sidecar_health_tx_received_total") {
		t.Error("Expected health metrics to be absent when provider is nil")
	}
}

func TestSidecarCollector_ZeroLatency(t *testing.T) {
	m := &Metrics{}
	// Leave latency counts at 0 — should produce 0 average, not NaN
	collector := NewSidecarCollector(m, nil)

	reg := prometheus.NewRegistry()
	reg.MustRegister(collector)

	handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	req := httptest.NewRequest(http.MethodGet, "/prometheus/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Result().Body)
	output := string(body)

	expectMetric(t, output, "sidecar_avg_tx_processing_latency_seconds", "0")
	expectMetric(t, output, "sidecar_avg_node_message_latency_seconds", "0")
}

func TestSidecarCollector_ArrivalAfterCommitHistogram(t *testing.T) {
	m := &Metrics{}

	// Record some observations:
	// 3ms → le5 bucket, 7ms → le10 bucket, 150ms → le200 bucket
	m.RecordTxArrivalAfterCommit(3)
	m.RecordTxArrivalAfterCommit(7)
	m.RecordTxArrivalAfterCommit(150)

	collector := NewSidecarCollector(m, nil)

	reg := prometheus.NewRegistry()
	reg.MustRegister(collector)

	handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	req := httptest.NewRequest(http.MethodGet, "/prometheus/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Result().Body)
	output := string(body)

	// Verify histogram metadata
	expectContains(t, output, "# TYPE sidecar_tx_arrival_after_commit_ms histogram")

	// Verify cumulative bucket counts:
	// le5=1, le10=2, le20=2, le50=2, le100=2, le200=3, le500=3, le1000=3
	expectContains(t, output, `sidecar_tx_arrival_after_commit_ms_bucket{le="5"} 1`)
	expectContains(t, output, `sidecar_tx_arrival_after_commit_ms_bucket{le="10"} 2`)
	expectContains(t, output, `sidecar_tx_arrival_after_commit_ms_bucket{le="20"} 2`)
	expectContains(t, output, `sidecar_tx_arrival_after_commit_ms_bucket{le="50"} 2`)
	expectContains(t, output, `sidecar_tx_arrival_after_commit_ms_bucket{le="100"} 2`)
	expectContains(t, output, `sidecar_tx_arrival_after_commit_ms_bucket{le="200"} 3`)
	expectContains(t, output, `sidecar_tx_arrival_after_commit_ms_bucket{le="500"} 3`)
	expectContains(t, output, `sidecar_tx_arrival_after_commit_ms_bucket{le="1000"} 3`)
	expectContains(t, output, `sidecar_tx_arrival_after_commit_ms_bucket{le="+Inf"} 3`)

	// Verify count and sum
	expectContains(t, output, "sidecar_tx_arrival_after_commit_ms_count 3")
	// sum = 3+7+150 = 160
	expectContains(t, output, "sidecar_tx_arrival_after_commit_ms_sum 160")
}

func TestSidecarCollector_PriorityRoundTripHistogram(t *testing.T) {
	m := &Metrics{}

	// Record observations: 3ms → le4, 9ms → le10, 18ms → le20
	m.RecordPriorityRoundTrip(3)
	m.RecordPriorityRoundTrip(9)
	m.RecordPriorityRoundTrip(18)

	collector := NewSidecarCollector(m, nil)

	reg := prometheus.NewRegistry()
	reg.MustRegister(collector)

	handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	req := httptest.NewRequest(http.MethodGet, "/prometheus/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Result().Body)
	output := string(body)

	// Verify histogram metadata
	expectContains(t, output, "# TYPE sidecar_priority_round_trip_ms histogram")

	// Cumulative: le2=0, le4=1, le6=1, le8=1, le10=2, le15=2, le20=3, le30=3, le50=3, le100=3
	expectContains(t, output, `sidecar_priority_round_trip_ms_bucket{le="2"} 0`)
	expectContains(t, output, `sidecar_priority_round_trip_ms_bucket{le="4"} 1`)
	expectContains(t, output, `sidecar_priority_round_trip_ms_bucket{le="6"} 1`)
	expectContains(t, output, `sidecar_priority_round_trip_ms_bucket{le="8"} 1`)
	expectContains(t, output, `sidecar_priority_round_trip_ms_bucket{le="10"} 2`)
	expectContains(t, output, `sidecar_priority_round_trip_ms_bucket{le="15"} 2`)
	expectContains(t, output, `sidecar_priority_round_trip_ms_bucket{le="20"} 3`)
	expectContains(t, output, `sidecar_priority_round_trip_ms_bucket{le="30"} 3`)
	expectContains(t, output, `sidecar_priority_round_trip_ms_bucket{le="50"} 3`)
	expectContains(t, output, `sidecar_priority_round_trip_ms_bucket{le="100"} 3`)
	expectContains(t, output, `sidecar_priority_round_trip_ms_bucket{le="+Inf"} 3`)

	// count=3, sum=3+9+18=30
	expectContains(t, output, "sidecar_priority_round_trip_ms_count 3")
	expectContains(t, output, "sidecar_priority_round_trip_ms_sum 30")
}

// expectMetric checks that a line "metric_name <value>" appears in the output.
func expectMetric(t *testing.T, output, name, value string) {
	t.Helper()
	expected := name + " " + value
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.TrimSpace(line) == expected {
			return
		}
	}
	t.Errorf("Expected metric line %q not found in output:\n%s", expected, output)
}

// expectMetricPresent checks that any non-comment line starts with the given prefix.
func expectMetricPresent(t *testing.T, output, prefix string) {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, prefix) {
			return
		}
	}
	t.Errorf("Expected metric with prefix %q not found in output", prefix)
}

// expectContains checks that the output contains the given substring.
func expectContains(t *testing.T, output, substr string) {
	t.Helper()
	if !strings.Contains(output, substr) {
		t.Errorf("Expected output to contain %q", substr)
	}
}
