//go:build debug

package workerpool

import (
	"fmt"
	"sync/atomic"
)

type segmentPoolMetricsSnapshot struct {
	allocated  uint64
	reused     uint64
	put        uint64
	putAttempt uint64
}

type segmentPoolMetrics struct {
	allocated  atomic.Uint64
	reused     atomic.Uint64
	put        atomic.Uint64
	putAttempt atomic.Uint64
}

func (m *segmentPoolMetrics) IncAllocated() {
	m.allocated.Add(1)
}

func (m *segmentPoolMetrics) IncReused() {
	m.reused.Add(1)
}
func (m *segmentPoolMetrics) IncPut() {
	m.put.Add(1)
}

func (m *segmentPoolMetrics) IncPutAttempt() {
	m.putAttempt.Add(1)
}

func (m *segmentPoolMetrics) Reset() {
	m.allocated.Store(0)
	m.reused.Store(0)
	m.put.Store(0)
	m.putAttempt.Store(0)
}

func (m *segmentPoolMetrics) Snapshot() segmentPoolMetricsSnapshot {
	return segmentPoolMetricsSnapshot{
		allocated:  m.allocated.Load(),
		reused:     m.reused.Load(),
		put:        m.put.Load(),
		putAttempt: m.putAttempt.Load(),
	}
}

func (s segmentPoolMetricsSnapshot) String() string {

	return fmt.Sprintf(
		"SegmentPool Metrics: Allocated: %d, Recycled: %d, Put: %d, PutAttempt: %d \n",
		s.allocated,
		s.reused,
		s.put,
		s.putAttempt,
	)
}
