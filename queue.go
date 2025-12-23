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
type item[T any] struct {
	// job is the actual job payload and execution function.
	job Job[T]

	// basePrio is the user-provided priority value supplied at Submit time.
	basePrio int

	// queuedAt records when the job entered the scheduler.
	// Used for aging in the priority (heap) scheduler.
	queuedAt time.Time

	// eff is the effective priority computed from basePrio and job age.
	// It is used only by the heap-based priority queue.
	//eff int

	// index is maintained by the heap-based queue. It stores the element’s
	// current position in the heap and is required by the heap.Interface.
	//index int

	// prio is the discrete bucket index used by the bucket-based queue.
	prio Prio
}

// submitReq is what the pool feeds into the scheduler.
// We separate it from item so we can attach timestamps inside the scheduler.
//type submitReq[T any] struct {
//	job      Job[T]
//	basePrio int
//	//respCh    chan error // TODO: future blocking submit
//}

// schedQueue defines the common behavior of all internal scheduler queues.
//
// A queue is responsible for storing pending jobs and determining
// which one should be dispatched next. Different implementations
// (such as FIFO or priority-based) define their own ordering logic.
//
// The scheduler goroutine interacts only through this interface,
// making it easy to plug in alternative queueing strategies.
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

	// Tick updates internal state periodically.
	//
	// Priority-based queues use it to apply *aging* (increasing
	// effective priority with time), while FIFO queues typically
	// implement it as a no-op.
	Tick(now time.Time)

	// Len returns the current number of jobs waiting in the queue.
	//
	// The scheduler uses this to update runtime metrics.
	Len() int

	// MaxAge reports the maximum waiting time among queued jobs.
	//
	// This metric helps track fairness and queue health. For FIFO
	// queues or strategies that do not track age, it can safely
	// return zero.
	MaxAge() time.Duration
}

func (p *Pool[T,M]) makeQueue() schedQueue[T] {
	switch p.opts.QT {
	case Fifo:
		return NewFifoQueue[T](initialFifoCapacity)
	case Conditional:
		// for now fall back to FIFO
		return NewFifoQueue[T](initialFifoCapacity)
	case BucketQueue:
		return NewBucketQueue[T](p.opts.AgingRate, p.opts.QueueSize)	
	case ShardedQueue:
		return newShardedBucketQueue[T](defaultShardNum,p.opts.AgingRate, initialBucketSize)

	default:
		return NewFifoQueue[T](initialFifoCapacity)

	}
}

