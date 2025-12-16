package workerpool

import (
	"sync/atomic"
	//"time"
)

// Metrics is a snapshot of pool internals useful for monitoring.
// All fields are read-only through accessor methods.
type Metrics struct {
	executed      atomic.Uint64
	queued        atomic.Int64
	workersActive []atomic.Bool
}


// Metrics returns a copy of the current metrics snapshot.
func (p *Pool[T]) Metrics() *Metrics {
	p.metricsMu.Lock()
	defer p.metricsMu.Unlock()
	return &p.metrics
}

// Executed returns the total number of jobs processed by workers
// (including canceled ones).
func (p *Pool[T]) Executed() uint64 { 
	return p.metrics.executed.Load() 
}

func (p *Pool[T]) incExecuted() uint64  { 
	return p.metrics.executed.Add(1) 
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

func (p *Pool[T]) Queued() int64 { 
	return p.metrics.queued.Load() 
}

func (p *Pool[T]) incQueued() int64 {
	return p.metrics.queued.Add(1)
}

func (p *Pool[T]) batchDecQueued(n int64) int64{
	return p.metrics.queued.Add(-n)
}
