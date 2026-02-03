// Package workerpool provides high-performance concurrency primitives
// for building scalable worker pools and schedulers.
//
// Design goals
//
// The package is designed around the following principles:
//
//   - Minimize allocations and garbage collection pressure
//   - Avoid locks on hot paths
//   - Reduce scheduler and wake-up overhead
//   - Provide predictable throughput under high contention
//
// Rather than optimizing for minimal latency of a single task,
// workerpool optimizes for sustained throughput and stability
// when handling large volumes of short-lived jobs.
//
// Architecture overview
//
// The worker pool is composed of three loosely coupled layers:
//
//   1. Scheduling (schedQueue)
//      Responsible for ordering, batching, and dequeuing jobs.
//      Different queue implementations may be plugged in without
//      modifying the pool or worker logic.
//
//   2. Execution (Pool / workers)
//      Workers fetch batches of jobs and execute them sequentially.
//      Parallelism is achieved across workers, not within a batch.
//
//   3. Job lifecycle
//      Jobs carry their payload, execution function, optional context,
//      and optional cleanup logic.
//
// Batching model
//
// Jobs are dequeued in batches to amortize scheduling overhead such as
// atomic operations, cache misses, and worker wake-ups.
//
// Important: batching amortizes scheduling, not execution.
//
// Jobs within a batch are executed sequentially by a single worker.
// This preserves cache locality, avoids goroutine churn, and keeps
// execution costs predictable.
//
// Parallelism is achieved by running multiple workers concurrently,
// each processing its own batches.
//
// Queue design
//
// The default scheduler uses a lock-free segmented FIFO queue.
// Jobs are stored in fixed-size segments linked together dynamically.
//
// Key properties of the segmented queue:
//
//   - Multiple producers can enqueue concurrently
//   - Consumers dequeue contiguous batches
//   - Memory is aggressively reused via segment recycling
//   - Generation counters prevent ABA issues without clearing buffers
//
// The queue design is optimized for workloads with many producers
// and relatively small, fast jobs.
//
// Error handling
//
// The pool distinguishes between two classes of errors:
//
//   - Job errors: returned by job functions or produced by panic recovery
//   - Internal errors: unexpected failures inside the pool itself
//
// Errors are reported via user-provided handlers and do not stop
// worker execution. Panics inside jobs are recovered to prevent
// worker termination.
//
// CPU pinning
//
// On Linux, workers may optionally be pinned to specific CPUs.
// When enabled, workers are locked to OS threads and restricted
// to run on a single CPU core.
//
// This can improve cache locality and reduce scheduler-induced
// migration for CPU-bound workloads, but is not universally beneficial.
//
// Intended use cases
//
// workerpool is well suited for:
//
//   - High-throughput task execution
//   - Fan-in / fan-out pipelines
//   - CPU-bound or cache-sensitive workloads
//   - Systems where allocation behavior matters
//
// It is not intended as a general-purpose goroutine replacement
// or for workloads dominated by blocking I/O.
//
// Extensibility
//
// The scheduling layer is intentionally abstracted to allow
// experimentation with alternative queue designs, such as:
//
//   - Priority queues
//   - Bucket-based schedulers
//   - Time-sliced or aging queues
//
// New queue types can be introduced without changing the worker
// execution model or public API.
package workerpool
