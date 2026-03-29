package workerpool

import (
	//"runtime"
	"sync"
)

const recycle = true

type segmentPool[T any] struct {
	head    *segment[T]
	mu      sync.Mutex
	metrics *segmentPoolMetrics

	kept     int
	done     chan struct{}
	pageSize uint32
	maxKeep  int
}

type limbo[T any] struct {
	mu   sync.Mutex
    buf []*segment[T]
    head int
}

func NewLimbo[T any]() *limbo[T]{
	l := limbo[T]{} 
	l.buf = make([]*segment[T],128)
	return &l
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
		s.inPool.Store(true)
		s.nextFree.Store(p.head)
		p.head = s
		p.kept++
	}

	return p
}


func (l *limbo[T]) Retire(s *segment[T]) *segment[T] {
	
	//w := s.loadWord()
	//if w.state() != segDetached {
	//	if !s.casWord(w, withState(w, segDetached)) {
	//		panic("limbo retire: detach CAS failed")
	//    }
	//}
	
	s.inPool.Store(true)
	
	l.mu.Lock()
	defer l.mu.Unlock()


    old := l.buf[l.head]
    l.buf[l.head] = s
    l.head = (l.head + 1) % len(l.buf)

	//if old.loadWord().state() != segDetached{
	//	print(DebugSeg(old))
	//	panic("limbo retire: old not detached")
	//}
    return old
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
    

    if seg.refs.Load() != 0 {
		panic("segmentPool.Put: segment with refs")
    }
	
	seg.inPool.Store(true)
	//if !seg.inPool.CompareAndSwap(false, true) {
	//	return
	//}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.kept >= p.maxKeep {
		seg.inPool.Store(false)
		return
	}
	p.metrics.IncPut()
	seg.nextFree.Store(p.head)
	p.head = seg
	p.kept++
}

func (p *segmentPool[T]) Get() *segment[T] {
	if !recycle {
		return mkSegment[T](p.pageSize)
	} // TODO: debug

	p.mu.Lock()
	h := p.head
	if h != nil {
		p.head = h.nextFree.Load()
		h.nextFree.Store(nil)
		p.kept--
	}
	p.mu.Unlock()

	if h == nil {
		p.metrics.IncAllocated()
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
    if h.done.Load() < int64(h.loadWord().reserve()){
		panic("segmentPool.Get: Done less then reserve")
    }
	h.resetForUse()
	p.metrics.IncReused()
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
