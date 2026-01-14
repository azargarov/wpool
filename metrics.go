package workerpool

import (
	"sync/atomic"
)

type MetricsPolicy interface {
	Executed()
	IncExecuted()
	Queued()
	IncQueued()
	BatchDecQueued(n int64)
}

// Metrics is a snapshot of pool internals useful for monitoring.
// All fields are read-only through accessor methods.
type AtomicMetrics struct {
	executed atomic.Uint64
	_        [56]byte
	queued   atomic.Int64
}

// Executed returns the total number of jobs processed by workers
// (including canceled ones).
func (m *AtomicMetrics) Executed() {
	m.executed.Load()
}

func (m *AtomicMetrics) IncExecuted() {
	m.executed.Add(1)
}

func (m *AtomicMetrics) Queued() {
	m.queued.Load()
}

func (m *AtomicMetrics) IncQueued() {
	m.queued.Add(1)
}

func (m *AtomicMetrics) BatchDecQueued(n int64) {
	m.queued.Add(-n)
}

//-------------------------------------------------

type NoopMetrics struct{}

func (m *NoopMetrics) Executed()              {}
func (m *NoopMetrics) IncExecuted()           {}
func (m *NoopMetrics) Queued()                {}
func (m *NoopMetrics) IncQueued()             {}
func (m *NoopMetrics) BatchDecQueued(n int64) {}
