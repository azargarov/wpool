package workerpool

import (
	"context"
	"fmt"
	boff "github.com/Andrej220/go-utils/backoff"
	lg "github.com/Andrej220/go-utils/zlog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultMaxWorkers   = 10
	defaultAttempts     = 3
	defaultInitialRetry = 200 * time.Millisecond
	defauiltMaxRetry    = 5 * time.Second
)

type RetryPolicy struct {
	Attempts int
	Initial  time.Duration
	Max      time.Duration
}

type JobFunc[T any] func(T) error

type Job[T any] struct {
	Payload     T
	Fn          JobFunc[T]
	Ctx         context.Context
	CleanupFunc func()
	Retry       *RetryPolicy
}

type Pool[T any] struct {
	jobs           chan Job[T]
	wg             sync.WaitGroup
	maxWorkers     int
	activeWorkers  atomic.Int32
	stopOnce       sync.Once
	closed         chan struct{} // signals no more submissions
	defaultRetry   RetryPolicy
	submitBufRatio int
}

func GetDefaultRP() *RetryPolicy {
	rp := RetryPolicy{
		Attempts: defaultAttempts,
		Initial:  defaultInitialRetry,
		Max:      defauiltMaxRetry,
	}
	return &rp
}

func NewPool[T any](maxWorkers int, defaultRetry RetryPolicy) *Pool[T] {
	if maxWorkers <= 0 {
		maxWorkers = DefaultMaxWorkers
	}
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
		jobs:           make(chan Job[T], maxWorkers*2),
		maxWorkers:     maxWorkers,
		closed:         make(chan struct{}),
		defaultRetry:   defaultRetry,
		submitBufRatio: 2,
	}
	for i := 0; i < p.maxWorkers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
	return p
}

// None-blocking pull stop
func (p *Pool[T]) Shutdown(ctx context.Context) error {
	var already bool
	p.stopOnce.Do(func() {
		close(p.closed) // reject new jobs
		close(p.jobs)   // drain
	})
	// detect if we already closed before
	select {
	case <-p.closed:
		already = true
	default:
	}
	if !already {
		close(p.closed)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.wg.Wait()
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// blocking stop
func (p *Pool[T]) Stop() { _ = p.Shutdown(context.Background()) }

func (p *Pool[T]) Submit(job Job[T]) error {
	if job.Ctx == nil {
		job.Ctx = context.Background()
	}
	select {
	case <-p.closed:
		return fmt.Errorf("workerpool: pool closed")
	default:
	}
	select {
	case p.jobs <- job:
		lg.FromContext(job.Ctx).Info("Job submitted", lg.Any("job", job.Payload))
		return nil
	case <-p.closed:
		return fmt.Errorf("workerpool: pool closed")
	}
}

// Non-blocking submit.
func (p *Pool[T]) TrySubmit(job Job[T]) bool {
	if job.Ctx == nil {
		job.Ctx = context.Background()
	}
	select {
	case <-p.closed:
		return false
	default:
	}
	select {
	case p.jobs <- job:
		return true
	default:
		return false
	}
}

func (p *Pool[T]) worker() {
	defer p.wg.Done()
	for job := range p.jobs {
		p.activeWorkers.Add(1)
		func() {
			defer p.activeWorkers.Add(-1)
			defer func() {
				if r := recover(); r != nil {
					lg.FromContext(job.Ctx).Error("job panicked", lg.Any("panic", r))
				}
				if job.CleanupFunc != nil {
					job.CleanupFunc()
				}
			}()
			p.processJob(job)
		}()
	}
}

func (p *Pool[T]) processJob(job Job[T]) {
	logger := lg.FromContext(job.Ctx).With(lg.Any("job", job.Payload))
	logger.Info("Worker processing job", lg.Int32("active_workers", p.activeWorkers.Load()))

	pol := p.defaultRetry
	if job.Retry != nil {
		// override non-zero per-job values
		if job.Retry.Attempts > 0 {
			pol.Attempts = job.Retry.Attempts
		}
		if job.Retry.Initial > 0 {
			pol.Initial = job.Retry.Initial
		}
		if job.Retry.Max > 0 {
			pol.Max = job.Retry.Max
		}
	}

	bo := boff.New(pol.Initial, pol.Max, time.Now().UnixNano())

	for attempt := 1; attempt <= pol.Attempts; attempt++ {
		if err := job.Fn(job.Payload); err == nil {
			logger.Info("Worker finished", lg.Int32("active_workers", p.activeWorkers.Load()))
			return
		} else if attempt == pol.Attempts {
			logger.Error("Worker error", lg.Int("attempt", attempt), lg.Any("error", err))
			return
		} else {
			delay := bo.Next()
			logger.Warn("job attempt failed; backing off",
				lg.Int("attempt", attempt),
				lg.String("sleep", delay.String()),
				lg.Any("error", err),
			)
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-job.Ctx.Done():
				if !timer.Stop() {
					<-timer.C // drain if timer is fired
				}
				logger.Info("Job canceled", lg.Any("reason", job.Ctx.Err()))
				return
			}
		}
	}
}

func (p *Pool[T]) ActiveWorkers() int32 { return p.activeWorkers.Load() }
func (p *Pool[T]) QueueLength() int     { return len(p.jobs) }
