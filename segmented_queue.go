package workerpool

import (
	"golang.org/x/sys/cpu"
	"runtime"
	"sync/atomic"
	"errors"
)

// cachePad is used to prevent false sharing between hot fields.
type cachePad = cpu.CacheLinePad

const (
	// DefaultSegmentSize is the default number of jobs per segment.
	// It should be large enough to amortize allocation costs but
	// small enough to fit comfortably in cache.
	DefaultSegmentSize = 4096

	DefaultFastPutGet = 1024
)

// DefaultSegmentCount defines the default number of preallocated segments.
// It scales with GOMAXPROCS to reduce contention under load.
var DefaultSegmentCount uint32 = uint32(runtime.GOMAXPROCS(0) * 16)
var (
	segErrorNilSegment = errors.New("NULL segment")
)
// producerView contains fields frequently modified by producers.
type producerView struct {
	tail    uint32
	reserve uint32
	_       cachePad
}

// consumerView contains fields frequently modified by consumers.
type consumerView struct {
	head uint32
	_    cachePad
}

// segment is a fixed-size chunk of jobs forming a node in a linked list.
//
// Segments move through the following logical states:
//
//   active   → detached → recycled
//
// Synchronization strategy:
//   - producers reserve slots using CAS
//   - consumers claim batches via head advancement
//   - generation counters prevent ABA on slot reuse
//   - refs/inflight counters ensure safe reclamation
type segment[T any] struct {
	producer producerView
	consumer consumerView

	// inflight counts how many batches are currently being processed
	// from this segment.
	inflight atomic.Int32

	// gen is a generation counter used to distinguish reused slots
	// without clearing the ready array.	
	gen      atomic.Uint32

	// detached marks the segment as removed from the queue.
	detached atomic.Uint32
	_        cachePad

	// refs counts active producers/consumers holding a reference
	// to this segment.
	refs atomic.Int32
	_    cachePad

	// buf holds job payloads.
	buf   []Job[T]

	// ready marks whether a slot belongs to the current generation.
	ready []uint32
	_     cachePad

	// next points to the next segment in the queue.
	next  atomic.Pointer[segment[T]]
}

// segmentedQ is a multi-producer, multi-consumer queue composed
// of fixed-size segments.
//
// It supports:
//   - lock-free Push
//   - batched Pop
//   - safe segment recycling
//
// The queue grows dynamically by linking new segments
// and reuses memory aggressively to reduce allocations.
type segmentedQ[T any] struct {
	head atomic.Pointer[segment[T]]

	tail atomic.Pointer[segment[T]]

	pool segmentPoolProvider[T]

	pageSize uint32
}

// mkSegment allocates and initializes a new segment.
func mkSegment[T any](segSize uint32) *segment[T] {
	seg := segment[T]{
		buf:   make([]Job[T], segSize),
		ready: make([]uint32, segSize),
	}
	seg.gen.Store(1)
	return &seg
}

// NewSegmentedQ initializes a segmented queue with preallocated segments.
func NewSegmentedQ[T any](opts Options, spool segmentPoolProvider[T]) *segmentedQ[T] {
	q := &segmentedQ[T]{pageSize: opts.SegmentSize}

	capacity := opts.PoolCapacity
	if capacity <= 0 {
		capacity = opts.SegmentCount * 2
	}
	if spool == nil{
		q.pool = NewSegmentPool[T](opts.SegmentSize, int(opts.SegmentCount), int(capacity), DefaultFastPutGet, DefaultFastPutGet) 
	} else {
		q.pool = spool
	}


	first := q.pool.Get()
	atomic.StoreUint32(&first.consumer.head, 0)
	atomic.StoreUint32(&first.producer.reserve, 0)
	first.next.Store(nil)
	first.detached.Store(0)
	first.inflight.Store(0)

	q.head.Store(first)
	q.tail.Store(first)
	return q
}

func (rq *segmentedQ[T])StatSnapshot()string{
	return rq.pool.StatSnapshot()
}

