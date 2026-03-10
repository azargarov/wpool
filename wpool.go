package workerpool

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	//"os"
)

const (
	// defaultPushBatch is the minimum number of pending jobs
	// required before a worker wake-up is triggered eagerly.
	// Smaller values reduce latency, larger values improve batching.
	defaultWakeMinJobs   = 16

	// batchTimerInterval is the periodic interval used by the batch timer
	// to ensure progress even if no new submissions arrive.
	defaultFlushInterval = 50 * time.Microsecond
)

var (
	// ErrClosed is returned when submitting a job to a pool
	// that has already been shut down.
	ErrClosed    = errors.New("workerpool: pool is closed")
)


// ErrorHandler is a user-provided callback invoked on internal
// or job-level errors.
type ErrorHandler func(e error)

// WakeupWorker is a lightweight signal channel used to wake
// an idle worker.
type WakeupWorker chan struct{}

type timerHot struct {
    lastDrainNano atomic.Int64
}

type drainHot struct {
    batchInFlight atomic.Bool
}

type submitHot struct {
    pendingJobs atomic.Int64
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
	//pendingJobs   atomic.Int64
	submit 	submitHot
	// batchInFlight ensures only one batch drain runs at a time.
	//batchInFlight atomic.Bool
	drain drainHot

	// lastDrainNano tracks the last successful batch drain timestamp.
	timer timerHot

	wgWorkers   sync.WaitGroup

	workersDone chan struct{}

	// OnInternaError is called when the pool encounters
	// an unexpected internal error.
	OnInternalError ErrorHandler

	// OnJobError is called when a job function returns an error.	
	OnJobError     ErrorHandler
}

func (p *Pool[T, M]) StatSnapshot() string{
	return p.queue.StatSnapshot()
}

// GetIdleLen returns the number of currently idle workers.
func (p *Pool[T, M]) GetIdleLen() int64 {
	return int64(len(p.idleWorkers))
}

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
    switch opts.QT {
    case SegmentedQueue:
        p.queue = NewSegmentedQ[T](opts, nil)
    case RevolvingBucketQueue:
        p.queue = NewRevolvingBucketQ[T](opts)
    }
	p.metrics = metrics
	p.wakes = make([]WakeupWorker, opts.Workers)
	for i := 0; i < opts.Workers; i++ {
		p.wakes[i] = make(WakeupWorker, 1)
	}

	// Start workers.
	for i := 0; i < opts.Workers; i++ {
		p.wgWorkers.Add(1)
		p.setWorkerState(i, true)
		go func(id int) {
			p.batchWorker(id, &p.wgWorkers)
		}(i)
	}

	// Track worker completion.
	p.workersDone = make(chan struct{})
	go func() {
		p.wgWorkers.Wait()
		close(p.workersDone)
	}()

	p.timer.lastDrainNano.Store(time.Now().UnixNano())
	p.drain.batchInFlight.Store(false)
	// Start periodic batch timer.
	go p.batchTimer()

	return p
}

func (p *Pool[T, M]) Shutdown(ctx context.Context) error {
	p.stopOnce.Do(func() {
		p.shutdown.Store(true)
		close(p.doneCh)
		p.queue.Close()
	})

	select {
	case <-p.workersDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Pool[T, M]) Stop() { _ = p.Shutdown(context.Background()) }


func (p *Pool[T, M]) Submit(job Job[T]) error {
	if p.shutdown.Load() {
		return ErrClosed
	}

	if job.Fn == nil {
		return ErrNilFunc
	}

	meta := job.Meta
	if meta != nil && meta.Ctx !=nil{
		select {
		case <-meta.Ctx.Done():
			return meta.Ctx.Err()
		default:
		}
	}

	pj := p.submit.pendingJobs.Add(1)
	p.metrics.IncQueued()

	if err := p.queue.Push(job); err != nil {
	    p.metrics.BatchDecQueued(1)
	    newPending := p.submit.pendingJobs.Add(-1)
	    if newPending < 0 {
	        p.submit.pendingJobs.Store(0)
	    }
	    return err
	}

	if pj >= p.opts.WakeMinJobs {  
			p.drain.batchInFlight.Store(true)
				select {
				case w := <-p.idleWorkers:
					w <- struct{}{}
				default:
					p.drain.batchInFlight.Store(false)
				}
			
	}
	return nil
}

func (p *Pool[T, M]) batchWorker(id int, wg *sync.WaitGroup) {
    defer func() {
        p.drain.batchInFlight.Store(false)
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

    select {
    case p.idleWorkers <- wake:
    case <-p.doneCh:
        return
    }

    for {
        select {
        case <-wake:
        case <-p.doneCh:
            return
        }

        for {
            if p.shutdown.Load() {
                p.drain.batchInFlight.Store(false)
                return
            }

            jobsNum := p.batchProcessJob()

            if jobsNum > 0 {
                p.metrics.BatchDecQueued(jobsNum)
				newPending := p.submit.pendingJobs.Add(-int64(jobsNum))
                if newPending < 0 {
                    p.submit.pendingJobs.Store(0)
                }
                p.timer.lastDrainNano.Store(time.Now().UnixNano())
            }

            p.drain.batchInFlight.Store(false)

			if p.submit.pendingJobs.Load() > 0 {
			    if p.drain.batchInFlight.CompareAndSwap(false, true) {
			        continue
			    }
			}
			
            if p.shutdown.Load() {
                return
            }

            if p.submit.pendingJobs.Load() == 0 {
                break
            }

            if !p.drain.batchInFlight.CompareAndSwap(false, true) {
                break
            }
        }

        select {
        case p.idleWorkers <- wake:
        case <-p.doneCh:
            return
        }
    }
}

func (p *Pool[T, M]) batchTimer() {
	t := time.NewTicker(p.opts.FlushInterval)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			if p.shutdown.Load() {
				return
			}
			if p.submit.pendingJobs.Load() == 0 {
				continue
			}
			if time.Since(time.Unix(0, p.timer.lastDrainNano.Load())) < p.opts.FlushInterval {
				continue
			}

			if !p.drain.batchInFlight.CompareAndSwap(false, true) {
				continue
			}

			select {
			case w := <-p.idleWorkers:
				select {
				case w <- struct{}{}:
				default:
				}
			default:
				p.drain.batchInFlight.Store(false)
			}

		case <-p.doneCh:
			return
		}
	}
}

func (p *Pool[T, M]) Metrics() *M {
	p.metricsMu.Lock()
	defer p.metricsMu.Unlock()
	return &p.metrics
}

func (p *Pool[T, M])MetricsStr() string{
	return p.metrics.String()  + fmt.Sprintf(", Penidng jobs: %d", p.submit.pendingJobs.Load()) + 
	fmt.Sprintf(", idle workers : %d", p.GetIdleLen()) + fmt.Sprintf(", active workers: %d", p.ActiveWorkers())
}

func (p *Pool[T, M])DebugHead() string{
	return p.queue.DebugHead()
}

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
