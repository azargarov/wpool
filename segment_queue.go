package workerpool

import (
	"runtime"
	"sync/atomic"
	"sync"
	"golang.org/x/sys/cpu"
)

type cachePad = cpu.CacheLinePad

const (
	DefaultSegmentSize = 4096
	//defaultSegmentPoolSize = 2048
)

var DefaultSegmentCount uint32 = uint32(runtime.GOMAXPROCS(0) * 16)

type producerView struct{
	tail    uint32
	reserve uint32
	_       cachePad
}

type consumerView struct{
	head uint32
	_    cachePad
}

type segment[T any] struct {
	producer 	producerView
	consumer 	consumerView

	inflight 	atomic.Int32
	gen   		atomic.Uint32

	detached 	atomic.Uint32
	_    		cachePad 

	refs     	atomic.Int32
	_    		cachePad 

	buf 		[]Job[T]
	ready 		[]uint32
	_ 			cachePad
	next  		atomic.Pointer[segment[T]] 
}

type segmentedQ[T any] struct {
	head atomic.Pointer[segment[T]]
	
	tail atomic.Pointer[segment[T]]
	
	pool  segmentPool[T]
	
	pageSize uint32

}

func mkSegment[T any](segSize uint32) *segment[T] {
	seg :=segment[T]{
		buf:   make([]Job[T], segSize),
		ready: make([]uint32, segSize),
	}
	seg.gen.Store(1)
	statAllocated()
	return &seg
}

type segmentPool[T any] struct {
	mu   sync.Mutex
	maxKeep int64
	free []*segment[T]
}

func (p *segmentPool[T]) Put(seg *segment[T]) {
    p.mu.Lock()
    max := int(p.maxKeep)
    if max <= 0 { max = cap(p.free) }
    if len(p.free) < max {
        p.free = append(p.free, seg)
    }
    p.mu.Unlock()
	statRecycled()
}

func (p *segmentPool[T]) Get(pageSize uint32) *segment[T] {
    p.mu.Lock()
    n := len(p.free)
    if n == 0 {
        p.mu.Unlock()
        return mkSegment[T](pageSize)
    }
    seg := p.free[n-1]
    p.free[n-1] = nil 
    p.free = p.free[:n-1]
    p.mu.Unlock()
	statConsumed()
    return seg
}

func NewSegmentedQ[T any](opts Options ) *segmentedQ[T]{  
	q := &segmentedQ[T]{ pageSize: opts.SegmentSize }

	capacity := opts.PoolCapacity
	if capacity <= 0{
		capacity = opts.SegmentCount * 2
	}
	q.pool.free = make([]*segment[T], 0, capacity)
	for range opts.SegmentCount {
		q.pool.free = append(q.pool.free, mkSegment[T](opts.SegmentSize))
	}

	first := q.pool.Get(opts.SegmentSize)
	atomic.StoreUint32(&first.consumer.head, 0)
	atomic.StoreUint32(&first.producer.reserve, 0)
	first.next.Store(nil)
	first.detached.Store(0)
	first.inflight.Store(0)

	q.head.Store(first)
	q.tail.Store(first)
	return q
}

func (q *segmentedQ[T]) Push(v Job[T]) bool {
	for {
		seg := q.tail.Load()
		if seg == nil { return false }

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
			//statCASAttempt()
			if atomic.CompareAndSwapUint32(&seg.producer.reserve, r, r + 1) {
				seg.buf[r] = v
				atomic.StoreUint32(&seg.ready[r],g) 
				seg.refs.Add(-1)
				return true
			}
			statCASMiss()
		}

		next := seg.next.Load()
		if next == nil {
			newSeg := q.pool.Get(q.pageSize)
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
		for end < limit && atomic.LoadUint32(&seg.ready[end]) == g{ 
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


func (q *segmentedQ[T]) OnBatchDone(b Batch[T]) {
	
	seg := b.Seg
	if seg == nil {
        return
    }
	n := seg.inflight.Add(-1)

	if n < 0  {
		panic("Inflight went negative")
	}
	q.tryRecycle(seg)
}

func (s *segment[T]) tryAddRef() bool {
	if s.detached.Load()!=0 {return false}
	s.refs.Add(1)
	if s.detached.Load()!=0 { s.refs.Add(-1); return false }
	return true

}

func (q *segmentedQ[T]) tryRecycle(seg *segment[T]) {
	if seg.detached.Load() == 0 { return }
	if seg.inflight.Load() != 0 { return }
	if seg.refs.Load() != 0 { return }

	if q.head.Load() == seg { return }
	if q.tail.Load() == seg { return }

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


// Len should returns an approximate number of jobs in the queue.
func (q *segmentedQ[T]) Len() int { return 0 }

func (q *segmentedQ[T]) MaybeHasWork() bool {
	seg := q.head.Load()
	if seg == nil { return false }
	h := atomic.LoadUint32(&seg.consumer.head)
	r := atomic.LoadUint32(&seg.producer.reserve)
	return r > h || seg.next.Load() != nil
}
