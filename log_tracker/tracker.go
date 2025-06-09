package logtracker

import (
	"bufio"
	"context"
	"fmt"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/log"
	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

type LogCallback func(line string)

type LogTracker struct {
	ContainerID string
	Parser      LogParser
	callbacks   map[LogType][]LogCallback
	stopCh      chan struct{}
}

func NewLogTracker(containerID string) *LogTracker {
	parser := &NilParser{}

	return &LogTracker{
		ContainerID: containerID,
		Parser:      parser,
		callbacks:   make(map[LogType][]LogCallback),
		stopCh:      make(chan struct{}),
	}
}

func (lt *LogTracker) RegisterCallback(logType LogType, cb LogCallback) {
	lt.callbacks[logType] = append(lt.callbacks[logType], cb)
}

func (lt *LogTracker) Start() {
	go lt.runWithBackoff()
}

func (lt *LogTracker) runWithBackoff() {
	backoff := time.Second
	for {
		select {
		case <-lt.stopCh:
			return
		default:
			err := lt.streamLogs()
			if err != nil {
				log.Error("logTracker", "log stream error", "error", err, "backoff", backoff)
				time.Sleep(backoff)
				if backoff < 30*time.Second {
					backoff *= 2
				}
			} else {
				backoff = time.Second
			}
		}
	}
}

func (lt *LogTracker) streamLogs() error {
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("docker client init: %w", err)
	}

	options := containertypes.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: false,
		Tail:       "0",
	}

	out, err := cli.ContainerLogs(ctx, lt.ContainerID, options)
	if err != nil {
		return fmt.Errorf("fetching logs: %w", err)
	}
	defer out.Close()

	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		line := scanner.Text()
		log.Debug("logTracker", "line", line)
		logTypes := lt.Parser.ParseLog(line)
		for _, logType := range logTypes {
			for _, cb := range lt.callbacks[logType] {
				cb(line)
			}
		}
	}
	return scanner.Err()
}

func (lt *LogTracker) Stop() {
	close(lt.stopCh)
}
