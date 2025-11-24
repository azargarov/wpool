// fifo_queue.go
package workerpool

import "time"

const (
	initialFifoCapacity = 1000
)

// fifoQueue implements a simple fixed-capacity first-in–first-out job queue.
//
// It satisfies the schedQueue[T] interface used by the scheduler.
// Jobs are processed strictly in the order they are submitted.
// No priorities, no aging, no reordering.
type fifoQueue[T any] struct {
	buf        []Job[T] // circular buffer
	head, tail int      // read/write indices
	size       int      // number of jobs currently buffered
	capacity   int
}

// newFifoQueue creates a FIFO queue with the given capacity.
// The capacity determines how many jobs can be queued before
// Push starts dropping new submissions.
func newFifoQueue[T any](cap int) *fifoQueue[T] {
	return &fifoQueue[T]{
		buf:      make([]Job[T], cap),
		capacity: cap,
	}
}

// Len returns the number of jobs currently waiting in the queue.
func (q *fifoQueue[T]) Len() int { return q.size }

// Push inserts a job at the tail of the FIFO queue.
//
// FIFO does not use priority or timestamps, so basePrio and now
// are ignored. If the queue is full, the job is silently dropped
// (consistent with existing behavior). You may choose to return
// an error in the future for debugging purposes.
func (q *fifoQueue[T]) Push(j Job[T], _ float64, _ time.Time) {
	if q.size == q.capacity {
		// queue full — drop job (non-blocking model)
		return
	}
	q.buf[q.tail] = j
	q.tail++
	if q.tail == q.capacity {
		q.tail = 0
	}
	q.size++
}

// Pop removes and returns the oldest job.
//
// If the queue is empty, returns zero-value Job[T] and false.
func (q *fifoQueue[T]) Pop(_ time.Time) (Job[T], bool) {
	if q.size == 0 {
		return Job[T]{}, false
	}
	j := q.buf[q.head]
	q.head++
	if q.head == q.capacity {
		q.head = 0
	}
	q.size--
	return j, true
}

// Tick is a no-op for FIFO queues.
func (q *fifoQueue[T]) Tick(_ time.Time) {}

// MaxAge always returns zero, since FIFO does not track per-job
// timestamps or waiting durations.
func (q *fifoQueue[T]) MaxAge() time.Duration {
	return 0
}
