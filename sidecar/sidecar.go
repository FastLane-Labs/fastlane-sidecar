package sidecar

import (
	jsonApiRpc "github.com/FastLane-Labs/fastlane-json-rpc/rpc"
	"github.com/FastLane-Labs/fastlane-sidecar/config"
	logtracker "github.com/FastLane-Labs/fastlane-sidecar/log_tracker"
	"github.com/prometheus/client_golang/prometheus"
)

type Sidecar struct {
	config       *config.Config
	logTracker   logtracker.LogTracker
	api          *SidecarApi
	shutdownChan chan struct{}
}

func NewSidecar(config *config.Config, shutdownChan chan struct{}) *Sidecar {
	logTracker := logtracker.NewLogTracker(config)
	api := NewSidecarApi(logTracker)
	return &Sidecar{
		config:       config,
		logTracker:   logTracker,
		api:          api,
		shutdownChan: shutdownChan,
	}
}

func (s *Sidecar) Start() error {
	_, err := s.startJsonRpcServer()
	if err != nil {
		return err
	}

	go func() {
		s.logTracker.Start()
	}()

	return nil
}

func (s *Sidecar) startJsonRpcServer() (*jsonApiRpc.Server, error) {
	reg := prometheus.DefaultRegisterer
	return jsonApiRpc.NewServer(
		&jsonApiRpc.RpcConfig{
			Port:                uint64(s.config.HttpPort),
			HealthcheckEndpoint: s.config.HealthcheckEndpoint,
			HTTP: &jsonApiRpc.HttpConfig{
				Enabled: true,
			},
			Websocket: &jsonApiRpc.WebsocketConfig{
				Enabled: true,
			},
		},
		s.api,
		s.HealthCheck,
		reg,
	)
}
