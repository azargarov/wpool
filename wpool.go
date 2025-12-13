package workerpool

//go:inline

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultMaxWorkers   = 10
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

	idleWorkers  chan WakeupWorker

	sem chan struct{}  
}

func NewPool[T any](opts Options) *Pool[T]{ 
	opts.FillDefaults()


	p := &Pool[T]{
		opts:         opts,
		doneCh:       make(chan struct{}),
		sem:    make(chan struct{}, opts.Workers*2),
		idleWorkers: make(chan WakeupWorker, opts.Workers),
	}
	p.queue = p.makeQueue()
	p.metrics.workersActive = make([]atomic.Bool, opts.Workers)
	
	var startWorker func(int)

	if opts.PT == SerialPop{
		startWorker = p.worker
	}else{
		startWorker = p.batchWorker
	}

	for i := 0; i < opts.Workers; i++ {
		go func(id int) {
			startWorker(id)
		}(i)
		p.SetWorkerState(i, true)
	}

	return p
}

func (p *Pool[T]) Shutdown(ctx context.Context) error {
	p.stopOnce.Do(func() {
		p.shutdown.Store(true)
		close(p.sem)
	})

	// wait for workers
	doneWorkers := make(chan struct{})
	go func() {
		for p.ActiveWorkers() != 0 {
			time.Sleep(0)
		}
		close(doneWorkers)
	}()

	select {
	case <-doneWorkers:
		return nil
	case <-ctx.Done():
		return ctx.Err()
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

	p.queue.Push(job, basePrio, time.Now())

	p.sem <- struct{}{}
	
	p.metrics.submitted.Add(1)
	p.incSubmitted()
	p.incQueued()
	return nil
}

func (p *Pool[T]) worker(id int) {
	defer p.SetWorkerState(id, false)

	for {
		select {

		case <-p.sem:
			for {
				job, ok := p.queue.Pop(time.Now())
				if !ok {break}

				func(j Job[T]) {
					defer func() {
						if r := recover(); r != nil {
							// TODO: log panic r
						}
						if j.CleanupFunc != nil {
							j.CleanupFunc()
						}
						p.metrics.executed.Add(1)
					}()
					_ = j.Fn(j.Payload)
				}(job)
			}
		default:
		}
	}
}

func (p *Pool[T]) batchWorker(id int) {
    defer p.SetWorkerState(id, false)

    for {
        _, ok := <-p.sem
        if !ok { 
            return
        }
        p.batchProcessJob()
    }
}

