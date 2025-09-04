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

func Debug(format string, v ...any) {
	log.Debug(format, v...)
}

func Info(format string, v ...any) {
	log.Info(format, v...)
}

func Warn(format string, v ...any) {
	log.Warn(format, v...)
}

func Error(format string, v ...any) {
	log.Error(format, v...)
}
