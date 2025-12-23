package workerpool

//go:inline

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
	DefaultMaxWorkers   = 10
	defaultBatch		= 512
	batchTimerInterval  = 50*time.Microsecond
)

type JobFunc[T any] func(T) error

type WakeupWorker chan struct{}

type Job[T any] struct {
	Payload T

	Fn JobFunc[T]

	Ctx context.Context

	CleanupFunc func()

}

type Pool[T any] struct {
	stopOnce     sync.Once
	opts         Options
	shutdown	 atomic.Bool
	doneCh       chan struct{}
	metricsMu    sync.Mutex
	metrics      Metrics
	queue 		 schedQueue[T]
	wakes 		 []WakeupWorker
	idleWorkers  chan WakeupWorker

	pendingJobs   atomic.Int64  // scheduling counter, not metrics
	batchInFlight atomic.Bool   // only one wake trigger at a time
	lastDrainNano atomic.Int64  // for timer (optional but recommended)

}

func (p * Pool[T])GetIdleLen()int64{
	return int64(len(p.idleWorkers))
}

func NewPool[T any](opts Options) *Pool[T]{ 
	opts.FillDefaults()

	p := &Pool[T]{
		opts:         opts,
		doneCh:       make(chan struct{}),
		idleWorkers: make(chan WakeupWorker, opts.Workers),
	}
	p.queue = p.makeQueue()
	p.metrics.workersActive = make([]atomic.Bool, opts.Workers)
	
	p.wakes = make([]WakeupWorker, opts.Workers)
	for i := 0; i < opts.Workers; i++ {
	    p.wakes[i] = make(WakeupWorker,1)
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

func (p *Pool[T]) Shutdown(ctx context.Context) error {
	p.stopOnce.Do(func() {
		p.shutdown.Store(true)
		close(p.doneCh)

    	for _, w := range p.wakes {
    	    if w != nil {
    	        close(w)
    	    }
    	}
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

func (p *Pool[T]) Stop() { _ = p.Shutdown(context.Background()) }

func (p *Pool[T]) Submit(job Job[T], basePrio int) error {
	if job.Ctx == nil {
		job.Ctx = context.Background()
	}

	if p.shutdown.Load(){
		return fmt.Errorf("workerpool: pool closed")
	}

	select {
	case <-job.Ctx.Done():
		return job.Ctx.Err()
	default:
	}

	ok := p.queue.Push(job, basePrio, time.Now())
	if ! ok {
		return errors.New("queue is full")
	}
	p.incQueued()

	pj := p.pendingJobs.Add(1)
	
	if pj < defaultBatch {
	    return nil
	}

	if p.batchInFlight.CompareAndSwap(false, true) {
	    select {
	    case w := <-p.idleWorkers:
	        w <- struct{}{}
	        p.lastDrainNano.Store(time.Now().UnixNano()) // 👈 reset signal
	    default:
	        p.batchInFlight.Store(false)
	    }
	}
	return nil
}

func (p *Pool[T, M]) batchWorker(id int) {
	if p.opts.PinWorkers{
		PinToCPU(id % runtime.NumCPU())
	}
	defer p.SetWorkerState(id, false)

	wake := p.wakes[id]
	p.idleWorkers <- wake

	for {
		_, ok := <-wake
		if !ok || p.shutdown.Load() {
			return
		}

		deQueued := p.batchProcessJob()
        p.batchDecQueued(deQueued)
		new := p.pendingJobs.Add(-int64(deQueued))
		if new < 0 {
		    p.pendingJobs.Store(0)
		}
		p.batchInFlight.Store(false)
		p.lastDrainNano.Store(time.Now().UnixNano())

		if p.shutdown.Load() {
		    return
		}
		p.idleWorkers <- wake

	}
}

func (p *Pool[T]) batchTimer() {
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

// SetWorkerState marks worker id as active/inactive. It is called internally
// by the pool when workers start or stop.
func (p *Pool[T]) SetWorkerState(id int, state bool) {
	p.metrics.workersActive[id].Store(state)
}