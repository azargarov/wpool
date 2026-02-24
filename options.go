package workerpool

import (
	"runtime"
	"time"
)

// QueueType defines the scheduling strategy used by the worker pool.
//
// Different queue types determine how jobs are ordered, grouped,
// and selected for execution by the scheduler.
type QueueType int

// Option configures a worker Pool.
type Option func(*Options)

const (
	// SegmentedQueue uses a lock-free segmented FIFO queue.
	//
	// It is optimized for high-throughput workloads with many producers
	// and batch-oriented consumption.
	SegmentedQueue QueueType = iota

	RevolvingBucketQueue	
)

// Options configure the behavior of a worker Pool.
//
// Any zero-value fields are replaced with sensible defaults
// when FillDefaults is called.
type Options struct {
	// Workers is the number of worker goroutines.
	//
	// Defaults to runtime.GOMAXPROCS(0).
	Workers int

	// SegmentSize is the number of jobs stored in a single queue segment.
	//
	// Larger values reduce segment churn but increase batch scan cost.
	SegmentSize uint32

	// SegmentCount is the number of queue segments preallocated on startup.
	//
	// Increasing this value reduces allocations under load at the cost
	// of higher baseline memory usage.
	SegmentCount uint32

	// PoolCapacity limits the number of reusable segments kept
	// in the internal segment pool.
	//
	// Larger values trade memory for fewer allocations.
	PoolCapacity uint32

	// QT selects the scheduler queue implementation.
	QT QueueType

	// PinWorkers enables CPU pinning for worker goroutines.
	//
	// When enabled, workers may be locked to OS threads to reduce
	// migration and improve cache locality.
	PinWorkers bool

	WakeMinJobs uint

	FlushInterval time.Duration
}

// WithWorkers sets the number of worker goroutines.
func WithWorkers(n int) Option {
	return func(o *Options) {
		o.Workers = n
	}
}

// WithSegmentSize sets the queue segment size.
func WithSegmentSize(n uint32) Option {
	return func(o *Options) {
		o.SegmentSize = n
	}
}

// WithSegmentCount sets the queue initial segment count.
func WithSegmentCount(n int) Option {
	return func(o *Options) {
		o.SegmentCount = uint32(n)
	}
}

// WithPinnedWorkers enables CPU pinning for workers.
func WithPinnedWorkers(enabled bool) Option {
	return func(o *Options) {
		o.PinWorkers = enabled
	}
}

// WithQT sets the scheduler queue type.
func WithQT(qt QueueType) Option {
	return func(o *Options) {
		o.QT = qt
	}
}

func WithWakeMinJobs(wj uint) Option {
	return func(o *Options) {
		o.WakeMinJobs = wj
	}
}

func WithFlushInterval(t time.Duration) Option {
	return func(o *Options) {
		o.FlushInterval = t
	}
}

// FillDefaults replaces zero-value fields with default settings.
//
// It is called internally by the Pool constructor and may also be
// used by callers who construct Options manually.
func (o *Options) FillDefaults() {
	if o.Workers <= 0 {
		o.Workers = runtime.GOMAXPROCS(0)
	}
	if o.QT == 0 {
		o.QT = SegmentedQueue
	}
	if o.SegmentSize <= 0 {
		o.SegmentSize = DefaultSegmentSize
	}
	if o.SegmentCount <= 0 {
		o.SegmentCount = DefaultSegmentCount
	}
	if o.WakeMinJobs == 0 {
    	o.WakeMinJobs= defaultWakeMinJobs
	}
		
	if o.FlushInterval == 0 {
    	o.FlushInterval= defaultFlushInterval
	}
}

// String returns the human-readable name of the queue type.
func (qt QueueType) String() string {
	switch qt {
	case SegmentedQueue:
		return "SegmentedQueue"
	default:
		return "Unknown"
	}
}

// Compile-time anchor for all QueueType values.
// Ensures new queue types are explicitly referenced and handled.
var _ = []QueueType{
	SegmentedQueue,
}
