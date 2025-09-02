package sidecar

import (
	"github.com/FastLane-Labs/fastlane-sidecar/config"
)

type Sidecar struct {
	config       *config.Config
	shutdownChan chan struct{}
}

func NewSidecar(config *config.Config, shutdownChan chan struct{}) *Sidecar {
	return &Sidecar{
		config:       config,
		shutdownChan: shutdownChan,
	}
}

func (s *Sidecar) Start() error {
	return nil
}
