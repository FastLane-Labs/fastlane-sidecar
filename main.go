package main

import (
	"github.com/FastLane-Labs/fastlane-sidecar/config"
	"github.com/FastLane-Labs/fastlane-sidecar/log"
	"github.com/FastLane-Labs/fastlane-sidecar/sidecar"
)

func main() {
	config := config.NewConfig()
	log.InitLogger(config.LogLevel)
	log.Info("config", "config", config)

	shutdownChan := make(chan struct{})
	sidecar := sidecar.NewSidecar(config, shutdownChan)
	sidecar.Start()

	log.Info("Sidecar started ...")
	<-shutdownChan
	log.Info("Shutting down ...")
}
