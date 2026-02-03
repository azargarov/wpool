package workerpool

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// defaultPushBatch is the minimum number of pending jobs
	// required before a worker wake-up is triggered eagerly.
	// Smaller values reduce latency, larger values improve batching.
	defaultPushBatch   = 64

	// batchTimerInterval is the periodic interval used by the batch timer
	// to ensure progress even if no new submissions arrive.
	batchTimerInterval = 50 * time.Microsecond
)

var (
	// ErrClosed is returned when submitting a job to a pool
	// that has already been shut down.
	ErrClosed    = errors.New("workerpool: pool is closed")

	// ErrQueueFull is returned when the underlying queue
	// cannot accept more jobs.
	ErrQueueFull = errors.New("workerpool: queue is full")

	// ErrNilFunc is returned when a submitted Job has a nil Fn.
	ErrNilFunc   = errors.New("workerpool: job func is nil")
)
// JobFunc is the function executed by a worker for a given job payload.
type JobFunc[T any] func(T) error

// ErrorHandler is a user-provided callback invoked on internal
// or job-level errors.
type ErrorHandler func(e error)

// WakeupWorker is a lightweight signal channel used to wake
// an idle worker.
type WakeupWorker chan struct{}

// Job represents a single unit of work submitted to the pool.
//
// Payload is passed to Fn when executed.
// Ctx controls cancellation before execution.
// CleanupFunc, if set, is executed after job completion.
type Job[T any] struct {
	Payload     T
	Fn          JobFunc[T]
	Ctx         context.Context
	CleanupFunc func()
}

// Pool is a high-performance worker pool with batched scheduling.
//
// It combines:
//   - a lock-free / low-contention queue
//   - explicit worker wake-ups
//   - batching to amortize scheduling overhead
//
// The pool is safe for concurrent use.
type Pool[T any, M MetricsPolicy] struct {
	stopOnce      sync.Once
	opts          Options
	shutdown      atomic.Bool
	doneCh        chan struct{}
	metricsMu     sync.Mutex
	metrics       M
	queue         schedQueue[T]

	// wakes is a per-worker wake-up channel.
	wakes         []WakeupWorker

	// idleWorkers tracks workers ready to accept work.	
	idleWorkers   chan WakeupWorker

	// workersActive marks whether a worker is currently alive.
	workersActive []atomic.Bool

	// pendingJobs is the global count of queued but unprocessed jobs.
	pendingJobs   atomic.Int64

	// batchInFlight ensures only one batch drain runs at a time.
	batchInFlight atomic.Bool

	// lastDrainNano tracks the last successful batch drain timestamp.
	lastDrainNano atomic.Int64

	wgWorkers   sync.WaitGroup
	workersDone chan struct{}

	// OnInternaError is called when the pool encounters
	// an unexpected internal error.
	OnInternaError ErrorHandler

	// OnJobError is called when a job function returns an error.	
	OnJobError     ErrorHandler
}

// GetIdleLen returns the number of currently idle workers.
func (p *Pool[T, M]) GetIdleLen() int64 {
	return int64(len(p.idleWorkers))
}

// NewPool creates a new Pool using the provided metrics implementation
// and optional configuration options.
func NewPool[M MetricsPolicy, T any](metrics M, opts ...Option) *Pool[T, M] {
	o := Options{}
	for _, opt := range opts {
		opt(&o)
	}
	o.FillDefaults()

	return NewPoolFromOptions[M, T](metrics, o)
}

// NewPoolFromOptions creates a new Pool from a fully specified Options struct.
func NewPoolFromOptions[M MetricsPolicy, T any](metrics M, opts Options) *Pool[T, M] {
	opts.FillDefaults()

	p := &Pool[T, M]{
		opts:          opts,
		doneCh:        make(chan struct{}),
		idleWorkers:   make(chan WakeupWorker, opts.Workers),
		workersActive: make([]atomic.Bool, opts.Workers),
	}
	p.queue = p.makeQueue()
	p.metrics = metrics
	p.wakes = make([]WakeupWorker, opts.Workers)
	for i := 0; i < opts.Workers; i++ {
		p.wakes[i] = make(WakeupWorker, 1)
	}

	// Start workers.
	for i := 0; i < opts.Workers; i++ {
		go func(id int) {
			p.batchWorker(id, &p.wgWorkers)
		}(i)
		p.wgWorkers.Add(1)
		p.setWorkerState(i, true)
	}

	// Track worker completion.
	p.workersDone = make(chan struct{})
	go func() {
		p.wgWorkers.Wait()
		close(p.workersDone)
	}()

	p.lastDrainNano.Store(time.Now().UnixNano())

	// Start periodic batch timer.
	go p.batchTimer()

	return p
}

