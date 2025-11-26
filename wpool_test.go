package workerpool_test

import (
	"context"
	"errors"
	//"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	wp "github.com/azargarov/go-utils/wpool"
)

var fastRetry = wp.RetryPolicy{
	Attempts: 3,
	Initial:  5 * time.Millisecond,
	Max:      10 * time.Millisecond,
}

var queueTypes = []wp.QueueType{
	wp.Fifo,
	//wp.Priority,
	//wp.Conditional,
	wp.BucketQueue,
}

func newTestPool(workers int, qt wp.QueueType) *wp.Pool[int] {
	opts := wp.Options{
		Workers:    workers,
		AgingRate:  0.3,
		RebuildDur: 10 * time.Millisecond,
		QueueSize:  4_000_000,
		QT:         qt,
	}
	return wp.NewPool[int](opts, fastRetry)
}

func TestDefaultRetryPolicy(t *testing.T) {
	rp := wp.GetDefaultRP()
	if rp == nil {
		t.Fatalf("Default Retry Policy is nil")
	}

	if rp.Attempts == 0 || rp.Initial == 0 || rp.Max == 0 {
		t.Fatal("Default retry policy is not default")
	}
}

func TestFillDefaults(t *testing.T) {
	var o wp.Options
	o.FillDefaults()
	if o.Workers <= 0 || o.QueueSize <= 0 {
		t.Fatal("defaults not filled")
	}
}

func TestPoolQueues(t *testing.T) {
	for _, qt := range queueTypes {
		qt := qt // capture range variable
		t.Run(qt.String(), func(t *testing.T) {
			t.Parallel()

			t.Run("JobSuccess", func(t *testing.T) {
				t.Parallel()
				jobSuccess(t, qt)
			})

			t.Run("RetryThenSuccess", func(t *testing.T) {
				t.Parallel()
				retryThenSuccess(t, qt)
			})

			t.Run("CancelDuringBackoff", func(t *testing.T) {
				t.Parallel()
				cancelDuringBackoff(t, qt)
			})

			t.Run("ShutdownTimeout", func(t *testing.T) {
				t.Parallel()
				shutdownTimeout(t, qt)
			})

			t.Run("SubmitAfterShutdown", func(t *testing.T) {
				t.Parallel()
				submitAfterShutdown(t, qt)
			})

			t.Run("PanicRecoveryAndCleanup", func(t *testing.T) {
				t.Parallel()
				panicRecoveryAndCleanup(t, qt)
			})

			t.Run("MetricsAndQueueLength", func(t *testing.T) {
				t.Parallel()
				metricsAndQueueLength(t, qt)
			})

			t.Run("SubmitCanceledContext", func(t *testing.T) {
				t.Parallel()
				submitCanceledContext(t, qt)
			})
		})
	}
}

// -----------------------------------------------------------------------------
// Helper test functions (one scenario, parameterized by queue type)
// -----------------------------------------------------------------------------

func jobSuccess(t *testing.T, qt wp.QueueType) {
	t.Helper()

	p := newTestPool(1, qt)
	defer p.Stop()

	done := make(chan struct{})
	jobCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := p.Submit(wp.Job[int]{
		Payload: 1,
		Ctx:     jobCtx,
		Fn: func(n int) error {
			close(done)
			return nil
		},
	}, 10)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("job did not complete in time")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = p.Shutdown(ctx)

	if got := p.ActiveWorkers(); got != 0 {
		t.Fatalf("active workers = %d; want 0", got)
	}
}

func retryThenSuccess(t *testing.T, qt wp.QueueType) {
	t.Helper()

	p := newTestPool(1, qt)
	defer p.Stop()

	var attempts int32
	done := make(chan struct{})

	jobCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := p.Submit(wp.Job[int]{
		Payload: 42,
		Ctx:     jobCtx,
		Retry:   &wp.RetryPolicy{Attempts: 3, Initial: 2 * time.Millisecond, Max: 5 * time.Millisecond},
		Fn: func(_ int) error {
			if atomic.AddInt32(&attempts, 1) < 3 {
				return errors.New("fail")
			}
			close(done)
			return nil
		},
	}, 10)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("job did not succeed after retries in time")
	}

	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("attempts = %d; want 3", got)
	}
}

