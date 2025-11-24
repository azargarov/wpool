package workerpool

import (
	"sync/atomic"
	"time"
)

// Metrics is a snapshot of pool internals useful for monitoring.
// All fields are read-only through accessor methods.
type Metrics struct {
	submitted     atomic.Uint64
	executed      atomic.Uint64
	queued        atomic.Int64
	maxAge        time.Duration
	workersActive []atomic.Bool
}

// Submitted returns the total number of jobs accepted by the pool.
func (m *Metrics) Submitted() uint64 { return m.submitted.Load() }

// Metrics returns a copy of the current metrics snapshot.
func (p *Pool[T]) Metrics() *Metrics {
	p.metricsMu.Lock()
	defer p.metricsMu.Unlock()
	return &p.metrics
}

// Executed returns the total number of jobs processed by workers
// (including canceled ones).
func (m *Metrics) Executed() uint64 { return m.executed.Load() }

// SetWorkerState marks worker id as active/inactive. It is called internally
// by the pool when workers start or stop.
func (p *Pool[T]) SetWorkerState(id int, state bool) {
	p.metrics.workersActive[id].Store(state)
}

// ActiveWorkers counts how many workers are currently marked active.
func (p *Pool[T]) ActiveWorkers() int {
	count := 0
	for i := range p.metrics.workersActive {
		if p.metrics.workersActive[i].Load() {
			count++
		}
	}
	return count
}

func (p *Pool[T]) incSubmitted() {
	p.metrics.submitted.Add(1)
}

func (p *Pool[T]) incExecuted() {
	p.metrics.executed.Add(1)
}

func (p *Pool[T]) setQueued(n int) {
	p.metrics.queued.Store(int64(n))
}

func (p *Pool[T]) setMaxAge(d time.Duration) {
	p.metricsMu.Lock()
	defer p.metricsMu.Unlock()
	p.metrics.maxAge = d
}
