package health

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
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
	LastHeartbeat        time.Time `json:"last_heartbeat"`
	TxReceived           uint64    `json:"tx_received"` // Kept for backward compatibility
	TxStreamed           uint64    `json:"tx_streamed"` // Kept for backward compatibility
	PoolSize             uint64    `json:"pool_size"`   // Kept for backward compatibility
	GatewayConnected     bool      `json:"gateway_connected"`
	GatewayAuthenticated bool      `json:"gateway_authenticated"`
	GatewayError         string    `json:"gateway_error,omitempty"`
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

	// Determine overall health status
	healthy := true
	if stats.LastHeartbeat.IsZero() || time.Since(stats.LastHeartbeat) > 60*time.Second {
		healthy = false // No heartbeat in last 60 seconds
	}

	response := map[string]interface{}{
		"status":                "ok",
		"healthy":               healthy,
		"last_heartbeat":        stats.LastHeartbeat.Format(time.RFC3339),
		"gateway_connected":     stats.GatewayConnected,
		"gateway_authenticated": stats.GatewayAuthenticated,
		"timestamp":             time.Now().UTC().Format(time.RFC3339),
		"note":                  "For detailed metrics, see /metrics endpoint (Prometheus format)",
		// Include basic stats for backward compatibility
		"tx_received": stats.TxReceived,
		"tx_streamed": stats.TxStreamed,
		"pool_size":   stats.PoolSize,
	}

	if stats.GatewayError != "" {
		response["gateway_error"] = stats.GatewayError
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
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
