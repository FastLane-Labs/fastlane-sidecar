package logtracker

import "github.com/FastLane-Labs/fastlane-sidecar/config"

type LogCallback func(line string)

type LogTracker interface {
	RegisterCallback(logType LogType, cb LogCallback)
	Start()
	Stop()
}

var _ LogTracker = &DockerLogTracker{}
var _ LogTracker = &SystemdLogTracker{}

//var _ LogTracker = &OtelLogTracker{}

func NewLogTracker(config *config.Config) LogTracker {
	switch config.LogTrackerType {
	case "docker":
		return NewDockerLogTracker(config.DockerContainerID)
	case "systemd":
		return NewSystemdLogTracker(config.SystemdUnitName)
	// case "otel":
	// 	return NewOtelLogTracker(config.)
	default:
		return nil
	}
}
