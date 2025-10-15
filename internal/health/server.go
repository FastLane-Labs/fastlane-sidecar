package health

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
)

// StatsProvider interface for components that can provide health statistics
type StatsProvider interface {
	GetHealthStats() Stats
}

// Stats contains sidecar health metrics
type Stats struct {
	LastHeartbeat        time.Time `json:"last_heartbeat"`
	TxReceived           uint64    `json:"tx_received"`
	TxStreamed           uint64    `json:"tx_streamed"`
	PoolSize             uint64    `json:"pool_size"`
	GatewayConnected     bool      `json:"gateway_connected"`
	GatewayAuthenticated bool      `json:"gateway_authenticated"`
	GatewayError         string    `json:"gateway_error,omitempty"`
}

// Server provides HTTP health endpoint
type Server struct {
	statsProvider StatsProvider
	httpServer    *http.Server
}

// NewServer creates a new health server
func NewServer(port int, statsProvider StatsProvider) *Server {
	mux := http.NewServeMux()
	s := &Server{
		statsProvider: statsProvider,
		httpServer: &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: mux,
		},
	}

	mux.HandleFunc("/health", s.handleHealth)
	return s
}

// Start starts the HTTP server
func (s *Server) Start() error {
	log.Info("Starting health server", "addr", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// Stop gracefully stops the HTTP server
func (s *Server) Stop() error {
	log.Info("Stopping health server")
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
		"last_heartbeat":        stats.LastHeartbeat.Format(time.RFC3339),
		"tx_received":           stats.TxReceived,
		"tx_streamed":           stats.TxStreamed,
		"pool_size":             stats.PoolSize,
		"gateway_connected":     stats.GatewayConnected,
		"gateway_authenticated": stats.GatewayAuthenticated,
		"timestamp":             time.Now().UTC().Format(time.RFC3339),
	}

	if stats.GatewayError != "" {
		response["gateway_error"] = stats.GatewayError
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
