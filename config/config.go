package config

import (
	"flag"
	"os"
)

type Config struct {
	LogLevel            string
	LogTrackerType      string
	DockerContainerID   string
	SystemdUnitName     string
	HttpPort            int
	HealthcheckEndpoint string
}

func NewConfig() *Config {
	var conf Config

	fs := flag.NewFlagSet("UserConfig", flag.ExitOnError)
	fs.StringVar(&conf.LogLevel, "log-level", "debug", "Log level")
	fs.StringVar(&conf.LogTrackerType, "log-tracker-type", "docker", "Log tracker type")
	fs.StringVar(&conf.DockerContainerID, "docker-container-id", "", "Docker container ID")
	fs.StringVar(&conf.SystemdUnitName, "systemd-unit-name", "", "Systemd unit name")
	fs.IntVar(&conf.HttpPort, "http-port", 8080, "HTTP port")
	fs.StringVar(&conf.HealthcheckEndpoint, "healthcheck-endpoint", "/health", "Healthcheck endpoint")

	fs.Parse(os.Args[1:])
	return &conf
}
