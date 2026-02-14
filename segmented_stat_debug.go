//go:build debug

package workerpool

import(
	"sync/atomic"
	"fmt"
)

type segmentPoolMetricsSnapshot struct {
	FastGetHits     uint64
	FastGetMisses   uint64
	FallbackAllocs  uint64
	FastPutHits     uint64
	FastPutToBuf    uint64
	FastPutDrops    uint64
	RefillSignals   uint64
	RefillCalls     uint64
	RefillFromFree  uint64
	RefillAllocated uint64
}

type segmentPoolMetrics struct{

	fastGetHits     atomic.Uint64
    fastGetMisses   atomic.Uint64
    fallbackAllocs  atomic.Uint64
    fastPutHits     atomic.Uint64
	fastPutToBuf    atomic.Uint64
    fastPutDrops    atomic.Uint64
    refillSignals   atomic.Uint64
    refillCalls     atomic.Uint64
    refillFromFree  atomic.Uint64
    refillAllocated atomic.Uint64

}

func (m *segmentPoolMetrics) IncFastGetHit() {
	m.fastGetHits.Add(1)
}

func (m *segmentPoolMetrics) IncFastGetMiss() {
	m.fastGetMisses.Add(1)
}

func (m *segmentPoolMetrics) IncFallbackAlloc() {
	m.fallbackAllocs.Add(1)
}

func (m *segmentPoolMetrics) IncFastPutHit() {
	m.fastPutHits.Add(1)
}
func (m *segmentPoolMetrics) IncFastPutToBuf() {
	m.fastPutToBuf.Add(1)
}
func (m *segmentPoolMetrics) IncFastPutDrop() {
	m.fastPutDrops.Add(1)
}

func (m *segmentPoolMetrics) IncRefillSignal() {
	m.refillSignals.Add(1)
}

func (m *segmentPoolMetrics) IncRefillCall() {
	m.refillCalls.Add(1)
}

func (m *segmentPoolMetrics) IncRefillFromFree() {
	m.refillFromFree.Add(1)
}

func (m *segmentPoolMetrics) IncRefillAllocated() {
	m.refillAllocated.Add(1)
}


func (m *segmentPoolMetrics) Reset() {
	m.fastGetHits.Store(0)
	m.fastGetMisses.Store(0)
	m.fallbackAllocs.Store(0)
	m.fastPutHits.Store(0)
	m.fastPutToBuf.Store(0)
	m.fastPutDrops.Store(0)
	m.refillSignals.Store(0)
	m.refillCalls.Store(0)
	m.refillFromFree.Store(0)
	m.refillAllocated.Store(0)
}

func (s segmentPoolMetricsSnapshot) FastGetHitRatio() float64 {
	total := s.FastGetHits + s.FastGetMisses
	if total == 0 {
		return 0
	}
	return float64(s.FastGetHits) / float64(total)
}

func (s segmentPoolMetricsSnapshot) FastGetTotal() uint64 {
	return s.FastGetHits + s.FastGetMisses
}

func (s segmentPoolMetricsSnapshot) SlowPathRate() float64 {
	total := s.FastGetTotal()
	if total == 0 {
		return 0
	}
	return float64(s.FallbackAllocs) / float64(total)
}

func (m *segmentPoolMetrics) Snapshot() segmentPoolMetricsSnapshot {
	return segmentPoolMetricsSnapshot{
		FastGetHits:     m.fastGetHits.Load(),
		FastGetMisses:   m.fastGetMisses.Load(),
		FallbackAllocs:  m.fallbackAllocs.Load(),
		FastPutHits:     m.fastPutHits.Load(),		
		FastPutToBuf:    m.fastPutToBuf.Load(),
		FastPutDrops:    m.fastPutDrops.Load(),
		RefillSignals:   m.refillSignals.Load(),
		RefillCalls:     m.refillCalls.Load(),
		RefillFromFree:  m.refillFromFree.Load(),
		RefillAllocated: m.refillAllocated.Load(),
	}
}

func (s segmentPoolMetricsSnapshot) String() string {
	totalGets := s.FastGetTotal()
	totalPuts := s.FastPutHits + s.FastPutDrops

	return fmt.Sprintf(
 `SegmentPool Metrics:
  GET:    TotalGets: %d, FastHits: %d, FastMisses: %d, HitRatio: %.2f%%, SlowPathRate: %.2f%%
  PUT:    TotalPuts: %d, FastPutHits: %d,FastPutToBuf: %d ,FastPutDrops: %d
  REFILL: RefillSignals: %d, RefillCalls: %d, FromFree: %d, Allocated: %d `,
		totalGets,
		s.FastGetHits,
		s.FastGetMisses,
		s.FastGetHitRatio()*100,
		s.SlowPathRate()*100,

		totalPuts,
		s.FastPutHits,
		s.FastPutToBuf,
		s.FastPutDrops,

		s.RefillSignals,
		s.RefillCalls,
		s.RefillFromFree,
		s.RefillAllocated,
	)
}

func (s segmentPoolMetricsSnapshot) CompactString() string {
	return fmt.Sprintf(
		"GET hit=%.2f%% slow=%.2f%% | PUT drops=%d | refill alloc=%d",
		s.FastGetHitRatio()*100,
		s.SlowPathRate()*100,
		s.FastPutDrops,
		s.RefillAllocated,
	)
}
