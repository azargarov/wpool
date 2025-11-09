package workerpool

import (
	"time"
)

// fifoQueue implements a simple first-in–first-out queue for jobs.
// It satisfies the schedQueue[T] interface used by the pool scheduler.
// Jobs are processed strictly in arrival order with no prioritization or aging.
type fifoQueue[T any] struct {
	q []Job[T]
}

// Push appends a new job to the tail of the FIFO queue.
//
// The parameters basePrio and now are ignored because FIFO
// scheduling does not use priorities or timestamps.
func (f *fifoQueue[T]) Push(job Job[T], _ float64, _ time.Time) {
	f.q = append(f.q, job)
}

// Pop removes and returns the oldest job in the queue.
//
// If the queue is empty, Pop returns a zero Job[T] value and false.
func (f *fifoQueue[T]) Pop(_ time.Time) (Job[T], bool) {
	if len(f.q) == 0 {
		return Job[T]{}, false
	}
	job := f.q[0]
	f.q = f.q[1:]
	return job, true
}

// Tick is a no-op for FIFO queues since they do not implement
// aging or periodic reordering based on job age or priority.
func (f *fifoQueue[T]) Tick(_ time.Time) {}

// Len returns the number of jobs currently waiting in the queue.
func (f *fifoQueue[T]) Len() int {
	return len(f.q)
}

// MaxAge always returns 0 for FIFO queues, since they do not track
// per-job waiting time or compute aging metrics.
func (f *fifoQueue[T]) MaxAge() time.Duration {
	return 0
}
