package workerpool_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	wp "github.com/azargarov/wpool"
)

var queueTypes = []wp.QueueType{
	//wp.Fifo,
	//wp.Priority,
	//wp.Conditional,
	wp.BucketQueue,
}

func newTestPool(workers int, qt wp.QueueType) *wp.Pool[int, *wp.NoopMetrics] {
	opts := wp.Options{
		Workers:    workers,
		AgingRate:  3,
		RebuildDur: 10 * time.Millisecond,
		QueueSize:  4_000_000,
		QT:         qt,
	}
	m := &wp.NoopMetrics{}
	return wp.NewPool[int](opts, m)
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

			t.Run("ShutdownTimeout", func(t *testing.T) {
				t.Parallel()
				shutdownTimeout(t, qt)
			})
//
			t.Run("SubmitAfterShutdown", func(t *testing.T) {
				t.Parallel()
				submitAfterShutdown(t, qt)
			})
//
			t.Run("PanicRecoveryAndCleanup", func(t *testing.T) {
				t.Parallel()
				panicRecoveryAndCleanup(t, qt)
			})
//
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


