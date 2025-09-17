package workerpool

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var fastRetry = RetryPolicy{Attempts: 3, Initial: 5 * time.Millisecond, Max: 10 * time.Millisecond}

func TestJobSuccess(t *testing.T) {
	p := NewPool[int](2, fastRetry)
	defer p.Stop()

	done := make(chan struct{})
	jobCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := p.Submit(Job[int]{
		Payload: 1,
		Ctx:     jobCtx,
		Fn: func(n int) error {
			close(done)
			return nil
		},
	})
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
	p := NewPool[int](1, fastRetry)
	defer p.Stop()

	var attempts int32
	done := make(chan struct{})

	jobCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := p.Submit(Job[int]{
		Payload: 42,
		Ctx:     jobCtx,
		Retry:   &RetryPolicy{Attempts: 3, Initial: 2 * time.Millisecond, Max: 5 * time.Millisecond},
		Fn: func(_ int) error {
			if atomic.AddInt32(&attempts, 1) < 3 {
				return errors.New("fail")
			}
			close(done)
			return nil
		},
	})
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
	p := NewPool[int](1, fastRetry)
	defer p.Stop()

	var attempts int32
	step := make(chan struct{}) 
	jobCtx, cancel := context.WithCancel(context.Background())

	err := p.Submit(Job[int]{
		Payload: 7,
		Ctx:     jobCtx,
		Retry:   &RetryPolicy{Attempts: 5, Initial: 100 * time.Millisecond, Max: 100 * time.Millisecond},
		Fn: func(_ int) error {
			atomic.AddInt32(&attempts, 1)
			close(step) 
			return errors.New("boom")
		},
	})
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
	p := NewPool[int](1, fastRetry)

	started := make(chan struct{})
	done := make(chan struct{})

	_ = p.Submit(Job[int]{
		Payload: 1,
		Ctx:     context.Background(),
		Fn: func(int) error {
			close(started)
			time.Sleep(300 * time.Millisecond)
			close(done)
			return nil
		},
	})

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
	p := NewPool[int](1, fastRetry)
	_ = p.Shutdown(context.Background())

	if ok := p.TrySubmit(Job[int]{Payload: 1, Ctx: context.Background(), Fn: func(int) error { return nil }}); ok {
		t.Fatal("TrySubmit succeeded on closed pool; want false")
	}
	if err := p.Submit(Job[int]{Payload: 1, Ctx: context.Background(), Fn: func(int) error { return nil }}); err == nil {
		t.Fatal("Submit succeeded on closed pool; want error")
	}
}

func TestPanicRecoveryAndCleanup(t *testing.T) {
	p := NewPool[int](1, fastRetry)
	defer p.Stop()

	var mu sync.Mutex
	cleaned := 0
	secondDone := make(chan struct{})

	// first job panics
	_ = p.Submit(Job[int]{
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
	})

	// second job should still run
	_ = p.Submit(Job[int]{
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
	})

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
