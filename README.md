# workerpool — Priority, Aging, and Retries for Go Jobs

**bounded-concurrency** worker pool for Go with:

-  **Priority queue** in front of workers  
-  **Aging** (old jobs bubble up automatically)  
-  **Per-job retries** with context-aware backoff  
-  **Panic-safe workers** and per-job cleanup  
-  **Graceful shutdown** with deadlines  
-  **Metrics** (submitted, executed, active workers, max age)

Module: `github.com/azargarov/go-utils/wpool`

---

##  Install

```bash
go get github.com/azargarov/go-utils/wpool
```

---

##  Quick start

```go
package main

import (
	"context"
	"fmt"
	"time"

	wp "github.com/azargarov/go-utils/wpool"
)

func main() {
	opts := wp.Options{
		Workers:    4,
		AgingRate:  0.3,
		RebuildDur: 200 * time.Millisecond,
		QueueSize:  256,
	}

	pool := wp.NewPool[int](opts, wp.RetryPolicy{
		Attempts: 3,
		Initial:  200 * time.Millisecond,
		Max:      5 * time.Second,
	})
	defer pool.Stop()

	jobCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Submit a job with base priority = 10
	if err := pool.Submit(wp.Job[int]{
		Payload: 42,
		Ctx:     jobCtx,
		Fn: func(n int) error {
			fmt.Println("processing", n)
			return nil
		},
	}, 10); err != nil {
		panic(err)
	}
}
```

---

## Priority & Aging

Each job has a **base priority** (`float64`).  
The scheduler uses a **max-heap** and periodically “ages” queued jobs:

```
effective = basePriority + agingRate * ageSeconds
```

So high-priority jobs run sooner, but even low-priority jobs eventually rise to the top — no starvation.

```go
_ = pool.Submit(jobFast, 100) // high priority
_ = pool.Submit(jobSlow, 1)   // low priority, but will age
```

---

##  Per-job Retry Override

You can override the pool’s default retry policy per job:

```go
_ = pool.Submit(wp.Job[int]{
	Payload: 1,
	Ctx:     context.Background(),
	Retry:   &wp.RetryPolicy{
		Attempts: 5,
		Initial:  50 * time.Millisecond,
		Max:      500 * time.Millisecond,
	},
	Fn: func(n int) error {
		return fmt.Errorf("transient failure")
	},
}, 5)
```

Retries are **context-aware** — canceling the job’s context stops the backoff instantly.

---

##  Cancel During Backoff

```go
ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
defer cancel()

_ = pool.Submit(wp.Job[int]{
	Payload: 7,
	Ctx:     ctx,
	Fn: func(int) error {
		// returning an error triggers retry/backoff;
		// the timeout cancels it early
		return fmt.Errorf("boom")
	},
}, 10)
```

---

##  Graceful Shutdown

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

if err := pool.Shutdown(ctx); err != nil {
	// context.DeadlineExceeded if not all jobs finished in time
}
```

`Stop()` is shorthand for `Shutdown(context.Background())` (waits indefinitely).

---

##  Metrics

```go
m := pool.Metrics()
fmt.Println("submitted:", m.Submitted())
fmt.Println("executed:", m.Executed())
fmt.Println("active workers:", pool.ActiveWorkers())
fmt.Println("queue length:", pool.QueueLength())
```

---

##  API Overview

```go
type Options struct {
	Workers    int
	AgingRate  float64
	RebuildDur time.Duration
	QueueSize  int
}

type RetryPolicy struct {
	Attempts int
	Initial  time.Duration
	Max      time.Duration
}

type Job[T any] struct {
	Payload     T
	Fn          func(T) error
	Ctx         context.Context
	CleanupFunc func()
	Retry       *RetryPolicy
}

func NewPool[T any](opts Options, defaultRetry RetryPolicy) *Pool[T]
func (p *Pool[T]) Submit(job Job[T], basePrio float64) error
func (p *Pool[T]) Shutdown(ctx context.Context) error
func (p *Pool[T]) Stop()
func (p *Pool[T]) Metrics() Metrics
func (p *Pool[T]) ActiveWorkers() int
func (p *Pool[T]) QueueLength() int
```

---

##  Design Highlights

- **Bounded concurrency** — fixed worker count, controlled queue.  
- **Scheduler** — priority heap + periodic “aging” of queued jobs.  
- **Graceful shutdown** — drains queue before exiting.  
- **Retry & backoff** — configurable per job, context-aware.  
- **Safe workers** — recover from panics and always run cleanup.  
- **Metrics** — track submitted/executed jobs and worker states.

---

##  Testing

This package is test-friendly:
- Tiny backoff values speed up unit tests.
- Includes tests for success, retry, cancel-during-backoff, shutdown deadlines, and panic recovery.

Run:
```bash
go test ./...
```

---

##  Related Packages

- [`backoff`](../backoff) — Exponential retry and delay generator  
- [`zlog`](../zlog) — Structured logger (zap-based)  
- [`grlimit`](../grlimit) — Lightweight concurrency gate  