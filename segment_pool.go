package workerpool

import (
	//"runtime"
	"sync"
)

const recycle = true

type segmentPool[T any] struct {
	head    *segment[T]
	_       cachePad
	mu      sync.Mutex
	metrics *segmentPoolMetrics

	kept     int
	done     chan struct{}
	pageSize uint32
	maxKeep  int
}

func NewSegmentPool[T any](pageSize uint32, prefill int, maxKeep int) *segmentPool[T] {
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
		s.casWord(w, withState(w, segDetached))
		s.life.inPool.Store(true)
		s.life.nextFree.Store(p.head)
		p.head = s
		p.kept++
	}

	return p
}

func (p *segmentPool[T]) Put(seg *segment[T]) {
	if !recycle {
		return
	}

	if seg == nil {
		return
	}

	p.metrics.IncPutAttempt()

	if seg.loadWord().state() != segDetached {
		print(DebugSeg(seg))
		panic("segmentPool.Put: non-detached segment")
	}

	if seg.life.refs.Load() != 0 {
		panic("segmentPool.Put: segment with refs")
	}

	seg.life.inPool.Store(true)
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.kept >= p.maxKeep {
		seg.life.inPool.Store(false)
		return
	}
	p.metrics.IncPut()
	seg.life.nextFree.Store(p.head)
	p.head = seg
	p.kept++
}

func (p *segmentPool[T]) Get() *segment[T] {
	if !recycle {
		return mkSegment[T](p.pageSize)
	}

	p.mu.Lock()
	h := p.head
	if h != nil {
		p.head = h.life.nextFree.Load()
		h.life.nextFree.Store(nil)
		p.kept--
	}
	p.mu.Unlock()

	if h == nil {
		p.metrics.IncAllocated()
		return mkSegment[T](p.pageSize)
	}

	if !h.life.inPool.CompareAndSwap(true, false) {
		panic("segmentPool.Get: popped segment not marked inPool")
	}

	//if h.loadWord().state() != segDetached {
	//	println("segment state: ", h.loadWord().state())
	//	panic("segmentPool.Get: freelist head is not detached")
	//}

	//if h.life.refs.Load() != 0 {
	//	panic("segmentPool.Get: freelist head has non-zero refs")
	//}

	//if h.life.done.Load() < int64(h.loadWord().reserve()){
	//	panic("segmentPool.Get: Done less then reserve")
	//}

	h.resetForUse()
	p.metrics.IncReused()
	return h
}

func (p *segmentPool[T]) StatSnapshot() string {
	return p.metrics.Snapshot().String()
}

func (p *segmentPool[T]) Close() {
	//select {
	//case <-p.done:
	//	return
	//default:
	//	close(p.done)
	//}
}
