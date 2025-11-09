package workerpool_test

import (
	"context"
	"errors"
	wp "github.com/azargarov/go-utils/wpool"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var fastRetry = wp.RetryPolicy{Attempts: 3, Initial: 5 * time.Millisecond, Max: 10 * time.Millisecond}

func newTestPool(workers int) *wp.Pool[int] {
	opts := wp.Options{
		Workers:    workers,
		AgingRate:  0.3,
		RebuildDur: 10 * time.Millisecond,
		QueueSize:  128,
	}
	return wp.NewPool[int](opts, fastRetry)
}

func TestDefultRetryPolicy(t *testing.T) {
	rp := wp.GetDefaultRP()
	if rp == nil {
		t.Fatalf("Default Retry Policy is nil")
	}

	if rp.Attempts == 0 || rp.Initial == 0 || rp.Max == 0 {
		t.Fatal("Default retry policy is not default")
	}
}

func TestJobSuccess(t *testing.T) {
	p := newTestPool(2)
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
	}, 10) // priority
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("job did not complete")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = p.Shutdown(ctx)

	if got := p.ActiveWorkers(); got != 0 {
		t.Fatalf("active workers = %d; want 0", got)
	}
}

func TestRetryThenSuccess(t *testing.T) {
	p := newTestPool(1)
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
		t.Fatal("job did not succeed after retries")
	}

	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("attempts = %d; want 3", got)
	}
}

func TestCancelDuringBackoff(t *testing.T) {
	p := newTestPool(1)
	defer p.Stop()

	var attempts int32
	step := make(chan struct{})
	jobCtx, cancel := context.WithCancel(context.Background())

	err := p.Submit(wp.Job[int]{
		Payload: 7,
		Ctx:     jobCtx,
		Retry:   &wp.RetryPolicy{Attempts: 5, Initial: 100 * time.Millisecond, Max: 100 * time.Millisecond},
		Fn: func(_ int) error {
			atomic.AddInt32(&attempts, 1)
			close(step)
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

	time.Sleep(50 * time.Millisecond) // allow worker to observe cancel
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("attempts after cancel = %d; want 1", got)
	}
}

func TestShutdownTimeout(t *testing.T) {
	p := newTestPool(1)

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

	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := p.Shutdown(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown err = %v; want deadline exceeded", err)
	}

	<-done
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown err = %v; want nil", err)
	}
}

func TestSubmitAfterShutdown(t *testing.T) {
	p := newTestPool(1)
	_ = p.Shutdown(context.Background())

	// we don't have TrySubmit in the new API, so just check Submit
	if err := p.Submit(wp.Job[int]{
		Payload: 1,
		Ctx:     context.Background(),
		Fn:      func(int) error { return nil },
	}, 10); err == nil {
		t.Fatal("Submit succeeded on closed pool; want error")
	}
}

func TestPanicRecoveryAndCleanup(t *testing.T) {
	p := newTestPool(1)
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

func TestMetricsAndQueueLength(t *testing.T) {
	p := newTestPool(1)
	defer p.Stop()

	// submit a quick job
	_ = p.Submit(wp.Job[int]{Payload: 1, Fn: func(n int) error { return nil }}, 10)

	time.Sleep(20 * time.Millisecond)

	m := p.Metrics()
	if m.Submitted() == 0 { // if you export getter
		t.Fatal("expected submitted > 0")
	}

	if p.QueueLength() < 0 {
		t.Fatal("queue length should not be negative")
	}
}

func TestFillDefaults(t *testing.T) {
	var o wp.Options
	o.FillDefaults()
	if o.Workers <= 0 || o.QueueSize <= 0 {
		t.Fatal("defaults not filled")
	}
}

func TestSubmitCanceledContext(t *testing.T) {
	p := newTestPool(1)
	defer p.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := p.Submit(wp.Job[int]{Ctx: ctx, Fn: func(int) error { return nil }}, 1)
	if err == nil {
		t.Fatal("expected error when submitting with canceled context")
	}
}
