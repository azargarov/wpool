package workerpool

import (
	"time"
)

// Metrics is a snapshot of pool internals useful for monitoring.
// All fields are read-only through accessor methods.
type Metrics struct {
	submitted uint64
	executed  uint64
	queued    int
	maxAge    time.Duration
	active    map[int]bool
}

// Submitted returns the total number of jobs accepted by the pool.
func (m Metrics) Submitted() uint64 { return m.submitted }

// Metrics returns a copy of the current metrics snapshot.
func (p *Pool[T]) Metrics() Metrics {
	p.metricsMu.Lock()
	defer p.metricsMu.Unlock()
	return p.metrics
}

// Executed returns the total number of jobs processed by workers
// (including canceled ones).
func (m Metrics) Executed() uint64 { return m.executed }

// SetWorkerState marks worker id as active/inactive. It is called internally
// by the pool when workers start or stop.
func (p *Pool[T]) SetWorkerState(id int, state bool) {
	p.metricsMu.Lock()
	defer p.metricsMu.Unlock()
	p.metrics.active[id] = state
}

// ActiveWorkers counts how many workers are currently marked active.
func (p *Pool[T]) ActiveWorkers() int {
	p.metricsMu.Lock()
	defer p.metricsMu.Unlock()
	count := 0
	for _, v := range p.metrics.active {
		if v {
			count++
		}
	}
	return count
}

func (p *Pool[T]) incSubmitted() {
	p.metricsMu.Lock()
	defer p.metricsMu.Unlock()
	p.metrics.submitted++
}

func (p *Pool[T]) incExecuted() {
	p.metricsMu.Lock()
	defer p.metricsMu.Unlock()
	p.metrics.executed++
}

func (p *Pool[T]) setQueued(n int) {
	p.metricsMu.Lock()
	defer p.metricsMu.Unlock()
	p.metrics.queued = n
}

func (p *Pool[T]) setMaxAge(d time.Duration) {
	p.metricsMu.Lock()
	defer p.metricsMu.Unlock()
	p.metrics.maxAge = d
}
