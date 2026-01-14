package workerpool

import (
	"runtime"
)

// QueueType defines the scheduling strategy used by the worker pool.
//
// Different queue types determine how jobs are ordered and selected
// for execution by the scheduler. The type is configured via
// Options.QueueType when creating a new Pool.
type QueueType int

type PopType int

const (
	SegmentedQueue QueueType = iota
)

const (
	SerialPop PopType = iota

	BatchPop
)

// Options configure a worker Pool.
//
// All zero values are replaced with sensible defaults in fillDefaults.
type Options struct {
	Workers int

	SegmentSize  int
	SegmentCount uint32

	QT QueueType

	PinWorkers bool
}

func (o *Options) FillDefaults() {
	if o.Workers <= 0 {
		o.Workers = runtime.GOMAXPROCS(0)
	}
	if o.QT == 0 {
		o.QT = SegmentedQueue
	}
	if o.SegmentSize <= 0 {
		o.SegmentSize = DefaultSegmentSize
	}
	if o.SegmentCount <= 0 {
		o.SegmentCount = DefaultSegmentCount
	}

}

func (qt QueueType) String() string {
	switch qt {
	case SegmentedQueue:
		return "SegmentedQueue"
	default:
		return "Unknown"
	}
}
