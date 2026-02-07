package workerpool

// Batch represents a contiguous group of jobs dequeued from a schedQueue.
//
// The batch must be completed by calling OnBatchDone on the originating
// queue to allow proper resource reclamation.
type Batch[T any] struct {
	Jobs []Job[T]
	Seg  *segment[T]
	End  uint32
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
	Push(job Job[T]) bool

	// BatchPop removes and returns a contiguous batch of jobs.
	//
	// The returned slice must be treated as read-only.
	// The boolean result reports whether any jobs were returned.
	BatchPop() (Batch[T], bool) 

	// OnBatchDone signals that a previously dequeued batch
	// has finished processing.
	OnBatchDone(b Batch[T])

	// MaybeHasWork performs a fast, approximate check
	// for whether the queue may contain work.
	MaybeHasWork() bool
	
	// Len returns the number of jobs currently enqueued.
	//
	// Implementations may return an approximate value.
	Len() int
}

