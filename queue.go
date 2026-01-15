// schedQueue defines the minimal interface required by the scheduler
// to enqueue and dequeue jobs.
//
// Implementations are expected to be safe for concurrent producers.
// Consumer-side concurrency depends on the concrete implementation.
//
// The interface is intentionally small to allow multiple queue
// strategies (e.g. segmented, bucketed, priority-based) to be swapped
// without affecting the pool logic.
// The schedQueue abstraction decouples scheduling policy from execution.
// This allows experimentation with different queue designs without
//
// impacting the worker pool core.
// NOTE: New queue types can be added here without modifying
// the scheduler or worker logic, as long as they implement schedQueue.
package workerpool

import (
)


type schedQueue[T any] interface {
	// Push appends a job to the queue.
	//
	// It returns true once the job has been successfully enqueued
	// and made visible to consumers.
	Push(job Job[T]) bool

	// BatchPop removes and returns a contiguous batch of jobs.
	//
	// The returned slice must be treated as read-only.
	// The boolean result reports whether any jobs were returned.
	BatchPop() ([]Job[T], bool)

	// Len returns the number of jobs currently enqueued.
	//
	// Implementations may return an approximate value.
	Len() int
}

// makeQueue constructs the scheduler queue according to pool options.
//
// The selected queue implementation defines the scheduling behavior
// and performance characteristics of the pool.
func (p *Pool[T, M]) makeQueue() schedQueue[T] {
	switch p.opts.QT {
	case SegmentedQueue:
		return NewSegmentedQ[T](uint32(p.opts.SegmentSize), p.opts.SegmentCount)

	default:
		return NewSegmentedQ[T](DefaultSegmentSize, DefaultSegmentCount)

	}
}
