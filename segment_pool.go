package workerpool

import (
	"sync"
)

type segmentPool[T any] struct {
	pageSize uint32
	maxKeep  int

	mu   sync.Mutex
	head *segment[T]
	kept int

	metrics *segmentPoolMetrics
	done    chan struct{}

}

func NewSegmentPool[T any](pageSize uint32, prefill int, maxKeep int, fastPut int, fastGet int) *segmentPool[T] {
	if maxKeep <= 0 {
		maxKeep = max(prefill*2, 64)
	}

	if prefill < 0 {
		prefill = 0
	}
	if prefill > maxKeep {
		prefill = maxKeep
	}

	p := &segmentPool[T]{
		pageSize: pageSize,
		maxKeep:  maxKeep,
		done:     make(chan struct{}),
		metrics:  &segmentPoolMetrics{},
	}

	for i := 0; i < prefill; i++ {
		s := mkSegment[T](pageSize)

		s.resetForUse()
        w := s.loadWord()
        s.casWord(w, withState(w,segDetached))
        s.inPool.Store(true)
		s.nextFree.Store(p.head)
		p.head = s
		p.kept++
	}

	return p
}

func (p *segmentPool[T]) Put(seg *segment[T]) {
	if seg == nil {
		return
	}

	if seg.refs.Load() != 0 {
        panic("segmentPool.Put: segment with refs")
	}
    
    if seg.loadWord().state() != segDetached {
        panic("segmentPool.Put: non-detached segment")
    }
	
    if !seg.inPool.CompareAndSwap(false, true) {
        return          // TODO: debug
		panic("segmentPool.Put: double Put of same segment")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.kept >= p.maxKeep {
        seg.inPool.Store(false)
		return
	}

	seg.nextFree.Store(p.head)
	p.head = seg
	p.kept++
}

func (p *segmentPool[T]) Get() *segment[T] {
    return mkSegment[T](p.pageSize)     // TODO: debug
	p.mu.Lock()
	h := p.head
	if h != nil {
		p.head = h.nextFree.Load()
		h.nextFree.Store(nil)
		p.kept--
	}
	p.mu.Unlock()

	if h == nil {
		return mkSegment[T](p.pageSize)
	}

	if !h.inPool.CompareAndSwap(true, false) {
		panic("segmentPool.Get: popped segment not marked inPool")
	}

	if h.loadWord().state() != segDetached {
        println("segment state: ", h.loadWord().state())
		panic("segmentPool.Get: freelist head is not detached")
	}
	if h.refs.Load() != 0 {
		panic("segmentPool.Get: freelist head has non-zero refs")
	}

	h.resetForUse()
	return h
}

func (p *segmentPool[T]) StatSnapshot() string {
	return p.metrics.Snapshot().String()
}

func (p *segmentPool[T]) Close() {
	select {
	case <-p.done:
		return
	default:
		close(p.done)
	}
}
