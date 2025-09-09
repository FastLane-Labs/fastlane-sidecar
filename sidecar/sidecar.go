package sidecar

import (
	jsonApiRpc "github.com/FastLane-Labs/fastlane-json-rpc/rpc"
	"github.com/FastLane-Labs/fastlane-sidecar/config"
	"github.com/prometheus/client_golang/prometheus"
)

type Sidecar struct {
	config       *config.Config
	api          *SidecarApi
	shutdownChan chan struct{}
}

func NewSidecar(config *config.Config, shutdownChan chan struct{}) *Sidecar {
	api := NewSidecarApi()
	return &Sidecar{
		config:       config,
		api:          api,
		shutdownChan: shutdownChan,
	}
}

func (s *Sidecar) Start() error {
	_, err := s.startJsonRpcServer()
	if err != nil {
		return err
	}

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
