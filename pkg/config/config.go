package config

import (
	"flag"
	"os"
)

type Config struct {
	LogLevel string
	IPCPath  string
}

func NewConfig() *Config {
	var conf Config

	fs := flag.NewFlagSet("UserConfig", flag.ExitOnError)
	fs.StringVar(&conf.LogLevel, "log-level", "debug", "Log level")
	fs.StringVar(&conf.IPCPath, "ipc-path", "/tmp/monad-validator.ipc", "Unix domain socket path for IPC connection")

	fs.Parse(os.Args[1:])
	return &conf
}
