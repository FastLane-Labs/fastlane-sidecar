package logtracker

import (
	"fmt"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/log"
	"github.com/coreos/go-systemd/v22/sdjournal"
)

type SystemdLogTracker struct {
	UnitName  string
	Parser    LogParser
	callbacks map[LogType][]LogCallback
	stopCh    chan struct{}
}

func NewSystemdLogTracker(unitName string) *SystemdLogTracker {
	parser := &NilParser{}
	return &SystemdLogTracker{
		UnitName:  unitName,
		Parser:    parser,
		callbacks: make(map[LogType][]LogCallback),
		stopCh:    make(chan struct{}),
	}
}

func (lt *SystemdLogTracker) RegisterCallback(logType LogType, cb LogCallback) {
	lt.callbacks[logType] = append(lt.callbacks[logType], cb)
}

func (lt *SystemdLogTracker) Start() {
	go lt.runWithBackoff()
}

func (lt *SystemdLogTracker) Stop() {
	close(lt.stopCh)
}

func (lt *SystemdLogTracker) runWithBackoff() {
	backoff := time.Second
	for {
		select {
		case <-lt.stopCh:
			return
		default:
			err := lt.streamLogs()
			if err != nil {
				log.Error("systemdLogTracker", "log stream error", "error", err, "backoff", backoff)
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

func (lt *SystemdLogTracker) streamLogs() error {
	journal, err := sdjournal.NewJournal()
	if err != nil {
		return fmt.Errorf("open journal: %w", err)
	}
	defer journal.Close()

	if err := journal.AddMatch(fmt.Sprintf("_SYSTEMD_UNIT=%s", lt.UnitName)); err != nil {
		return fmt.Errorf("add match: %w", err)
	}

	if err := journal.SeekTail(); err != nil {
		return fmt.Errorf("seek tail: %w", err)
	}
	journal.Next()

	for {
		select {
		case <-lt.stopCh:
			return nil
		default:
			journal.Wait(sdjournal.IndefiniteWait)

			if n, err := journal.Next(); err != nil || n == 0 {
				continue
			}

			entry, err := journal.GetEntry()
			if err != nil {
				continue
			}

			line := entry.Fields["MESSAGE"]
			log.Debug("systemdLogTracker", "line", line)
			logTypes := lt.Parser.ParseLog(line)
			for _, logType := range logTypes {
				for _, cb := range lt.callbacks[logType] {
					cb(line)
				}
			}
		}
	}
}
