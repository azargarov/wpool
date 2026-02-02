# workerpool — High-Performance Batch Worker Pool for Go

`workerpool` is a **high-throughput, bounded-concurrency worker pool** for Go, built around a **lock-free segmented FIFO queue** and **batch-based scheduling**.

It is designed for workloads where:
- job execution is cheap,
- submission rate is high,
- contention must be minimized,
- memory allocation must be predictable.

This is **not** a generic “feature-heavy” pool — it is a **low-level execution engine** optimized for performance and scalability.

Module:

```
github.com/azargarov/go-utils/wpool
```

---

## Core Design

At its core, `workerpool` combines:

- A **lock-free segmented queue** (MPMC)
- **Batch draining** instead of per-job wakeups
- **Bounded concurrency** with a fixed worker set
- **Minimal synchronization** between producers and consumers
- **Explicit memory reuse** via a segment pool

The queue is optimized to keep producers and consumers mostly independent, reducing cache-line contention and CAS pressure.

---

## Features

- **Bounded concurrency**
  - Fixed number of worker goroutines
  - No unbounded goroutine spawning
- **Lock-free segmented FIFO queue**
  - Multiple producers
  - Batch-based consumption
- **Batch scheduling**
  - Workers process jobs in batches for cache efficiency
  - Reduces wakeups and atomic traffic
- **Explicit memory reuse**
  - Preallocated queue segments
  - Segment recycling with generation counters
- **Context-aware jobs**
  - Submission respects `context.Context`
- **Panic-safe execution**
  - Workers are isolated from job panics
- **Graceful shutdown**
  - Deadline-aware draining
- **Low-overhead metrics hook**
  - Metrics policy is injected, not hardcoded
- **Optional CPU pinning**
  - Improves cache locality on NUMA / high-core systems

---

## Installation

```bash
go get github.com/azargarov/go-utils/wpool
```

---

## Quick Start

```go
package main

import (
	"context"
	"fmt"

	wp "github.com/azargarov/go-utils/wpool"
)

func main() {
	pool := wp.NewPool(
		wp.NoopMetrics{},
		wp.WithWorkers(4),
		wp.WithSegmentSize(4096),
		wp.WithSegmentCount(64),
	)

	defer pool.Stop()

	_ = pool.Submit(wp.Job[int]{
		Payload: 42,
		Ctx:     context.Background(),
		Fn: func(n int) error {
			fmt.Println("processing", n)
			return nil
		},
	}, 0)
}
```

---

## Job Model

```go
type Job[T any] struct {
	Payload     T
	Fn          func(T) error
	Ctx         context.Context
	CleanupFunc func()
}
```

- `Ctx` is checked before enqueueing
- `CleanupFunc` is guaranteed to run after execution
- Jobs are executed **in FIFO order**

> Note: `basePrio` is currently unused and reserved for future schedulers.

---

## Queue Implementation

### Segmented Queue

- Queue consists of linked **segments**
- Each segment contains:
  - job buffer
  - readiness bitmap
  - producer / consumer cursors
- Producers append using CAS on a per-segment reserve index
- Consumers drain **contiguous ready ranges** as batches

Key properties:

- No global locks
- No per-job wakeups
- Minimal false sharing
- Segment reuse via generation counters (ABA-safe)

---

## Batch Processing

Workers wake up only when:
- enough jobs are pending, or
- a batch timer fires

This allows:
- amortized synchronization cost
- better cache locality
- predictable throughput under load

---

## Shutdown Semantics

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

if err := pool.Shutdown(ctx); err != nil {
	// deadline exceeded
}
```

- Prevents new submissions
- Drains queued work
- Waits for workers to finish or deadline to expire

---

## Metrics

Metrics are **policy-driven**:

```go
type MetricsPolicy interface {
	IncQueued()
	BatchDecQueued(n int)
}
```

This keeps the hot path free of unnecessary overhead.

---

## Options

```go
type Options struct {
	Workers       int
	SegmentSize   uint32
	SegmentCount  uint32
	PoolCapacity  uint32
	QT            QueueType
	PinWorkers    bool
}
```

Defaults are applied automatically via `FillDefaults()`.

---

## QueueType

Currently implemented:

- **SegmentedQueue** — lock-free FIFO queue

`QueueType` exists as an extension point. Other schedulers (bucketed, priority, aging) are **not yet wired in**.

---

## What This Is (and Isn’t)

✅ This **is**:
- a high-performance execution engine
- suitable for internal systems, pipelines, schedulers
- ideal when you care about ns/op and cache lines

❌ This is **not**:
- a feature-rich task framework
- a priority scheduler (yet)
- a general-purpose job system

---

## Roadmap (Explicitly Non-Promissory)

Planned directions (not yet implemented):

- Bucket-based priority scheduler
- Aging via queue rotation
- Adaptive segment provisioning
- NUMA-aware worker placement

These are **design directions**, not guarantees.