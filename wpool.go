package workerpool

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	// DefaultMaxWorkers is kept for backwards compatibility with earlier versions
	// of the pool that accepted just the worker count. New code should use Options.
	DefaultMaxWorkers   = 10
	defaultAttempts     = 3
	defaultInitialRetry = 200 * time.Millisecond
	defauiltMaxRetry    = 5 * time.Second
)

// JobFunc is the function type executed by the pool.
// The argument is the job payload, the returned error is used for retries.
type JobFunc[T any] func(T) error

// Job describes a single unit of work to be executed by the pool.
type Job[T any] struct {
	// Payload is passed to Fn.
	Payload T

	// Fn is the actual job logic. It may be called multiple times if retries are enable
	Fn JobFunc[T]

	// Ctx controls the lifetime of the job and its backoff sleeps.
	// If nil, context.Background() is used.
	Ctx context.Context

	// CleanupFunc is called once after the job finishes (even if it panicked).
	// Use it to release resources tied to the job.
	CleanupFunc func()

	// Retry optionally overrides the pool's default retry policy.
	// Zero values in this struct are ignored.
	Retry *RetryPolicy
}

// Pool is a bounded worker pool with a priority scheduler in front of it.
// Jobs are first submitted to the scheduler and then dispatched to workers.
type Pool[T any] struct {
	stopOnce     sync.Once
	closed       chan struct{} // signals no more submissions
	defaultRetry RetryPolicy
	opts         Options
	submitCh     chan submitReq[T]
	workCh       chan Job[T]
	stopCh       chan struct{}
	doneCh       chan struct{}
	metricsMu    sync.Mutex
	metrics      Metrics
}

// Shutdown stops accepting new jobs, drains the internal priority queue,
// waits for the scheduler to finish dispatching, and then waits for all
// workers up to ctx deadline.
//
// If the context expires before workers finish, context error is returned.
func (p *Pool[T]) Shutdown(ctx context.Context) error {
	p.stopOnce.Do(func() {
		// tell scheduler to stop
		p.stopCh <- struct{}{}
		// also block new submits
		close(p.closed)
	})

	// wait for scheduler to say "I'm done dispatching"
	doneSched := make(chan struct{})
	go func() {
		<-p.doneCh // scheduler closes this at the end
		close(doneSched)
	}()

	select {
	case <-doneSched:
	case <-ctx.Done():
		return ctx.Err()
	}

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

// Stop is a convenience around Shutdown(context.Background()).
// It blocks until all jobs are processed.
func (p *Pool[T]) Stop() { _ = p.Shutdown(context.Background()) }

// Submit enqueues a job into the scheduler with the given basePrio.
// Higher basePrio means "run sooner". The scheduler will still age jobs
// over time so low-priority jobs eventually run.
//
// Submit returns an error if the pool has been shut down or if the job
// context was already canceled.
func (p *Pool[T]) Submit(job Job[T], basePrio float64) error {
	ctx := job.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	select {
	case <-p.closed:
		return fmt.Errorf("workerpool: pool closed")
	default:
	}

	req := submitReq[T]{
		job:      job,
		basePrio: basePrio,
	}

	// if already canceled -> return ctx.Err()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	select {
	case p.submitCh <- req:
		p.incSubmitted()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// worker runs in a goroutine started by NewPool and executes jobs
// dispatched by the scheduler.
func (p *Pool[T]) worker(id int) {
	for job := range p.workCh {
		if job.Ctx != nil {
			select {
			case <-job.Ctx.Done():
				p.incExecuted()
				continue
			default:
			}
		}

		//p.wg.Add(1)
		func() {
			defer func() {
				if r := recover(); r != nil {
					// TODO: log recover
				}
				if job.CleanupFunc != nil {
					job.CleanupFunc()
				}
			}()
			p.processJob(job)
		}()
		//p.wg.Done()
		p.incExecuted()
	}
	p.SetWorkerState(id, false)
}

// QueueLength reports how many jobs are currently waiting to be
// picked up by the scheduler (i.e. length of submit channel).
func (p *Pool[T]) QueueLength() int { return len(p.submitCh) }
