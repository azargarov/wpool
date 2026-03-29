package workerpool

import (
	"sync/atomic"
)

type producerView struct {
	tail atomic.Uint32
}

type consumerView struct {
	head atomic.Uint32
}

var segIDSrc atomic.Uint64

type segment[T any] struct {
	//id uint64
	state    atomic.Uint64
	refs     atomic.Int64 //number of c/p referencing to segment + detached bin 63
	done     atomic.Int64

	producer producerView
	consumer consumerView
	nextFree atomic.Pointer[segment[T]]

	next   atomic.Pointer[segment[T]]

	// Temporary: use inPool as a "retired/unavailable" guard to prevent
    // duplicate limbo insertion. This is broader than actual freelist membership.
	inPool atomic.Bool 
	ready  []uint32
	buf    []Job[T]
}

// mkSegment allocates and initializes a new segment.
func mkSegment[T any](segSize uint32) *segment[T] {
	seg := segment[T]{
		buf:   make([]Job[T], segSize),
		ready: make([]uint32, segSize),
	}
	//seg.id = segIDSrc.Add(1)
	seg.refs.Store(int64(0))
	seg.state.Store(uint64(segReset(seg.loadWord())))
	return &seg
}

func (s *segment[T]) loadWord() segWord {
	return segWord(s.state.Load())
}

func (s *segment[T]) casWord(old, new segWord) bool {
	return s.state.CompareAndSwap(uint64(old), uint64(new))
}

func (s *segment[T]) resetForUse() {
	s.consumer.head.Store(0)
	s.producer.tail.Store(0)
	s.next.Store(nil)
	s.refs.Store(int64(0))
	s.done.Store(int64(0))
	s.inPool.Store(false)
	s.casWord(s.loadWord(), segReset(s.loadWord()))
}

func (s *segment[T]) tryAddRef(producer bool) (segGen, bool) {
	w1 := s.loadWord()
	st := w1.state()

	if (producer && st != segOpen) || st == segDetached {
		return 0, false
	}

	g := w1.gen()
	s.refs.Add(1)

	w2 := s.loadWord()
	if w2.gen() == g && w2.state() != segDetached {
		return g, true
	}

	s.refs.Add(-1)
	return 0, false
}

func (s *segment[T]) releaseRef() bool {
	r := s.refs.Add(-1)
	if r < 0 {
		panic("negative refs")
	}
	return r == 0
}
