package workerpool

import (
	"sync/atomic"
	"time"
)

// QueueType defines the scheduling strategy used by the worker pool.
//
// Different queue types determine how jobs are ordered and selected
// for execution by the scheduler. The type is configured via
// Options.QueueType when creating a new Pool.
type QueueType int

const (
	// Fifo represents a simple first-in–first-out queue.
	// Jobs are executed strictly in the order they are submitted.
	Fifo QueueType = iota

	// Priority represents a priority queue that orders jobs
	// by their effective priority, which increases over time
	// according to the configured aging rate.
	Priority

	// Conditional is a placeholder for future scheduling strategies
	// where dispatching logic may depend on job state or custom
	// conditions. Currently behaves the same as Fifo.
	Conditional

	// BucketQueue selects the fixed-range, O(1) bucket-based priority queue.
	// Jobs are assigned to one of 100 priority buckets based on:
	//
	//     effective = basePriority - agingRate * seq
	//
	// where seq is a monotonically increasing insertion counter.
	// This scheduler offers extremely fast push/pop, predictable behavior,
	// and static aging without periodic rebalancing.
	BucketQueue
)

// Options configure a worker Pool.
//
// All zero values are replaced with sensible defaults in fillDefaults.
type Options struct {
	// Workers is the number of worker goroutines processing jobs.
	Workers int

	// AgingRate controls how fast queued jobs grow in effective priority.
	// Higher values mean old jobs will be pulled to the top sooner.
	AgingRate float64

	// RebuildDur is how often the scheduler re-computes effective priorities
	// for all queued jobs.
	RebuildDur time.Duration

	// QueueSize is the capacity of the submit channel — how many jobs can
	// wait in front of the scheduler.
	QueueSize int

	// QT selects the scheduler queue type (FIFO, Priority, or Conditional).
	//
	// Use Fifo for simple first-in–first-out execution,
	// Priority to enable aging-based priority scheduling,
	// or Conditional for custom or experimental logic.
	QT QueueType
}

// FillDefaults populates missing or zero-valued fields in Options
// with sensible defaults.
//
// This method is automatically called by NewPool before creating
// the worker pool. It ensures that all parameters have valid values
// even if the caller provides only partial configuration.
//
// Default values:
//   - Workers:     4
//   - AgingRate:   0.3
//   - RebuildDur:  200ms
//   - QueueSize:   256
//   - QT:          Fifo
func (o *Options) FillDefaults() {
	if o.Workers <= 0 {
		o.Workers = 4
	}
	if o.AgingRate <= 0 {
		o.AgingRate = 0.3
	}
	if o.RebuildDur <= 0 {
		o.RebuildDur = 200 * time.Millisecond
	}
	if o.QueueSize <= 0 {
		o.QueueSize = 256
	}
	if o.QT == 0 {
		o.QT = Fifo
	}
}

// String returns a human-readable name for the queue type.
// It is used in logs, metrics, and test output. Unknown values
// return "Unknown".
func (qt QueueType) String() string {
	switch qt {
	case Fifo:
		return "Fifo"
	case Priority:
		return "Priority"
	case Conditional:
		return "Conditional"
	case BucketQueue:
		return "BucketQueue"
	default:
		return "Unknown"
	}
}

// NewPool creates and starts a new Pool with the given options and a default
// retry policy. Zero values in defaultRetry are filled with built-in defaults.
func NewPool[T any](opts Options, defaultRetry RetryPolicy) *Pool[T] {
	opts.FillDefaults()

	if defaultRetry.Attempts <= 0 {
		defaultRetry.Attempts = defaultAttempts
	}
	if defaultRetry.Initial <= 0 {
		defaultRetry.Initial = defaultInitialRetry
	}
	if defaultRetry.Max <= 0 {
		defaultRetry.Max = defauiltMaxRetry
	}

	p := &Pool[T]{
		opts:         opts,
		submitCh:     make(chan submitReq[T], opts.QueueSize),
		workCh:       make(chan Job[T]),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
		closed:       make(chan struct{}),
		defaultRetry: defaultRetry,
	}
	p.metrics.workersActive = make([]atomic.Bool, opts.Workers)

	for i := 0; i < opts.Workers; i++ {
		go func(id int) {
			p.worker(id)
		}(i)
		p.SetWorkerState(i, true)
	}

	go p.scheduler()

	return p
}
