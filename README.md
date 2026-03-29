# wpool

`wpool` is a Go worker pool for bounded-concurrency job execution.

It is designed for high-throughput workloads with many concurrent producers. Internally, it uses a segmented FIFO queue and batch-based draining to reduce coordination overhead.

## What it does

- runs jobs with a fixed number of worker goroutines
- accepts concurrent submissions from many producers
- uses a segmented FIFO queue for scheduling
- drains work in batches to improve throughput
- supports graceful shutdown with `context.Context`
- supports optional metrics collection
- supports optional worker pinning on Linux

## Installation

```bash
go get github.com/azargarov/wpool
```

## Basic example

```go
package main

import (
    "context"
    "fmt"
    "time"

    wp "github.com/azargarov/wpool"
)

func main() {
    pool := wp.NewPoolFromOptions[*wp.NoopMetrics, int](
        &wp.NoopMetrics{},
        wp.Options{
            Workers:      4,
            QT:           wp.SegmentedQueue,
            SegmentSize:  1024,
            SegmentCount: 64,
            PoolCapacity: 256,
        },
    )

    for i := 0; i < 10; i++ {
        v := i
        err := pool.Submit(wp.Job[int]{
            Payload: v,
            Fn: func(x int) error {
                fmt.Println("job:", x)
                return nil
            },
        })
        if err != nil {
            panic(err)
        }
    }

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    if err := pool.Shutdown(ctx); err != nil {
        panic(err)
    }
}
```

## Creating a pool

You can construct a pool with functional options:

```go
pool := wp.NewPool(
    &wp.NoopMetrics{},
    wp.WithWorkers(4),
    wp.WithQT(wp.SegmentedQueue),
    wp.WithSegmentSize(1024),
    wp.WithSegmentCount(64),
)
```

Or with an `Options` struct:

```go
pool := wp.NewPoolFromOptions[*wp.NoopMetrics, string](
    &wp.NoopMetrics{},
    wp.Options{
        Workers:       4,
        QT:            wp.SegmentedQueue,
        SegmentSize:   64,
        SegmentCount:  32,
        PoolCapacity:  256,
        WakeMinJobs:   16,
        FlushInterval: 50 * time.Microsecond,
    },
)
```

Zero values are filled with defaults by the constructors.

## Submitting jobs

A job has a payload, a function, and optional metadata.

```go
err := pool.Submit(wp.Job[string]{
    Payload: "hello",
    Fn: func(s string) error {
        fmt.Println(s)
        return nil
    },
})
```

Job fields:

- `Payload` — value passed to the job function
- `Fn` — function executed by a worker
- `Meta` — optional per-job context and cleanup hook
- `Flags` — internal flags, including priority bits

## Job metadata

You can attach a context and a cleanup callback per job:

```go
ctx, cancel := context.WithTimeout(context.Background(), time.Second)
defer cancel()

job := wp.Job[int]{
    Payload: 42,
    Meta: &wp.JobMeta{
        Ctx: ctx,
        CleanupFunc: func() {
            fmt.Println("cleanup")
        },
    },
    Fn: func(v int) error {
        fmt.Println(v)
        return nil
    },
}

if err := pool.Submit(job); err != nil {
    panic(err)
}
```

If the job context is already canceled before submission, `Submit` returns the context error.

## Shutdown

`Shutdown` stops accepting new jobs, closes the queue, and waits for workers to exit until the provided context expires.

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

if err := pool.Shutdown(ctx); err != nil {
    // context deadline exceeded or canceled
}
```

There is also a convenience method:

```go
pool.Stop()
```

`Stop()` calls `Shutdown(context.Background())`.

## Configuration

The main `Options` fields are:

```go
type Options struct {
    Workers       int
    SegmentSize   uint32
    SegmentCount  uint32
    PoolCapacity  uint32
    QT            QueueType
    PinWorkers    bool
    WakeMinJobs   int64
    FlushInterval time.Duration
}
```

What they mean:

- `Workers` — number of worker goroutines
- `QT` — queue type used by the pool
- `SegmentSize` — number of jobs stored in one segment
- `SegmentCount` — number of segments preallocated at startup
- `PoolCapacity` — maximum number of reusable internal segments kept in the segment pool
- `PinWorkers` — pin workers to CPUs on Linux to reduce migration
- `WakeMinJobs` — minimum pending-job threshold before eager wake-up is attempted
- `FlushInterval` — periodic interval used to trigger draining when work is pending

## Queue types

`wpool` exposes a `QueueType` enum. The supported queue to use today is:

- `SegmentedQueue` — lock-free segmented FIFO queue optimized for concurrent producers and batch-oriented consumption

The codebase may contain other queue-related experiments, but `SegmentedQueue` is the main implementation.

## Metrics

The pool accepts a metrics policy.

Built-in options:

- `NoopMetrics` — disables metrics collection
- `AtomicMetrics` — tracks queued and executed job counters

Example:

```go
metrics := &wp.AtomicMetrics{}
pool := wp.NewPoolFromOptions[*wp.AtomicMetrics, int](
    metrics,
    wp.Options{Workers: 4},
)

defer pool.Stop()

_ = pool.Submit(wp.Job[int]{
    Payload: 1,
    Fn: func(v int) error { return nil },
})

fmt.Println(metrics.String())
```

## Errors

Common submission errors:

- `ErrClosed` — the pool has already been shut down
- `ErrNilFunc` — the submitted job has a nil function
- job context error — the job context was already canceled before submission

Execution behavior:

- panics inside job functions are recovered
- cleanup callbacks still run when present
- job execution errors are reported through the pool's internal error path

## Testing

Run tests:

```bash
go test ./...
```

Run with the race detector:

```bash
go test -race ./...
```

If you use the included `Makefile`, typical commands are:

```bash
make test
make race
make bench
make lint
```

## Benchmarks

The repository includes throughput, latency, and fairness benchmarks.

Example:

```bash
go test -run=^$ -bench=. -benchmem ./...
```

## Status

`wpool` is usable and tested, but still evolving.

The main supported path today is the segmented FIFO queue with batch draining. Some parts of the codebase are experimental or not yet enabled by default.

## License

MIT