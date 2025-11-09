// Package workerpool provides a bounded-concurrency worker pool with
// priority scheduling, aging (to prevent starvation), per-job retry
// policies, context-aware backoff, panic-safe workers, and basic metrics.
//
// Jobs are submitted together with a base priority; the scheduler keeps
// them in a heap and periodically "ages" them so old low-priority jobs
// still get executed.
package workerpool
