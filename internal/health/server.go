package health

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
)

// StatsProvider interface for components that can provide health statistics
type StatsProvider interface {
	GetHealthStats() Stats
}

// MetricsProvider interface for components that can provide metrics snapshots
type MetricsProvider interface {
	GetSnapshot() interface{}
}

// Stats contains sidecar health status
type Stats struct {
	TxReceived      uint64    `json:"tx_received"`
	TxStreamed      uint64    `json:"tx_streamed"`
	PoolSize        uint64    `json:"pool_size"`
	LastReceivedAt  time.Time `json:"last_received_at"`
	LastSentAt      time.Time `json:"last_sent_at"`
	MonadBftVersion string    `json:"monad_bft_version,omitempty"`
}

// Server provides HTTP monitoring endpoints (health and metrics)
type Server struct {
	statsProvider   StatsProvider
	metricsProvider MetricsProvider
	httpServer      *http.Server
}

// NewServer creates a new monitoring server with health and metrics endpoints
func NewServer(port int, statsProvider StatsProvider, metricsProvider MetricsProvider) *Server {
	mux := http.NewServeMux()
	s := &Server{
		statsProvider:   statsProvider,
		metricsProvider: metricsProvider,
		httpServer: &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: mux,
		},
	}

	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/metrics", s.handleMetrics)
	return s
}

// Start starts the HTTP server
func (s *Server) Start() error {
	log.Info("Starting monitoring server", "addr", s.httpServer.Addr, "endpoints", "/health,/metrics")
	return s.httpServer.ListenAndServe()
}

// Stop gracefully stops the HTTP server
func (s *Server) Stop() error {
	log.Info("Stopping monitoring server")
	return s.httpServer.Close()
}

// handleHealth handles GET /health requests
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := s.statsProvider.GetHealthStats()

	response := map[string]interface{}{
		"status":      "ok",
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"tx_received": stats.TxReceived,
		"tx_streamed": stats.TxStreamed,
		"pool_size":   stats.PoolSize,
	}

	if !stats.LastReceivedAt.IsZero() {
		response["last_received_at"] = stats.LastReceivedAt.Format(time.RFC3339)
	}
	if !stats.LastSentAt.IsZero() {
		response["last_sent_at"] = stats.LastSentAt.Format(time.RFC3339)
	}
	if stats.MonadBftVersion != "" {
		response["monad_bft_version"] = stats.MonadBftVersion
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
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

// handleMetrics handles GET /metrics requests
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get metrics snapshot
	snapshot := s.metricsProvider.GetSnapshot()

	// Also include runtime info
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	response := map[string]interface{}{
		"metrics": snapshot,
		"runtime": map[string]interface{}{
			"go_version":        runtime.Version(),
			"goroutines":        runtime.NumGoroutine(),
			"heap_alloc_mb":     float64(m.Alloc) / 1024.0 / 1024.0,
			"heap_sys_mb":       float64(m.HeapSys) / 1024.0 / 1024.0,
			"heap_idle_mb":      float64(m.HeapIdle) / 1024.0 / 1024.0,
			"heap_inuse_mb":     float64(m.HeapInuse) / 1024.0 / 1024.0,
			"heap_released_mb":  float64(m.HeapReleased) / 1024.0 / 1024.0,
			"gc_runs":           m.NumGC,
			"last_gc_time_ns":   m.LastGC,
			"next_gc_target_mb": float64(m.NextGC) / 1024.0 / 1024.0,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Error("Failed to encode metrics", "error", err)
	}
}
