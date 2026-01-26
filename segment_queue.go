package workerpool

import (
	"runtime"
	"sync/atomic"
	"sync"
)


const (
	DefaultSegmentSize = 4096
	defaultSegmentPoolSize = 128
)


var DefaultSegmentCount uint32 = uint32(runtime.GOMAXPROCS(0) * 16)

type segHot struct {
	head uint32
	_    [60]byte

	tail    uint32
	reserve uint32
	_       [56]byte
}

type segment[T any] struct {
	hot segHot

	next  atomic.Pointer[segment[T]] 
	ready []uint32                   

	inflight atomic.Int32
	detached atomic.Uint32
	refs     atomic.Int32

	buf []Job[T]
}

type segmentedQ[T any] struct {
	head atomic.Pointer[segment[T]]
	
	tail atomic.Pointer[segment[T]]
	
	pool  segmentPool[T]
	
	pageSize uint32

	mu sync.Mutex
}

func mkSegment[T any](segSize uint32) *segment[T] {
	seg :=segment[T]{
		buf:   make([]Job[T], segSize),
		ready: make([]uint32, segSize),
	}
	return &seg
}

type segmentPool[T any] struct {
	mu   sync.Mutex
	free []*segment[T]
}

func (p *segmentPool[T]) Put(seg *segment[T]) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.free = append(p.free, seg)
}

func (p *segmentPool[T]) Get(pageSize uint32) *segment[T] {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := len(p.free)
	if n == 0 {
		return mkSegment[T](pageSize)
	}
	seg := p.free[0]
	p.free = p.free[1:]
	return seg
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
	q.pool.free = make([]*segment[T], 0, defaultSegmentPoolSize)
	for range(2){
		q.pool.free = append(q.pool.free, mkSegment[T](segSize))
	}
	return q
}

func (q *segmentedQ[T]) Push(v Job[T]) bool {
	
	for {
		seg := q.tail.Load()
		seg.refs.Add(1)
		
		// validate still current tail
		if q.tail.Load() != seg {
			seg.refs.Add(-1)
			continue
		}

		// reserve slot
		for {
			r := atomic.LoadUint32(&seg.hot.reserve)
			if r >= q.pageSize {
				break
			}
			if atomic.CompareAndSwapUint32(&seg.hot.reserve, r, r+1) {
				seg.buf[r] = v
				atomic.StoreUint32(&seg.ready[r], 1)
				seg.refs.Add(-1)
				return true
			}
		}
		// segment full -> ensure next and advance tail
		next := seg.next.Load()
		if next == nil {
			newSeg := q.pool.Get(q.pageSize)  
			if seg.next.CompareAndSwap(nil, newSeg) {
				next = newSeg
				} else {
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
		seg.refs.Add(1)
		
		if q.head.Load() != seg {
			seg.refs.Add(-1)
		    continue
		}
		
		if seg == nil {
			panic("nil segment")
		}

		h := atomic.LoadUint32(&seg.hot.head)
		r := atomic.LoadUint32(&seg.hot.reserve)
		if h == q.pageSize && r == q.pageSize {
			next := seg.next.Load()
			if next == nil {
				seg.refs.Add(-1)
				return Batch[T]{}, false
			}
			
			if q.head.CompareAndSwap(seg, next) {
				if seg.detached.CompareAndSwap(0, 1) {
					seg.refs.Add(-1)
					q.tryRecycle(seg)
					return Batch[T]{}, false
				}
			}
			seg.refs.Add(-1)
			continue
		}

		r = atomic.LoadUint32(&seg.hot.reserve)
		limit := min(r, q.pageSize)

		end := h
		for end < limit && atomic.LoadUint32(&seg.ready[end]) == 1 {
			end++
		}
		if end > h {
			if atomic.CompareAndSwapUint32(&seg.hot.head, h, end) {
				seg.inflight.Add(1)
				seg.refs.Add(-1)
				return Batch[T]{
					Jobs: seg.buf[h:end],
					Seg:  seg,
					End:  end,
				}, true
			}
			seg.refs.Add(-1)
			continue
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

func (q *segmentedQ[T]) tryRecycle(seg *segment[T]) {
    if seg.refs.Load() > 0 { return }
    if seg.detached.Load() == 0 { return }
    if seg.inflight.Load() != 0 { return }
    if q.head.Load() == seg { return }
    if q.tail.Load() == seg { return }


	atomic.StoreUint32(&seg.hot.head, 0)
	atomic.StoreUint32(&seg.hot.reserve, 0)
	atomic.StoreUint32(&seg.hot.tail, 0)
    seg.detached.Store(0)
    seg.inflight.Store(0)
    seg.next.Store(nil)
    for i := range seg.ready { seg.ready[i] = 0 }
    q.pool.Put(seg)
}


// Len should returns an approximate number of jobs in the queue.
func (q *segmentedQ[T]) Len() int { return 0 }

func (q *segmentedQ[T]) MaybeHasWork() bool {
	seg := q.head.Load()
	if seg == nil { return false }
	h := atomic.LoadUint32(&seg.hot.head)
	r := atomic.LoadUint32(&seg.hot.reserve)
	return r > h || seg.next.Load() != nil
}
