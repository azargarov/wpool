# wpool — Experimental Worker Pool for Go

[![Go 1.22+](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat-square&logo=go)](https://go.dev/)
[![MIT License](https://img.shields.io/badge/License-MIT-green?style=flat-square)](LICENSE)
[![Experimental](https://img.shields.io/badge/status-experimental-orange?style=flat-square)](https://github.com/azargarov/wpool)
[![Go Reference](https://pkg.go.dev/badge/github.com/azargarov/wpool.svg)](https://pkg.go.dev/github.com/azargarov/wpool)

> ⚠️ **Experimental.** This is a research-grade implementation exploring lock-free queue design and heuristic memory reclamation in Go. It is not production-ready. See [Known Limitations](#known-limitations) before use.

`wpool` is a **bounded-concurrency worker pool** for Go built around a **lock-free segmented FIFO queue** and **batch-based scheduling**. The primary goal is to explore how far contention and allocation overhead can be pushed down on the submission path.

**Empty-job baseline** (scheduler + queue overhead only, 1 producer):

```
~93 ns/op   ·   ~10.7 M jobs/sec   ·   0 allocs/op   (w=1, p=1)
```

> Measured on AMD Ryzen 7 8845HS · Go 1.22 · Linux. See [Benchmarks](#benchmarks) for full worker/workload breakdown.

---

## Why wpool?


- **Lock-free queue** built from linked segments with per-slot CAS reservation — producers never block each other
- **Batch draining** means workers wake once to process many jobs, not once per job — cuts atomic traffic and scheduler overhead
- **Zero allocations** on the hot path — segments are pre-allocated and recycled using generation counters (ABA-safe)
- **Heuristic memory reclamation** via a limbo queue — safe for typical configurations, with known edge cases under investigation (see [Known Limitations](#known-limitations))

This is primarily a learning and research project. The design trades simplicity and operational safety for exploration of what's achievable in lock-free Go.

---

## Installation

```bash
go get github.com/azargarov/wpool
```

---

## Quick Start

```go
package main

import (
    "context"
    "fmt"

    wp "github.com/azargarov/wpool"
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

## Architecture

```
Producers (N goroutines)
        │
        ▼
┌───────────────────────────────────┐
│         Segmented FIFO Queue      │
│  ┌────────┐  ┌────────┐           │
│  │ Seg 0  │→ │ Seg 1  │→  ...     │
│  │ bitmap │  │ bitmap │           │
│  │ CAS ix │  │ CAS ix │           │
│  └────────┘  └────────┘           │
│         ↑ recycled via pool       │
└──────────────────┬────────────────┘
                   │ batch drain
        ┌──────────▼──────────┐
        │   Batch Scheduler   │
        │  (timer + threshold)│
        └──────┬──────────────┘
               │
       ┌───────┴────────┐
       ▼                ▼
   Worker 0  ...   Worker N-1
```

Each queue segment holds a fixed-size job buffer with a **readiness bitmap**. Producers reserve slots via CAS; consumers drain contiguous ready ranges as a single batch — keeping cache lines warm and synchronization amortized.

---

## Benchmarks

All results: AMD Ryzen 7 8845HS · Go 1.22 · Linux · 1 producer goroutine (`p=1`).

### Empty job (scheduler + queue overhead only)

| Workers | ns/op | Throughput | allocs/op |
|---------|------:|----------:|----------:|
| w=1 | 93 | ~10.7 Mj/s | 0 |
| w=2 | 115 | ~8.7 Mj/s | 0 |
| w=4 | 146 | ~6.9 Mj/s | 0 |
| w=8 | 261 | ~3.8 Mj/s | 0 |
| w=16 | 855 | ~1.2 Mj/s | 0 |

### SHA-256 job (~4 µs CPU work)

| Workers | ns/op | Throughput | allocs/op |
|---------|------:|----------:|----------:|
| w=1 | 4379 | 228 kj/s | 0 |
| w=2 | 2267 | 441 kj/s | 0 |
| w=4 | 1200 | 833 kj/s | 0 |
| w=8 | 683 | 1463 kj/s | 0 |
| w=16 | 557 | 1794 kj/s | 0 |

### CPU-bound job (~40 µs)

| Workers | ns/op | Throughput | allocs/op |
|---------|------:|----------:|----------:|
| w=1 | 40939 | 24.4 kj/s | 0 |
| w=4 | 11064 | 90.4 kj/s | 0 |
| w=8 | 7395 | 135 kj/s | 0 |
| w=16 | 5927 | 169 kj/s | 0 |

The pool adds near-zero overhead on the submission path. For CPU-bound or IO-bound jobs, throughput scales with worker count as expected.

> Note: throughput *decreases* as workers increase for empty jobs. This is expected — with no real work, more workers means more scheduler/synchronization overhead without any benefit. The zero-allocation property holds across all scenarios.

---

## Known Limitations

Memory reclamation in lock-free data structures is a hard problem. `wpool` currently uses a **heuristic "limbo" approach**: segments that may still be referenced are deferred and reclaimed lazily based on observed state rather than precise reference counting (e.g. hazard pointers or epoch-based reclamation).

**This has a known failure mode:**

> With very small segments (e.g. `SegmentSize=1`) and a small segment pool (`SegmentCount`), the limbo queue can exhaust available segments under load, causing submission to stall or fail.

**Recommended configuration:** use `SegmentSize ≥ 64` and size `SegmentCount` generously relative to your worker count. The defaults are chosen to avoid this in typical use.

**Status:** memory reclamation is under active investigation. The goal is to find a provably safe approach that fits Go's memory model without introducing significant overhead. Contributions and ideas welcome.

---

## Features

| Feature | Details |
|---|---|
| **Lock-free queue** | Segmented MPMC FIFO, CAS-based reservation |
| **Batch scheduling** | Amortized wakeups, better cache locality |
| **Zero allocations** | Segment pool with generation counters |
| **Bounded concurrency** | Fixed goroutine count, no unbounded spawning |
| **Context-aware jobs** | `context.Context` checked before execution |
| **Panic-safe workers** | Panics are isolated per worker |
| **Graceful shutdown** | Deadline-aware draining via `pool.Shutdown(ctx)` |
| **Pluggable metrics** | Interface injection keeps the hot path clean |
| **CPU affinity** | Optional Linux worker pinning |

---

## Job Model

```go
type Job[T any] struct {
    Payload     T
    Fn          func(T) error
    Ctx         context.Context
    CleanupFunc func()         // always runs after execution
}
```

Jobs are generic — no interface boxing on the submission path.

---

## Graceful Shutdown

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

if err := pool.Shutdown(ctx); err != nil {
    log.Println("shutdown timed out:", err)
}
```

Shutdown blocks new submissions, drains queued work, and waits for in-flight jobs to complete — or until the deadline expires.

---

## Metrics

The metrics interface is injected at pool construction, keeping instrumentation off the critical path:

```go
type MetricsPolicy interface {
    IncQueued()
    BatchDecQueued(n int)
}
```

Use `wp.NoopMetrics{}` for zero overhead, or plug in Prometheus counters, etc.

---

## Configuration

```go
pool := wp.NewPool(
    metrics,
    wp.WithWorkers(runtime.GOMAXPROCS(0)),
    wp.WithSegmentSize(4096),   // jobs per segment
    wp.WithSegmentCount(64),    // pre-allocated segments
)
```

Defaults are applied automatically via `FillDefaults()` for any unset options.

---

## Roadmap

- [ ] **Safe memory reclamation** — replace limbo heuristic with hazard pointers or epoch-based reclamation *(active)*
- [ ] Bucket-based priority scheduler
- [ ] Queue aging / rotation
- [ ] Adaptive segment provisioning
- [ ] NUMA-aware worker placement

---

## When to use wpool

**Suitable for:**
- Experimentation, benchmarking, and learning about lock-free queue design
- Internal tools where you control configuration and can tolerate edge cases
- High-frequency, short-lived jobs where allocation overhead matters

**Not suitable for:**
- Production systems requiring proven memory safety guarantees
- Extreme segment configurations (`SegmentSize=1`) — see Known Limitations
- Long-running background tasks (use a dedicated goroutine)
- Dynamic worker scaling

---

## License

[MIT](LICENSE)