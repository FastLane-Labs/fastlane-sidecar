package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/FastLane-Labs/fastlane-sidecar/internal/orchestrator"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/config"
	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
)

func main() {
	config := config.NewConfig()
	log.InitLogger(config.LogLevel)
	log.Info("config", "config", config)

	shutdownChan := make(chan struct{})
	sidecar, err := orchestrator.NewSidecar(config, shutdownChan)
	if err != nil {
		log.Error("Failed to create sidecar", "error", err)
		panic(err)
	}

	if err := sidecar.Start(); err != nil {
		log.Error("Failed to start sidecar", "error", err)
		panic(err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Info("shutdown signal received", "signal", sig.String())
		sidecar.Stop()
	}()

	log.Info("Sidecar started ...")
	<-shutdownChan
	log.Info("Shutting down ...")
}