// Push enqueues a job into the queue.
//
// It is lock-free and safe for concurrent producers.
// Returns false if the queue is no longer accepting work.
func (q *segmentedQ[T]) Push(v Job[T]) error {
	for {
		seg := q.tail.Load()
		if seg == nil {
			return segErrorNilSegment
		}

		if !seg.tryAddRef() {
			continue
		}
		g := seg.gen.Load()

		if q.tail.Load() != seg {
			seg.refs.Add(-1)
			continue
		}

		for {
			r := atomic.LoadUint32(&seg.producer.reserve)
			if r >= q.pageSize {
				break
			}
			if atomic.CompareAndSwapUint32(&seg.producer.reserve, r, r+1) {
				seg.buf[r] = v
				atomic.StoreUint32(&seg.ready[r], g)
				seg.refs.Add(-1)
				return nil
			}
		}

		next := seg.next.Load()
		if next == nil {
			newSeg := q.pool.Get()
			if seg.next.CompareAndSwap(nil, newSeg) {
				next = newSeg
			} else {
				q.pool.Put(newSeg)
				next = seg.next.Load()
			}
		}

		q.tail.CompareAndSwap(seg, next)
		seg.refs.Add(-1)
	}
}

// BatchPop dequeues a contiguous batch of jobs.
//
// The returned Batch must be completed via OnBatchDone
// to allow safe segment recycling.
func (q *segmentedQ[T]) BatchPop() (Batch[T], bool) {
	for {
		seg := q.head.Load()
		if seg == nil {
			return Batch[T]{}, false
		}

		if !seg.tryAddRef() {
			continue
		}
		if q.head.Load() != seg {
			seg.refs.Add(-1)
			continue
		}

		h := atomic.LoadUint32(&seg.consumer.head)
		r := atomic.LoadUint32(&seg.producer.reserve)
		limit := min(r, q.pageSize)

		end := h
		g := seg.gen.Load()
		for end < limit && atomic.LoadUint32(&seg.ready[end]) == g {
			end++
		}

		if end > h {
			if atomic.CompareAndSwapUint32(&seg.consumer.head, h, end) {
				seg.inflight.Add(1)
				seg.refs.Add(-1)
				return Batch[T]{Jobs: seg.buf[h:end], Seg: seg, End: end}, true
			}
			seg.refs.Add(-1)
			continue
		}

		if h == limit {
			next := seg.next.Load()
			if next != nil {
				if q.head.CompareAndSwap(seg, next) {
					if seg.detached.CompareAndSwap(0, 1) {
						seg.refs.Add(-1)
						q.tryRecycle(seg)
						continue
					}
				}
				seg.refs.Add(-1)
				continue
			}
		}

		seg.refs.Add(-1)
		return Batch[T]{}, false
	}
}

// OnBatchDone must be called after processing a batch
// to allow segment reclamation.
func (q *segmentedQ[T]) OnBatchDone(b Batch[T]) {

	seg := b.Seg
	if seg == nil {
		return
	}
	n := seg.inflight.Add(-1)

	if n < 0 {
		panic("Inflight went negative")
	}
	q.tryRecycle(seg)
}

// tryAddRef attempts to acquire a reference to a segment
// unless it is already detached.
func (s *segment[T]) tryAddRef() bool {
	if s.detached.Load() != 0 {
		return false
	}
	s.refs.Add(1)
	if s.detached.Load() != 0 {
		s.refs.Add(-1)
		return false
	}
	return true
}

// tryRecycle returns a detached segment to the pool
// once it is no longer referenced or inflight.
func (q *segmentedQ[T]) tryRecycle(seg *segment[T]) {
	if seg.detached.Load() == 0 {
		return
	}
	if seg.inflight.Load() != 0 {
		return
	}
	if seg.refs.Load() != 0 {
		return
	}

	if q.head.Load() == seg {
		return
	}
	if q.tail.Load() == seg {
		return
	}

	atomic.StoreUint32(&seg.consumer.head, 0)
	atomic.StoreUint32(&seg.producer.reserve, 0)

	seg.next.Store(nil)
	seg.inflight.Store(0)

	newGen := seg.gen.Add(1)
	if newGen == 0 {
		seg.gen.Store(1)
	}

	seg.detached.Store(0)
	q.pool.Put(seg)
}

// Len returns an approximate number of jobs in the queue.
// Currently unimplemented.
func (q *segmentedQ[T]) Len() int { 
	// approximate 
    seg := q.head.Load()
    h := atomic.LoadUint32(&seg.consumer.head)
    r := atomic.LoadUint32(&seg.producer.reserve)
    return int(r - h)
}

// MaybeHasWork performs a fast, approximate check for available work.
func (q *segmentedQ[T]) MaybeHasWork() bool {
	seg := q.head.Load()
	if seg == nil {
		return false
	}
	h := atomic.LoadUint32(&seg.consumer.head)
	r := atomic.LoadUint32(&seg.producer.reserve)
	return r > h || seg.next.Load() != nil
}
