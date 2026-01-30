package workerpool

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultPushBatch   = 512
	batchTimerInterval = 50 * time.Microsecond
)

type JobFunc[T any] func(T) error

type WakeupWorker chan struct{}

type Job[T any] struct {
	Payload     T
	Fn          JobFunc[T]
	Ctx         context.Context
	CleanupFunc func()
}

type Pool[T any, M MetricsPolicy] struct {
	stopOnce      sync.Once
	opts          Options
	shutdown      atomic.Bool
	doneCh        chan struct{}
	metricsMu     sync.Mutex
	metrics       M
	queue         schedQueue[T]
	wakes         []WakeupWorker
	idleWorkers   chan WakeupWorker
	workersActive []atomic.Bool

	pendingJobs   atomic.Int64 
	batchInFlight atomic.Bool  
	lastDrainNano atomic.Int64 

}

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

	for i := 0; i < opts.Workers; i++ {
		go func(id int) {
			p.batchWorker(id)
		}(i)
		p.SetWorkerState(i, true)
	}

	p.lastDrainNano.Store(time.Now().UnixNano())
	go p.batchTimer()

	return p
}

func (p *Pool[T, M]) Shutdown(ctx context.Context) error {
	p.stopOnce.Do(func() {
		p.shutdown.Store(true)
		close(p.doneCh)

	})

	for {
		if p.ActiveWorkers() == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			runtime.Gosched()
		}
	}
}

func (p *Pool[T, M]) Shutdown_(ctx context.Context) error {
    p.stopOnce.Do(func() {
        p.shutdown.Store(true)
    })


    for {
        if p.ActiveWorkers() == 0 {
            close(p.doneCh) 
            return nil
        }
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
            runtime.Gosched()
        }
    }
}

func (p *Pool[T, M]) Stop() { _ = p.Shutdown(context.Background()) }

func (p *Pool[T, M]) Submit(job Job[T], basePrio int) error {
	if job.Ctx == nil {
		job.Ctx = context.Background()
	}

	if p.shutdown.Load() {
		return fmt.Errorf("workerpool: pool closed")
	}

	select {
	case <-job.Ctx.Done():
		return job.Ctx.Err()
	default:
	}

	ok := p.queue.Push(job)
	if !ok {
		return errors.New("queue is full")
	}
	p.metrics.IncQueued()

	pj := p.pendingJobs.Add(1)

	if pj < defaultPushBatch {
		return nil
	}

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

func (p *Pool[T, M]) batchWorker(id int) {

	defer func() {
        p.batchInFlight.Store(false) 
        p.SetWorkerState(id, false)
    }()

	if p.opts.PinWorkers {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		if err := PinToCPU(id % runtime.NumCPU()); err != nil {
			// TODO: log or panic 
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
	//defer p.batchInFlight.Store(false)

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


// Metrics returns a copy of the current metrics snapshot.
func (p *Pool[T, M]) Metrics() *M {
	p.metricsMu.Lock()
	defer p.metricsMu.Unlock()
	return &p.metrics
}

func (p *Pool[T, M]) SetWorkerState(id int, state bool) {
	p.workersActive[id].Store(state)
}

// ActiveWorkers counts how many workers are currently marked active.
func (p *Pool[T, M]) ActiveWorkers() int {
	count := 0
	for i := range p.workersActive {
		if p.workersActive[i].Load() {
			count++
		}
	}
	return count
}

func (p *Pool[T, M])GetQeueu()schedQueue[T]{
	return p.queue
}
