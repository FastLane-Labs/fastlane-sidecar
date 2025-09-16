package main

import (
	"github.com/FastLane-Labs/fastlane-sidecar/internal/orchestrator"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/config"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
)

func main() {
	config := config.NewConfig()
	log.InitLogger(config.LogLevel)
	log.Info("config", "config", config)

	shutdownChan := make(chan struct{})
	sidecar := orchestrator.NewSidecar(config, shutdownChan)
	sidecar.Start()

	log.Info("Sidecar started ...")
	<-shutdownChan
	log.Info("Shutting down ...")
}
