package workerpool

import (
	"golang.org/x/sys/cpu"
	"runtime"
	"sync/atomic"
)

const (
	// DefaultSegmentSize is the number of jobs stored in a single segment.
	// It must remain stable for the lifetime of a segmented queue.
	//
	// Larger values reduce segment churn but increase cache pressure
	// and batch scan cost.
	DefaultSegmentSize = 4096
)

// DefaultSegmentCount defines how many segments are preallocated when
// creating a segmented queue.
//
// The default scales with GOMAXPROCS to reduce contention and amortize
// allocation cost under parallel producers.
var DefaultSegmentCount uint32 = uint32(runtime.GOMAXPROCS(0) * 16)

type segment[T any] struct {
	head    uint32
	_       cpu.CacheLinePad
	tail    uint32
	_       cpu.CacheLinePad
	reserve uint32
	_       cpu.CacheLinePad
	next    atomic.Pointer[segment[T]]
	_       cpu.CacheLinePad
	
	buf   []Job[T]
	ready []uint32
}

// segmentedQ is a lock-free, append-only segmented queue.
//
// It supports multiple concurrent producers and a single or multiple
// consumers calling BatchPop.
//
// The queue grows by linking new segments when capacity is exhausted.
// Segments are never reused once fully consumed.
type segmentedQ[T any] struct {
	// head points to the segment currently being consumed.
	head     atomic.Pointer[segment[T]]

	// tail points to the segment currently being appended to.	
	tail     atomic.Pointer[segment[T]]

	// pageSize is the fixed capacity of each segment.
	pageSize uint32
}

// mkSegment allocates and initializes a new empty segment
// with the given capacity.
func mkSegment[T any](segSize uint32) *segment[T] {
	return &segment[T]{
		buf:   make([]Job[T], segSize),
		ready: make([]uint32, segSize),
	}
}

// NewSegmentedQ creates a new segmented queue with fixed-size segments.
//
// segSize defines the number of jobs per segment.
// segCount defines how many segments are eagerly preallocated and linked.
//
// The queue supports concurrent producers without locks.
// Consumers retrieve work using BatchPop.
func NewSegmentedQ[T any](segSize uint32, segCount uint32) *segmentedQ[T] {
	q := &segmentedQ[T]{
		pageSize: segSize,
	}

	var first, prev *segment[T]

	for range segCount {
		s := mkSegment[T](segSize)
		if prev != nil {
			prev.next.Store(s)
		} else {
			first = s
		}
		prev = s
	}

	q.head.Store(first)
	q.tail.Store(first)
	return q
}

// Push appends a job to the queue.
//
// Push is lock-free and safe for concurrent use by multiple producers.
// It returns true once the job has been fully published and is visible
// to consumers.
//
// The call may allocate a new segment if the current tail segment
// becomes full.
func (q *segmentedQ[T]) Push(v Job[T]) bool {
	for {
		seg := q.tail.Load()

		// reserve slot
		for {
			r := atomic.LoadUint32(&seg.reserve)
			if r >= q.pageSize {
				break
			}
			if atomic.CompareAndSwapUint32(&seg.reserve, r, r+1) {
				seg.buf[r] = v
				atomic.StoreUint32(&seg.ready[r], 1)
				return true
			}
		}

		// segment full -> ensure next and advance tail
		next := seg.next.Load()
		if next == nil {
			newSeg := mkSegment[T](q.pageSize)
			if seg.next.CompareAndSwap(nil, newSeg) {
				next = newSeg
			} else {
				next = seg.next.Load()
			}
		}
		q.tail.CompareAndSwap(seg, next)
	}
}

// BatchPop returns a contiguous batch of ready jobs from the head segment.
//
// The returned slice is backed by the queue's internal buffer and must
// be treated as read-only.
//
// The boolean result reports whether any jobs were returned.
// If false, the queue is currently empty or no ready jobs are available.
//
// BatchPop is lock-free and may be called concurrently by multiple consumers,
// though typical usage assumes a single draining goroutine.
func (q *segmentedQ[T]) BatchPop() ([]Job[T], bool) {
	for {
		seg := q.head.Load()
		h := atomic.LoadUint32(&seg.head)

		//avoids scanning empty tail space.
		r := atomic.LoadUint32(&seg.reserve)
		limit := min(r, q.pageSize)

		// Scan forward while slots are ready
		end := h
		for end < limit && atomic.LoadUint32(&seg.ready[end]) == 1 {
			end++
		}

		// Return a batch if we found any ready items
		if end > h {
			if atomic.CompareAndSwapUint32(&seg.head, h, end) {
				return seg.buf[h:end], true
			}
			continue
		}

		// No ready items at current head.
		if h < q.pageSize {
			return nil, false
		}

		// segment fully consumed
		next := seg.next.Load()
		if next == nil {
			return nil, false
		}
		q.head.CompareAndSwap(seg, next)
	}
}

// Len should returns an approximate number of jobs in the queue.
//
func (q *segmentedQ[T]) Len() int { return 0 }
