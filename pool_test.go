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
	wp.SegmentedQueue,
}

// -----------------------------------------------------------------------------
// Options defaults
// -----------------------------------------------------------------------------

func TestFillDefaults(t *testing.T) {
	var o wp.Options
	o.FillDefaults()

	if o.Workers <= 0 {
		t.Fatal("expected Workers to be set by FillDefaults")
	}
}

// -----------------------------------------------------------------------------
// Pool behavior tests
// -----------------------------------------------------------------------------

func TestPoolQueues(t *testing.T) {
	tests := []struct {
		name string
		fn   func(t *testing.T, qt wp.QueueType)
	}{
		{"JobSuccess", testJobSuccess},
		{"ShutdownTimeout", testShutdownTimeout},
		{"SubmitAfterShutdown", testSubmitAfterShutdown},
		{"PanicRecoveryAndCleanup", testPanicRecoveryAndCleanup},
		{"SubmitCanceledContext", testSubmitCanceledContext},
	}

	for _, qt := range queueTypes {
		qt := qt

		t.Run(qt.String(), func(t *testing.T) {
			t.Parallel()

			for _, tc := range tests {
				tc := tc
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					tc.fn(t, qt)
				})
			}
		})
	}
}

func testJobSuccess(t *testing.T, qt wp.QueueType) {
	t.Helper()

	p := newTestPool[int](t, 1, qt)
	defer p.Stop()

	done := make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	meta := wp.JobMeta{Ctx: ctx}

	err := p.Submit(wp.Job[int]{
		Payload: 1,
		Meta:    &meta,
		Fn: func(int) error {
			close(done)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("job did not complete")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := p.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	if got := p.ActiveWorkers(); got != 0 {
		t.Fatalf("active workers = %d; want 0", got)
	}
}

func testShutdownTimeout(t *testing.T, qt wp.QueueType) {
	t.Helper()

	p := newTestPool[int](t, 1, qt)

	started := make(chan struct{})
	done := make(chan struct{})

	meta := wp.JobMeta{Ctx: context.Background()}

	_ = p.Submit(wp.Job[int]{
		Payload: 1,
		Meta:    &meta,
		Fn: func(int) error {
			close(started)
			time.Sleep(300 * time.Millisecond)
			close(done)
			return nil
		},
	})

	select {
	case <-started:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("job did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := p.Shutdown(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded; got %v", err)
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("job did not complete after timeout")
	}

	// second shutdown should succeed
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("second shutdown failed: %v", err)
	}
}

func testSubmitAfterShutdown(t *testing.T, qt wp.QueueType) {
	t.Helper()

	p := newTestPool[int](t, 1, qt)

	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	meta := wp.JobMeta{Ctx: context.Background()}

	err := p.Submit(wp.Job[int]{
		Payload: 1,
		Meta:    &meta,
		Fn:      func(int) error { return nil },
	})

	if err == nil {
		t.Fatal("expected error submitting to closed pool")
	}
}

func testPanicRecoveryAndCleanup(t *testing.T, qt wp.QueueType) {
	t.Helper()

	p := newTestPool[int](t, 1, qt)
	defer p.Stop()

	var mu sync.Mutex
	cleaned := 0
	secondDone := make(chan struct{})

	increment := func() {
		mu.Lock()
		cleaned++
		mu.Unlock()
	}

	meta1 := wp.JobMeta{
		Ctx:         context.Background(),
		CleanupFunc: increment,
	}

	// First job panics
	_ = p.Submit(wp.Job[int]{
		Payload: 1,
		Meta:    &meta1,
		Fn: func(int) error {
			panic("boom")
		},
	})

	meta2 := wp.JobMeta{
		Ctx:         context.Background(),
		CleanupFunc: increment,
	}

	// Second job must still execute
	_ = p.Submit(wp.Job[int]{
		Payload: 2,
		Meta:    &meta2,
		Fn: func(int) error {
			close(secondDone)
			return nil
		},
	})

	select {
	case <-secondDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("second job did not execute")
	}

	waitUntil(t, 200*time.Millisecond, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return cleaned == 2
	})
}

func testSubmitCanceledContext(t *testing.T, qt wp.QueueType) {
	t.Helper()

	p := newTestPool[int](t, 1, qt)
	defer p.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	meta := wp.JobMeta{Ctx: ctx}

	err := p.Submit(wp.Job[int]{
		Meta: &meta,
		Fn:   func(int) error { return nil },
	})

	if err == nil {
		t.Fatal("expected error when submitting canceled job")
	}
}

