# Changelog

All notable changes to this project will be documented in this file.

The format is inspired by [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

---

## [v0.2.1] - 2025-11-09
### Added
- Introduced unified scheduling interface `schedQueue[T]` for pluggable queue strategies.
- Added new queue type `fifoQueue` (first-in-first-out scheduling).
- Added new queue type `prioQueue` with time-based aging for priority scheduling.
- Added `QueueType` enum (`Fifo`, `Priority`, `Conditional`) to `Options`.
- Added `QT` field in `Options` to select the scheduling mode.
- Implemented `FillDefaults()` for `Options` with new defaults.
- Added full GoDoc-style documentation for all internal types and functions.

### Changed
- `scheduler()` logic refactored to use `schedQueue` interface instead of separate implementations.
- Improved metrics tracking (`MaxAge`, `Queued`, `Submitted`).
- Simplified shutdown logic with unified queue handling.

### Fixed
- Removed redundant `effective()` method on `Pool`.

## [v0.2.0] - 2025-11-09
[Compare changes](https://github.com/azargarov/go-utils/compare/wpool/v0.1.6...wpool/v0.2.0)

### Added
- **Priority scheduler** in front of worker goroutines.
- **Job aging** with configurable `AgingRate` and `RebuildDur`.
- **Metrics:** submitted, executed, active workers, queue length, max age.
- **Per-job retry overrides** for individual job control.
- **Context-aware backoff** during retries.

### Changed
- **Breaking:** `NewPool` now accepts an `Options` struct instead of an integer worker count.  
  Example migration:  
  ```go
  // old
  pool := workerpool.NewPool(4, retry)
  // new
  pool := workerpool.NewPool(workerpool.Options{Workers: 4}, retry)

## [0.1.0] - 2025-11-01
### Added
- Basic bounded worker pool.
- Per-job cleanup and panic recovery.
- Graceful shutdown with deadline.