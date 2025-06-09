package log

import (
	"os"

	"github.com/ethereum/go-ethereum/log"
)

func InitLogger(logLevel string) {
	switch logLevel {
	case "debug":
		log.SetDefault(log.NewLogger(log.NewTerminalHandlerWithLevel(os.Stdout, log.LevelDebug, true)))
	case "info":
		log.SetDefault(log.NewLogger(log.NewTerminalHandlerWithLevel(os.Stdout, log.LevelInfo, true)))
	case "warn":
		log.SetDefault(log.NewLogger(log.NewTerminalHandlerWithLevel(os.Stdout, log.LevelWarn, true)))
	case "error":
		log.SetDefault(log.NewLogger(log.NewTerminalHandlerWithLevel(os.Stdout, log.LevelError, true)))
	default:
		log.SetDefault(log.NewLogger(log.NewTerminalHandlerWithLevel(os.Stdout, log.LevelInfo, true)))
	}
}

func Debug(format string, v ...interface{}) {
	log.Debug(format, v...)
}

func Info(format string, v ...interface{}) {
	log.Info(format, v...)
}

func Warn(format string, v ...interface{}) {
	log.Warn(format, v...)
}

func Error(format string, v ...interface{}) {
	log.Error(format, v...)
}
