# workerpool

A tiny, **bounded-concurrency** worker pool for Go with **per‑job retries**, **context‑aware cancelation**, **panic recovery**, and **graceful shutdown with timeout**.

- Generic over payload type `T`.
- Per‑pool defaults + per‑job `RetryPolicy` overrides.
- Context-aware backoff (stops sleeping when the job is canceled).
- `Submit` (blocking) and `TrySubmit` (non‑blocking).
- `Shutdown(ctx)` to close the pool and wait up to a deadline.
- Safe cleanup callbacks and panic isolation (a panicking job won’t kill the worker).

---

## Install

```bash
go get github.com/Andrej220/go-utils/workerpool
```

---

## Quick start

```go
package main

import (
	"context"
	"fmt"
	"time"

	wp "github.com/Andrej220/go-utils/workerpool"
)

func main() {
	// Create a pool with 4 workers and sane default retry policy.
	pool := wp.NewPool[int](4, wp.RetryPolicy{
		Attempts: 3,
		Initial:  200 * time.Millisecond,
		Max:      5 * time.Second,
	})
	defer pool.Stop() // blocks until all jobs complete

	// Submit a job with a 3s timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_ = pool.Submit(wp.Job[int]{
		Payload: 42,
		Ctx:     ctx,
		Fn: func(n int) error {
			fmt.Println("processing", n)
			return nil
		},
	})
}
```

---

## Per‑job retry override

Override the pool defaults for a specific job:

```go
_ = pool.Submit(wp.Job[int]{
	Payload: 1,
	Ctx:     context.Background(),
	Retry:   &wp.RetryPolicy{Attempts: 5, Initial: 50 * time.Millisecond, Max: 500 * time.Millisecond},
	Fn: func(n int) error {
		// do work; return error to trigger retry/backoff
		return fmt.Errorf("transient failure")
	},
})
```

---

## Cancel during backoff (context‑aware)

Backoff sleep stops early if the job’s context is canceled:

```go
ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
defer cancel()

_ = pool.Submit(wp.Job[int]{
	Payload: 7,
	Ctx:     ctx,
	Fn: func(int) error {
		// return an error so the pool enters backoff; the timeout cancels it
		return fmt.Errorf("boom")
	},
})
```

---

## Graceful shutdown with deadline

`Shutdown(ctx)` stops submissions, closes the jobs channel, and waits for workers up to `ctx`’s deadline:

```go
// Ask workers to finish within 5s.
shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

if err := pool.Shutdown(shCtx); err != nil {
	// context.DeadlineExceeded if not all jobs finished in time
}
```

`Stop()` is equivalent to `Shutdown(context.Background())` (waits indefinitely).

---

## API

### Types

```go
type RetryPolicy struct {
	Attempts int           // number of tries; >=1
	Initial  time.Duration // first backoff
	Max      time.Duration // cap for backoff
}

type JobFunc[T any] func(T) error

type Job[T any] struct {
	Payload     T
	Fn          JobFunc[T]
	Ctx         context.Context      // nil -> context.Background()
	CleanupFunc func()               // optional; always called
	Retry       *RetryPolicy         // nil -> pool default
}

type Pool[T any] struct { /* ... */ }
```

### Constructors & methods

```go
func NewPool[T any](maxWorkers int, defaultRetry RetryPolicy) *Pool[T]

// Queue a job (blocks if the channel buffer is full). Returns error if pool is closed.
func (p *Pool[T]) Submit(job Job[T]) error

// Try to queue a job without blocking. Returns false if buffer full or pool is closed.
func (p *Pool[T]) TrySubmit(job Job[T]) bool

// Close the pool and wait for workers up to ctx deadline (drains queued jobs).
func (p *Pool[T]) Shutdown(ctx context.Context) error

// Wait forever (no deadline). Legacy convenience.
func (p *Pool[T]) Stop()

func (p *Pool[T]) ActiveWorkers() int32
func (p *Pool[T]) QueueLength() int
```

---

## Design notes

- **Bounded concurrency:** fixed worker count reads from a buffered `jobs` channel (size `2 * maxWorkers` by default).
- **Draining on shutdown:** `Shutdown` closes `jobs`; workers exit after the buffer is drained and in‑flight jobs finish (or their contexts cancel).
- **Backoff:** uses your `github.com/Andrej220/go-utils/backoff` generator.
- **Panic safety:** worker wraps each job in `recover()` so a crashing job doesn’t kill the worker.
- **Context everywhere:** jobs can time out or be canceled; backoff sleeps are interruptible via `ctx.Done()`.
- **Logging:** if you inject a logger into `context` (e.g., your `zlog` helper), the pool will use it; otherwise it’s a no‑op.

---

## Testing

This package is designed for unit tests:
- Tiny retry/backoff values speed up tests.
- See sample tests for: success, retry, cancel‑during‑backoff, shutdown deadline, panic recovery, cleanup callbacks.

Run:
```bash
go test ./...
```

