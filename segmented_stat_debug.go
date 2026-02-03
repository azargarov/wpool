//go:build debug

package workerpool

import (
	"sync/atomic"
)

// Debug statistics are enabled only when built with the "debug" tag.
//
// These counters are intended for performance analysis and debugging
// of queue behavior under load. They are NOT part of the stable API
// and should not be relied upon in production builds.
var (
	allocated  atomic.Int64 // number of newly allocated segments
	recycled   atomic.Int64 // number of segments returned to the pool
	consumed   atomic.Int64 // number of segments reused from the pool
	casMiss    atomic.Int64 // number of failed CAS operations
	casAttempt atomic.Int64 // number of attempted CAS operations (optional)
)

// Stats represents a snapshot of internal debug counters.
//
// Values are approximate and intended for trend analysis rather
// than exact accounting.
type Stats struct {
	Allocated int64 // segments allocated from the heap
	Recycled  int64 // segments recycled back into the pool
	Consumed  int64 // segments taken from the pool
	CASMiss   int64 // failed CAS attempts under contention
}

func statAllocated() { allocated.Add(1) }
func statRecycled()  { recycled.Add(1) }
func statConsumed()  { consumed.Add(1) }
func statCASMiss()   { casMiss.Add(1) }

// SnapshotStats returns a point-in-time snapshot of debug statistics.
func SnapshotStats() Stats {
	return Stats{
		Allocated: allocated.Load(),
		Recycled:  recycled.Load(),
		Consumed:  consumed.Load(),
		CASMiss:   casMiss.Load(),
	}
}

// PrintStat prints a human-readable summary of debug statistics.
//
// Intended for quick inspection during benchmarks or experiments.
func PrintStat() {
	println(
		"allocated / recycled / consumed / CAS misses :",
		allocated.Load(),
		recycled.Load(),
		consumed.Load(),
		casMiss.Load(),
	)
}
