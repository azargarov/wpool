//go:build !debug

package workerpool

// Stats is a no-op placeholder in non-debug builds.
type Stats struct{}

// SnapshotStats returns an empty stats snapshot.
func SnapshotStats() Stats { return Stats{} }

func statAllocated() {}
func statRecycled()  {}
func statConsumed()  {}
func statCASMiss()   {}

// PrintStat prints nothing in non-debug builds.
func PrintStat()     {}
