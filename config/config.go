package config

import (
	"flag"
	"os"
)

type Config struct {
	LogLevel            string
	ContainerID         string
	HttpPort            int
	HealthcheckEndpoint string
}

func NewConfig() *Config {
	var conf Config

	fs := flag.NewFlagSet("UserConfig", flag.ExitOnError)
	fs.StringVar(&conf.LogLevel, "log-level", "debug", "Log level")
	fs.StringVar(&conf.ContainerID, "container-id", "", "Container ID")
	fs.IntVar(&conf.HttpPort, "http-port", 8080, "HTTP port")
	fs.StringVar(&conf.HealthcheckEndpoint, "healthcheck-endpoint", "/health", "Healthcheck endpoint")

	fs.Parse(os.Args[1:])
	return &conf
}
