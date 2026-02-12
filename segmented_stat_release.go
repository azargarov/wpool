//go:build !debug

package workerpool

type segmentPoolMetricsSnapshot struct {}

type segmentPoolMetrics struct{}
func (m *segmentPoolMetrics) IncFastGetHit() {}
func (m *segmentPoolMetrics) IncFastGetMiss() {}
func (m *segmentPoolMetrics) IncFallbackAlloc() {}
func (m *segmentPoolMetrics) IncFastPutHit() {}
func (m *segmentPoolMetrics) IncFastPutDrop() {}
func (m *segmentPoolMetrics) IncRefillSignal() {}
func (m *segmentPoolMetrics) IncRefillCall() {}
func (m *segmentPoolMetrics) IncRefillFromFree() {}
func (m *segmentPoolMetrics) IncRefillAllocated() {}
func (m *segmentPoolMetrics) Reset() {}
func (m *segmentPoolMetrics) Snapshot() segmentPoolMetricsSnapshot {
	return segmentPoolMetricsSnapshot{}
}
func (s segmentPoolMetricsSnapshot) String() string {return ""}