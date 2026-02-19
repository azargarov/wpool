
# wpool — a Worker Pool for Go

`wpool` is a bounded-concurrency worker pool for Go.

It separates **execution** from **scheduling policy**, allowing different queue
strategies (FIFO or priority-based) to be plugged into the same execution engine.

Module:

    github.com/azargarov/wpool


---

## Design Philosophy

This project evolved through iterative refinement:

- Started as a simple worker pool
- Improved batching
- Introduced lock-free segmented queue
- Added pluggable scheduling
- Implemented Revolving Bucket Queue (RBQ) with aging

The result is a policy-extensible execution engine.

---

## Architecture Overview

The pool depends on a minimal scheduling interface:

```go
type schedQueue[T any] interface {
    Push(job Job[T]) error
    BatchPop() (Batch[T], bool)
    OnBatchDone(b Batch[T])
    StatSnapshot() string  // for debuggin reason
    Close()
}
```

Execution logic is independent of scheduling policy.

---

## Default Scheduler: SegmentedQueue (FIFO)

Zero-value default.

Characteristics:

- Lock-free segmented FIFO
- Multiple producers (MPMC safe)
- Batch-based draining
- Ignores submitted priority
- Optimized for throughput
- 0 allocs in hot path

If `Options.QT` is not set, FIFO is used.

---

## Optional Scheduler: RevolvingBucketQueue (RBQ)

RBQ must be explicitly enabled:

```go
opts.QT = wp.RevolvingBucketQueue
```

### RBQ Model

- 64 priority buckets (0–63)
- Bucket 0 is the active bucket
- After draining bucket 0, rotation occurs
- Priority `p` means eligibility after `p` rotations
- Aging is implicit via rotation
- Starvation is prevented

Semantics:

- Lower bucket index = higher effective priority
- Old low-priority jobs eventually outrank new high-priority jobs

RBQ is experimental and scheduling semantics may evolve in later releases.

---

## Performance Snapshot

Measured on AMD Ryzen 7 8845HS, Go 1.22, Linux.

Typical steady-state throughput:

    ~22–23 M jobs/sec
    ~43 ns/op
    0 allocs/op

These figures represent queue + scheduler overhead with minimal job body.

---

## Options

```go
type Options struct {
    Workers      int
    QT           QueueType
    SegmentSize  uint32
    SegmentCount uint32
    PoolCapacity uint32
    PinWorkers   bool
}
```

Defaults:

- Workers → GOMAXPROCS
- QT → SegmentedQueue (FIFO)
- SegmentSize → DefaultSegmentSize
- SegmentCount → DefaultSegmentCount

---

## Job Model

```go
type Job[T any] struct {
    Payload T
    Fn      JobFunc[T]
    Flags   uint64
    Meta    *JobMeta
}
```

Priority is encoded in lower 6 bits of `Flags`.

```go
func (j *Job[T]) SetPriority(p JobPriority)
func (j Job[T]) GetPriority() JobPriority
```

SegmentedQueue ignores priority.
RBQ uses it for scheduling.

---

## Shutdown

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

if err := pool.Shutdown(ctx); err != nil {
    // deadline exceeded
}
```

- Prevents new submissions
- Drains queued work
- Waits for workers or deadline

---

## Status

Current version: 

- Core architecture stable
- FIFO scheduler stable
- RBQ scheduling semantics experimental
- API may evolve before v1.0.0

---

## Motivation

This project was built from fascination with system design, concurrency,
and performance engineering.

It is both:

- A high-performance execution engine
- A playground for exploring scheduling strategies

Contributions and feedback are welcome.