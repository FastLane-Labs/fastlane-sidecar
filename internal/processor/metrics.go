package processor

import (
	"sync/atomic"
	"time"
)

type Metrics struct {
	transactionsReceived  atomic.Uint64
	transactionsValidated atomic.Uint64
	transactionsForwarded atomic.Uint64
	transactionsRejected  atomic.Uint64
	bytesProcessed        atomic.Uint64
	startTime             time.Time
}

func NewMetrics() *Metrics {
	return &Metrics{
		startTime: time.Now(),
	}
}

func (m *Metrics) IncrementReceived() {
	m.transactionsReceived.Add(1)
}

func (m *Metrics) IncrementValidated() {
	m.transactionsValidated.Add(1)
}

func (m *Metrics) IncrementForwarded() {
	m.transactionsForwarded.Add(1)
}

func (m *Metrics) IncrementRejected() {
	m.transactionsRejected.Add(1)
}

func (m *Metrics) AddBytes(bytes uint64) {
	m.bytesProcessed.Add(bytes)
}

func (m *Metrics) GetStats() map[string]any {
	return map[string]any{
		"transactions_received":  m.transactionsReceived.Load(),
		"transactions_validated": m.transactionsValidated.Load(),
		"transactions_forwarded": m.transactionsForwarded.Load(),
		"transactions_rejected":  m.transactionsRejected.Load(),
		"bytes_processed":        m.bytesProcessed.Load(),
		"uptime_seconds":         time.Since(m.startTime).Seconds(),
	}
}