// Shutdown gracefully stops the pool, waiting for workers
// to finish or until the provided context is canceled.
func (p *Pool[T, M]) Shutdown(ctx context.Context) error {
	p.stopOnce.Do(func() {
		p.shutdown.Store(true)
		close(p.doneCh)
	})

	select {
	case <-p.workersDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stop shuts down the pool using a background context.
func (p *Pool[T, M]) Stop() { _ = p.Shutdown(context.Background()) }

// Submit enqueues a job for execution.
//
// It may trigger a worker wake-up depending on batching state.
// Submit is non-blocking and safe for concurrent use.
func (p *Pool[T, M]) Submit(job Job[T], basePrio int) error {
	if p.shutdown.Load() {
		return ErrClosed
	}

	if job.Fn == nil {
		return ErrNilFunc
	}

	if job.Ctx == nil {
		job.Ctx = context.Background()
	}

	select {
	case <-job.Ctx.Done():
		return job.Ctx.Err()
	default:
	}

	ok := p.queue.Push(job)
	if !ok {
		return ErrQueueFull
	}
	p.metrics.IncQueued()

	pj := p.pendingJobs.Add(1)

	// Delay wake-up until batch threshold is reached.
	if pj < defaultPushBatch {
		return nil
	}

	// Attempt to trigger a batch drain.
	if p.batchInFlight.CompareAndSwap(false, true) {
		select {
		case w := <-p.idleWorkers:
			w <- struct{}{}
			p.lastDrainNano.Store(time.Now().UnixNano())
		default:
			p.batchInFlight.Store(false)
		}
	}
	return nil
}

func (p *Pool[T, M]) batchWorker(id int, wg *sync.WaitGroup) {

	defer func() {
		p.batchInFlight.Store(false)
		p.setWorkerState(id, false)
		wg.Done()
	}()

	if p.opts.PinWorkers {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		if err := PinToCPU(id % runtime.NumCPU()); err != nil {
			p.reportInternalError(err)
		}
	}

	wake := p.wakes[id]

	// initial publish
	select {
	case p.idleWorkers <- wake:
	case <-p.doneCh:
		return
	}

	for {
		select {
		case <-wake:
		case <-p.doneCh:
			p.batchInFlight.Store(false)
			return
		}

		if p.shutdown.Load() {
			p.batchInFlight.Store(false)
			return
		}
		deQueued := p.batchProcessJob()
		p.metrics.BatchDecQueued(deQueued)
		new := p.pendingJobs.Add(-int64(deQueued))
		if new < 0 {
			p.pendingJobs.Store(0)
		}
		p.batchInFlight.Store(false)
		p.lastDrainNano.Store(time.Now().UnixNano())

		if p.shutdown.Load() {
			return
		}
		select {
		case p.idleWorkers <- wake:
		case <-p.doneCh:
			return
		default:
			//TODO: timer/submit can still find other workers but if handle it might reduce wake efficiency
		}
	}
}

func (p *Pool[T, M]) batchTimer() {
	t := time.NewTicker(batchTimerInterval)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			if p.shutdown.Load() {
				return
			}
			if p.pendingJobs.Load() == 0 {
				continue
			}
			if time.Since(time.Unix(0, p.lastDrainNano.Load())) < batchTimerInterval {
				continue
			}
			if p.batchInFlight.CompareAndSwap(false, true) {
				select {
				case w := <-p.idleWorkers:
					w <- struct{}{}
				default:
					p.batchInFlight.Store(false)
				}
			}
		case <-p.doneCh:
			return
		}
	}
}

// Metrics returns a snapshot of the current pool metrics.
//
// The returned value should be treated as read-only.
// Metrics collection is implementation-defined by the MetricsPolicy.
func (p *Pool[T, M]) Metrics() *M {
	p.metricsMu.Lock()
	defer p.metricsMu.Unlock()
	return &p.metrics
}


// ActiveWorkers returns the number of workers currently marked as active.
//
// A worker is considered active if it has been started and not yet exited.
// This does not necessarily mean the worker is currently executing a job.
func (p *Pool[T, M]) ActiveWorkers() int {
	count := 0
	for i := range p.workersActive {
		if p.workersActive[i].Load() {
			count++
		}
	}
	return count
}

func (p *Pool[T, M]) setWorkerState(id int, state bool) {
	p.workersActive[id].Store(state)
}

//func (p *Pool[T, M]) getQueue() schedQueue[T] {
//	return p.queue
//}
