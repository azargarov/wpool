package workerpool

import (
	"time"
)

// QueueType defines the scheduling strategy used by the worker pool.
//
// Different queue types determine how jobs are ordered and selected
// for execution by the scheduler. The type is configured via
// Options.QueueType when creating a new Pool.
type QueueType int

type PopType int

const (
	// Fifo represents a simple first-in–first-out queue.
	// Jobs are executed strictly in the order they are submitted.
	Fifo QueueType = iota

	// Conditional is a placeholder for future scheduling strategies
	// where dispatching logic may depend on job state or custom
	// conditions. Currently behaves the same as Fifo.
	Conditional

	// BucketQueue selects the fixed-range, O(1) bucket-based priority queue.
	// Jobs are assigned to one of 100 priority buckets based on:
	//
	//     effective = basePriority - agingRate * seq
	//
	// where seq is a monotonically increasing insertion counter.
	// This scheduler offers extremely fast push/pop, predictable behavior,
	// and static aging without periodic rebalancing.
	BucketQueue

	SegmentedQueue

	ShardedQueue
)

const (
	SerialPop PopType = iota

	BatchPop 
)

// Options configure a worker Pool.
//
// All zero values are replaced with sensible defaults in fillDefaults.
type Options struct {
	// Workers is the number of worker goroutines processing jobs.
	Workers int

	// AgingRate controls how fast queued jobs grow in effective priority.
	// Higher values mean old jobs will be pulled to the top sooner.
	AgingRate int

	// RebuildDur is how often the scheduler re-computes effective priorities
	// for all queued jobs.
	RebuildDur time.Duration

	// QueueSize is the capacity of the submit channel — how many jobs can
	// wait in front of the scheduler.
	QueueSize int

	// QT selects the scheduler queue type (FIFO, Priority, or Conditional).
	//
	// Use Fifo for simple first-in–first-out execution,
	// Priority to enable aging-based priority scheduling,
	// or Conditional for custom or experimental logic.
	QT QueueType

	PinWorkers bool

}

func (o *Options) FillDefaults() {
	if o.Workers <= 0 {
		o.Workers = 4
	}
	if o.AgingRate <= 0 {
		o.AgingRate = 3
	}
	//if o.RebuildDur <= 0 {
	//	o.RebuildDur = 200 * time.Millisecond
	//}
	if o.QueueSize <= 0 {
		o.QueueSize = 256
	}
	if o.QT == 0 {
		o.QT = Fifo
	}

}

func (qt QueueType) String() string {
	switch qt {
	case Fifo:
		return "Fifo"
	case Conditional:
		return "Conditional"
	case BucketQueue:
		return "BucketQueue"	
	case ShardedQueue:
		return "ShardedQueue"
	case SegmentedQueue:
		return "SegmentedQueue"
	default:
		return "Unknown"
	}
}