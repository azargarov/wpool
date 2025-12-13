package workerpool

//go:inline

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	DefaultMaxWorkers   = 10
)

type JobFunc[T any] func(T) error

type Job[T any] struct {
	Payload T

	Fn JobFunc[T]

	Ctx context.Context

	CleanupFunc func()

}

type Pool[T any] struct {
	stopOnce     sync.Once
	closed       chan struct{} // signals no more submissions
	opts         Options
	stopCh       chan struct{}
	doneCh       chan struct{}
	metricsMu    sync.Mutex
	metrics      Metrics
	queue schedQueue[T]
	sem chan struct{}  
}

func (p *Pool[T]) Shutdown(ctx context.Context) error {
	p.stopOnce.Do(func() {
		close(p.stopCh)
		close(p.closed)
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

	select {
	case <-job.Ctx.Done():
		return job.Ctx.Err()
	case <-p.closed:
		return fmt.Errorf("workerpool: pool closed")
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
		case <-p.stopCh:
			return

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
		}
	}
}

func (p *Pool[T]) batchWorker(id int) {
	defer p.SetWorkerState(id, false)
	for {
		select {
		case <-p.stopCh:
			return
		case <-p.sem:
			p.processJob__()
		}
	}
}

