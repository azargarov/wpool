# workerpool — High-Performance Queues, Priority, Aging & Retries for Go

A **bounded-concurrency** worker pool for Go with:

- **Multiple schedulers**: FIFO, Priority, Conditional, BucketQueue  
- **Aging** (old jobs bubble up automatically)  
- **Per-job retries** with context-aware backoff  
- **Panic-safe workers** and per-job cleanup  
- **Graceful shutdown** with deadlines  
- **High-resolution metrics** (submitted, executed, active workers, max age)  
- **Ultra-fast bucket-based priority queue** (v0.3.0)

Module: `github.com/azargarov/go-utils/wpool`

---

## Install

```bash
go get github.com/azargarov/go-utils/wpool@v0.3.0
```

---

## Quick start

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
        QT:         wp.Priority,
    }

    pool := wp.NewPool[int](opts, wp.RetryPolicy{
        Attempts: 3,
        Initial:  200 * time.Millisecond,
        Max:      5 * time.Second,
    })
    defer pool.Stop()

    jobCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()

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

## Queue Types (v0.3.0)

`Options.QT` lets you choose between several scheduling strategies:

| Queue Type     | Behavior | Use Case |
|----------------|----------|----------|
| **Fifo**       | First-in–first-out | Best latency, predictable ordering |
| **Priority**   | Aging + max-heap | Mixed workloads where fairness matters |
| **BucketQueue**  | Fixed-range buckets with O(1) push/pop | High throughput |

---

## Priority & Aging

Each job has a **base priority**.  
Scheduler computes:

```
effective = basePriority + agingRate * ageSeconds
```

Higher effective priority → runs sooner.  
Low-priority jobs eventually rise — **no starvation**.

---

## BucketQueue (v0.3.0)

Bucket-based priority queue:

- 61 buckets (prio 0–60)
- Bitmap for bucket occupancy
- `bits.LeadingZeros64` for instant highest-bucket lookup

---

## Per-job Retry Override

```go
_ = pool.Submit(wp.Job[int]{
    Payload: 1,
    Ctx:     context.Background(),
    Retry: &wp.RetryPolicy{Attempts: 5, Initial: 50 * time.Millisecond},
    Fn: func(n int) error { return fmt.Errorf("fail") },
}, 5)
```

---

## Cancel During Backoff

```go
ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
defer cancel()

_ = pool.Submit(wp.Job[int]{
    Payload: 7,
    Ctx:     ctx,
    Fn: func(int) error { return fmt.Errorf("boom") },
}, 10)
```

---

## Graceful Shutdown

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

if err := pool.Shutdown(ctx); err != nil {
    // deadline exceeded
}
```

---

## Metrics

```go
m := pool.Metrics()
fmt.Println("submitted:", m.Submitted())
fmt.Println("executed:", m.Executed())
fmt.Println("active workers:", pool.ActiveWorkers())
fmt.Println("queue length:", pool.QueueLength())
```

---

## Benchmark Suite (v0.3.0)

- FIFO / Priority / BucketQueue microbenchmarks  
- Parallel submit scaling  
- Multi-priority stress tests  
- End-to-end latency  
- Automatic summarization

```bash
go test -bench Benchmark -benchmem -run ^$
```

---

## API Overview

```go
type Options struct {
    Workers    int
    AgingRate  float64
    RebuildDur time.Duration
    QueueSize  int
    QT         QueueType
}
```

---

## What’s New in v0.3.0

- New **BucketQueue** with bitmap O(1) scheduling  
- Benchmark suite overhaul  
- Scheduler cleanup + fewer allocations  
- Improved metrics  
---

## Related Packages

- `backoff` — retry helpers  
- `zlog` — structured logging  
- `grlimit` — goroutine throttler  