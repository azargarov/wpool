package workerpool

import (
	"time"
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
}

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

	p.metrics.active = make(map[int]bool, opts.Workers)

	for i := 0; i < opts.Workers; i++ {
		p.wg.Add(1)
		go func(id int) {
			defer p.wg.Done()
			p.worker(id)
		}(i)
		p.SetWorkerState(i, true)
	}

	// scheduler
	go p.scheduler()

	return p
}
