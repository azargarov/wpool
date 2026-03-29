//go:build !debug

package workerpool

type segmentPoolMetricsSnapshot struct{}

type segmentPoolMetrics struct{}

func (m *segmentPoolMetrics) IncAllocated() {}
func (m *segmentPoolMetrics) IncReused() {}
func (m *segmentPoolMetrics) IncPut() {}
func (m *segmentPoolMetrics) IncPutAttempt() {}
func (m *segmentPoolMetrics) Reset()              {}
func (m *segmentPoolMetrics) Snapshot() segmentPoolMetricsSnapshot {
	return segmentPoolMetricsSnapshot{}
}
func (s segmentPoolMetricsSnapshot) String() string { return "" }