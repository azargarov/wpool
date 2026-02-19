package workerpool

import(
	"context"
	"errors"
)

const jobPrioMask  uint64 = 0b0011_1111 

var (
	// ErrQueueFull is returned when the underlying queue
	// cannot accept more jobs.
	ErrQueueFull = errors.New("queue: queue is full")

	// ErrNilFunc is returned when a submitted Job has a nil Fn.
	ErrNilFunc   = errors.New("queue: job func is nil")
)
// Batch represents a contiguous group of jobs dequeued from a schedQueue.
//
// The batch must be completed by calling OnBatchDone on the originating
// queue to allow proper resource reclamation.
type Batch[T any] struct {
	Jobs []Job[T]
	Seg  *segment[T]
	End  uint32
	
	// Meta is an optional, queue-private field.
	// It is opaque to the pool and interpreted only by the queue implementation.
	Meta	any
}

type JobPriority uint8

// JobFunc is the function executed by a worker for a given job payload.
type JobFunc[T any] func(T) error

// Job represents a single unit of work submitted to the pool.
//
// Payload is passed to Fn when executed.
// Ctx controls cancellation before execution.
// CleanupFunc, if set, is executed after job completion.
type Job[T any] struct {
	Payload     T
	Fn          JobFunc[T]
	Flags 		uint64
	Meta    	*JobMeta 
}

type JobMeta struct {
	Ctx         context.Context
	CleanupFunc func()
}

func (j *Job[T]) SetPriority(p JobPriority) {
	j.Flags = (j.Flags & ^jobPrioMask) | uint64(p)
}

func (j Job[T]) GetPriority() JobPriority {
	return JobPriority(j.Flags & jobPrioMask)
}

// schedQueue defines the minimal interface required by the scheduler
// to enqueue and dequeue jobs.
//
// Implementations are expected to be safe for concurrent producers.
// Consumer-side concurrency depends on the concrete implementation.
//
// The interface is intentionally small to allow multiple queue
// strategies (e.g. segmented, bucketed, priority-based) to be swapped
// without affecting the pool logic.
//
// The schedQueue abstraction decouples scheduling policy from execution,
// allowing experimentation with different queue designs without
// impacting the worker pool core.
type schedQueue[T any] interface {
	// Push appends a job to the queue.
	//
	// It returns true once the job has been successfully enqueued
	// and made visible to consumers.
	Push(job Job[T]) error

	// BatchPop removes and returns a contiguous batch of jobs.
	//
	// The returned slice must be treated as read-only.
	// The boolean result reports whether any jobs were returned.
	BatchPop() (Batch[T], bool) 

	// OnBatchDone signals that a previously dequeued batch
	// has finished processing.
	OnBatchDone(b Batch[T])

	StatSnapshot()string

	Close()
}

