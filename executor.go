package workerpool

import (
	"context"
	"errors"
	"fmt"
)

// ErrPoolPanic is returned when a job function panics.
//
// Panics are recovered to prevent worker termination and
// are converted into regular errors.
var ErrPoolPanic = errors.New("workerpool: job panic. ")

// runBatch executes a batch of jobs sequentially.
//
// Any job-level error is reported via the pool's error handler.
// Execution continues even if individual jobs fail.
func (p *Pool[T, M]) runBatch(jobs []Job[T]) {
	for _, j := range jobs {
		if err := p.runJob(j); err != nil {
			p.reportJobError(err)
		}
	}
}

// runJob executes a single job and returns any resulting error.
//
// Guarantees:
//   - the job context is respected before execution
//   - CleanupFunc is always invoked if provided
//   - panics inside the job are recovered and returned as errors
//   - execution metrics are updated exactly once
func (p *Pool[T, M]) runJob(j Job[T]) (err error) {

	if j.Ctx == nil {
		j.Ctx = context.Background()
	}
	// Ensure cleanup is always attempted, even if the job panics.
	if j.CleanupFunc != nil {
		defer func() {
            // Cleanup panics are intentionally suppressed.
			defer func() { _ = recover() }()
			j.CleanupFunc()
		}()
	}

    // Count job execution regardless of outcome.
	defer p.metrics.IncExecuted()

	// Respect job cancellation before execution.
	select {
	case <-j.Ctx.Done():
		return j.Ctx.Err()
	default:
	}

	if j.Fn == nil {
		return ErrNilFunc
	}

	// Recover from panics in user code and convert them to errors.
	defer func() {
		if r := recover(); r != nil {
			err = errors.Join(ErrPoolPanic, fmt.Errorf("panic: %v", r))
		}
	}()

	return j.Fn(j.Payload)
}

// batchProcessJob drains available batches from the queue
// and executes them until no more work is available.
//
// It returns the total number of jobs processed.
func (p *Pool[T, M]) batchProcessJob() int64 {
	var counter int64

	for {
		batch, ok := p.sched.Pop()
		if !ok {
			return counter
		}

		p.runBatch(batch.Jobs)
		counter += int64(len(batch.Jobs))
		p.sched.OnBatchDone(batch)
	}
}
