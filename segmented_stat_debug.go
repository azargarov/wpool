//go:build debug

package workerpool

import (
	"sync/atomic"
)

var (
	allocated   atomic.Int64
	recycled    atomic.Int64
	consumed    atomic.Int64
	casMiss     atomic.Int64
	casAttempt  atomic.Int64
)

type Stats struct {
	Allocated  int64
	Recycled   int64
	Consumed   int64
	CASMiss    int64
}

func statAllocated()  { allocated.Add(1) }
func statRecycled()   { recycled.Add(1) }
func statConsumed()   { consumed.Add(1) }
func statCASMiss()    { casMiss.Add(1) }

func SnapshotStats() Stats {
	return Stats{
		Allocated:  allocated.Load(),
		Recycled:   recycled.Load(),
		Consumed:   consumed.Load(),
		CASMiss:    casMiss.Load(),
	}
}

func PrintStat() {

	println(
		"allocated / recycled / consumed / CAS misses :",
		allocated.Load(),
		recycled.Load(),
		consumed.Load(),
		casMiss.Load(),

	)
}