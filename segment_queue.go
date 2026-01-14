package workerpool

import (
	"golang.org/x/sys/cpu"
	"runtime"
	"sync/atomic"
)

// TODO: pass it as parameters to constructor
const (
	DefaultSegmentSize = 4096
)

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

type segmentedQ[T any] struct {
	head     atomic.Pointer[segment[T]]
	tail     atomic.Pointer[segment[T]]
	pageSize uint32
}

func mkSegment[T any](segSize uint32) *segment[T] {
	return &segment[T]{
		buf:   make([]Job[T], segSize),
		ready: make([]uint32, segSize),
	}
}

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

func (q *segmentedQ[T]) Len() int { return 0 }
