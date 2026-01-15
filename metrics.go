package workerpool

import (
	"sync/atomic"
)

// MetricsPolicy defines hooks used by the worker pool to report
// queueing and execution activity.
//
// Implementations must be safe for concurrent use.
// All methods are expected to be lightweight and non-blocking
type MetricsPolicy interface {

	// IncExecuted increments the executed jobs counter.
	IncExecuted()

	// IncQueued increments the queued jobs counter.
	IncQueued()

	// BatchDecQueued decrements the queued counter by n.
	//
	// This is typically used when a batch of jobs is removed
	// from the scheduling queue.
	BatchDecQueued(n int64)
}

// AtomicMetrics is a lock-free metrics implementation backed by atomics.
//
// Writes are optimized for hot paths.
// Reads are intended for cold-path observation.
type AtomicMetrics struct {
	// executed is the total number of jobs processed.
	executed atomic.Uint64

	_ [56]byte // padding to avoid false sharing

	// queued is the current number of jobs enqueued.
	queued atomic.Int64
}

// Executed returns the total number of executed jobs.
// Intended for cold-path observation.
func (m *AtomicMetrics) Executed() uint64 {
	return m.executed.Load()
}

// Queued returns the current number of queued jobs.
// Intended for cold-path observation.
func (m *AtomicMetrics) Queued() int64 {
	return m.queued.Load()
}

// IncExecuted increments the executed jobs counter by one.
func (m *AtomicMetrics) IncExecuted() {
	m.executed.Add(1)
}

// IncQueued increments the queued jobs counter by one.
func (m *AtomicMetrics) IncQueued() {
	m.queued.Add(1)
}

// BatchDecQueued decrements the queued jobs counter by
func (m *AtomicMetrics) BatchDecQueued(n int64) {
	m.queued.Add(-n)
}

//------------- NoopMetrics ----------------------------------

// NoopMetrics is a MetricsPolicy implementation that discards
// all metric updates.
//
// It can be used when metrics collection is disabled and
// zero overhead is desired.
type NoopMetrics struct{}

func (m *NoopMetrics) IncExecuted()           {}
func (m *NoopMetrics) IncQueued()             {}
func (m *NoopMetrics) BatchDecQueued(n int64) {}