func cancelDuringBackoff(t *testing.T, qt wp.QueueType) {
	t.Helper()

	p := newTestPool(1, qt)
	defer p.Stop()

	var attempts int32
	step := make(chan struct{})
	jobCtx, cancel := context.WithCancel(context.Background())

	var once sync.Once

	err := p.Submit(wp.Job[int]{
		Payload: 7,
		Ctx:     jobCtx,
		Retry:   &wp.RetryPolicy{Attempts: 5, Initial: 100 * time.Millisecond, Max: 100 * time.Millisecond},
		Fn: func(_ int) error {
			atomic.AddInt32(&attempts, 1)
			once.Do(func() { close(step) })
			return errors.New("boom")
		},
	}, 10)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	// wait until first attempt happened, then cancel during backoff
	select {
	case <-step:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("first attempt did not happen in time")
	}
	cancel()

	// small wait so worker can observe cancel and exit backoff
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("attempts after cancel = %d; want 1", got)
	}
}

func shutdownTimeout(t *testing.T, qt wp.QueueType) {
	t.Helper()

	p := newTestPool(1, qt)

	started := make(chan struct{})
	done := make(chan struct{})

	_ = p.Submit(wp.Job[int]{
		Payload: 1,
		Ctx:     context.Background(),
		Fn: func(int) error {
			close(started)
			time.Sleep(300 * time.Millisecond)
			close(done)
			return nil
		},
	}, 10)

	select {
	case <-started:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("job did not start in time")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := p.Shutdown(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown err = %v; want deadline exceeded", err)
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("job did not finish after shutdown timeout")
	}

	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown err = %v; want nil", err)
	}
}

func submitAfterShutdown(t *testing.T, qt wp.QueueType) {
	t.Helper()

	p := newTestPool(1, qt)
	_ = p.Shutdown(context.Background())

	if err := p.Submit(wp.Job[int]{
		Payload: 1,
		Ctx:     context.Background(),
		Fn:      func(int) error { return nil },
	}, 10); err == nil {
		t.Fatal("Submit succeeded on closed pool; want error")
	}
}

func panicRecoveryAndCleanup(t *testing.T, qt wp.QueueType) {
	t.Helper()

	p := newTestPool(1, qt)
	defer p.Stop()

	var mu sync.Mutex
	cleaned := 0
	secondDone := make(chan struct{})

	// first job panics
	_ = p.Submit(wp.Job[int]{
		Payload: 1,
		Ctx:     context.Background(),
		Fn: func(int) error {
			panic("boom")
		},
		CleanupFunc: func() {
			mu.Lock()
			cleaned++
			mu.Unlock()
		},
	}, 10)

	// second job should still run
	_ = p.Submit(wp.Job[int]{
		Payload: 2,
		Ctx:     context.Background(),
		Fn: func(int) error {
			close(secondDone)
			return nil
		},
		CleanupFunc: func() {
			mu.Lock()
			cleaned++
			mu.Unlock()
		},
	}, 10)

	select {
	case <-secondDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("second job did not complete after first panicked")
	}

	// allow cleanup defers to run
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if cleaned != 2 {
		t.Fatalf("cleanup called %d times; want 2", cleaned)
	}
}

func metricsAndQueueLength(t *testing.T, qt wp.QueueType) {
	t.Helper()

	p := newTestPool(1, qt)
	defer p.Stop()

	_ = p.Submit(wp.Job[int]{Payload: 1, Fn: func(n int) error { return nil }}, 10)

	time.Sleep(20 * time.Millisecond)

	m := p.Metrics()
	if m.Submitted() == 0 {
		t.Fatal("expected submitted > 0")
	}

	if p.QueueLength() < 0 {
		t.Fatal("queue length should not be negative")
	}
}

func submitCanceledContext(t *testing.T, qt wp.QueueType) {
	t.Helper()

	p := newTestPool(1, qt)
	defer p.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := p.Submit(wp.Job[int]{
		Ctx: ctx,
		Fn:  func(int) error { return nil },
	}, 1)
	if err == nil {
		t.Fatal("expected error when submitting with canceled context")
	}
}
