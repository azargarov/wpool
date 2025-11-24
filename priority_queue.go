package workerpool

import (
	"container/heap"
	"time"
)

const (
	prioCap = 2048
)

// prioQueue implements a priority-based job queue used by the scheduler.
// Jobs are ordered by their *effective priority* which increases over time
// according to the configured aging rate. Older jobs naturally bubble up
// in priority to prevent starvation.
type prioQueue[T any] struct {
	pq        priorityQueue[T]
	agingRate float64
	maxAge    time.Duration
}

// newPrioQueue creates a new priority queue initialized as a max-heap.
// The agingRate parameter controls how quickly queued jobs gain priority
// relative to their waiting time.
func newPrioQueue[T any](agingRate float64) *prioQueue[T] {
	q := &prioQueue[T]{agingRate: agingRate}
	q.pq = make(priorityQueue[T], 0, prioCap) // preallocate
	heap.Init(&q.pq)
	return q
}

// Push inserts a new job into the priority queue.
//
// Each job is wrapped in an item that stores its base priority,
// enqueue timestamp, and computed effective priority.
func (p *prioQueue[T]) Push(job Job[T], basePrio float64, now time.Time) {
	it := item[T]{
		job:      job,
		basePrio: basePrio,
		queuedAt: now,
	}
	it.eff = effective(&it, p.agingRate, now)
	heap.Push(&p.pq, it)
}

// Pop removes and returns the job with the highest effective priority.
// If the queue is empty, Pop returns a zero Job[T] and false.
func (p *prioQueue[T]) Pop(_ time.Time) (Job[T], bool) {
	if p.pq.Len() == 0 {
		return Job[T]{}, false
	}
	it := heap.Pop(&p.pq).(item[T])
	return it.job, true
}

// Tick recalculates the effective priority of all queued jobs.
//
// This method is called periodically by the scheduler. For each job,
// Tick updates its effective priority based on its waiting time and the
// configured aging rate. Older jobs gain priority and naturally rise
// toward the top of the heap, preventing starvation.
//
// After all priorities are updated, the heap is reinitialized to restore
// correct ordering. The maximum observed job age is stored in maxAge and
// used for metrics reporting.
//
// Tick runs in O(n) time, where n is the number of queued jobs.
func (p *prioQueue[T]) Tick(now time.Time) {
	var maxAge time.Duration

	for i := range p.pq {
		age := now.Sub(p.pq[i].queuedAt)
		if age > maxAge {
			maxAge = age
		}
		p.pq[i].eff = effective(&p.pq[i], p.agingRate, now)
	}

	p.maxAge = maxAge
	heap.Init(&p.pq)
}

// effective computes the current effective priority of a job based on
// its base priority and how long it has been waiting in the queue.
func effective[T any](it *item[T], rate float64, now time.Time) float64 {
	age := now.Sub(it.queuedAt).Seconds()
	return it.basePrio + rate*age
}

// Len returns the number of jobs currently stored in the queue.
func (p *prioQueue[T]) Len() int {
	return p.pq.Len()
}

// MaxAge returns the maximum waiting time among all queued jobs.
// This value is updated each time Tick is called.
func (p *prioQueue[T]) MaxAge() time.Duration {
	return p.maxAge
}
