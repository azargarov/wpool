package workerpool

import (
	"time"
)

// item represents a scheduled job stored inside one of the internal
// scheduler queues (heap-based or bucket-based).
//
// For the priority (heap) scheduler, an item carries its base priority,
// enqueue timestamp, and its current effective priority (eff), which is
// recomputed during aging. The container/heap implementation requires
// that each item track its index within the heap.
//
// For the bucket-based scheduler, the prio field stores the discrete
// bucket index assigned at insertion time.
//type item[T any] struct {
//	// job is the actual job payload and execution function.
//	job Job[T]
//
//	// basePrio is the user-provided priority value supplied at Submit time.
//	basePrio int
//
//	// queuedAt records when the job entered the scheduler.
//	// Used for aging in the priority (heap) scheduler.
//	queuedAt time.Time
//
//}

type schedQueue[T any] interface {

	// Push inserts a newly submitted job into the queue.
	//
	// basePrio is the user-provided priority value, and now is the
	// enqueue timestamp. FIFO implementations can ignore both.
	Push(job Job[T], basePrio int, now time.Time) bool

	// Pop retrieves and removes the next job to dispatch.
	//
	// It returns the selected job and a boolean flag indicating
	// whether a job was available. If false, the queue is empty.
	Pop(now time.Time) (Job[T], bool)

	BatchPop() ([]Job[T], bool)

	// Len returns the current number of jobs waiting in the queue.
	//
	// The scheduler uses this to update runtime metrics.
	Len() int
}

func (p *Pool[T, M]) makeQueue() schedQueue[T] {
	switch p.opts.QT {
	case SegmentedQueue:
		return NewSegmentedQ[T](uint32(p.opts.SegmentSize), p.opts.SegmentCount)

	default:
		return NewSegmentedQ[T](DefaultSegmentSize, DefaultSegmentCount)

	}
}
