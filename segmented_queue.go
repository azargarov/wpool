package workerpool

import (
	"errors"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"fmt"

	"golang.org/x/sys/cpu"
)


//func (q *segmentedQ[T]) tryReserve(seg *segment[T]) (slot segReserve, gen segGen, ok bool)
//func (q *segmentedQ[T]) helpAdvanceTail(seg *segment[T])

type cachePad = cpu.CacheLinePad

const (
	DefaultSegmentSize = 4096
	DefaultFastPutGet = 4096
)

var DefaultSegmentCount uint32 = uint32(runtime.GOMAXPROCS(0) * 16)
var (
	segErrorNilSegment = errors.New("NULL segment")
)
type producerView struct {
	tail    	atomic.Uint32
}

type consumerView struct {
	head 		atomic.Uint32
}

type segment[T any] struct {
	nextFree atomic.Pointer[segment[T]]

	state		atomic.Uint64

	refs 		atomic.Int64  //number of c/p referencing to segment + detached bin 63
	_    cachePad

	producer 	producerView
	
	consumer 	consumerView
	
	next  		atomic.Pointer[segment[T]]
	
	ready 		[]uint32
	buf   		[]Job[T]

	mu 			sync.Mutex   // TODO: DEBUG

	inPool atomic.Bool		// TODO: DEBUG
}

type segmentedQ[T any] struct {
	head 		atomic.Pointer[segment[T]]
	tail 		atomic.Pointer[segment[T]]
	pool 		segmentPoolProvider[T]
	pageSize 	uint32
}

// mkSegment allocates and initializes a new segment.
func mkSegment[T any](segSize uint32) *segment[T] {
	seg := segment[T]{
		buf:   make([]Job[T], segSize),
		ready: make([]uint32, segSize),
	}
	seg.refs.Store(int64(0))
	seg.state.Store( uint64(segReset( seg.loadWord() ) ))
	return &seg
}


func (s *segment[T]) loadWord() segWord {
	return segWord(s.state.Load())
}

func (s *segment[T]) casWord(old, new segWord) bool {
	return s.state.CompareAndSwap(uint64(old), uint64(new))
}


func NewSegmentedQ[T any](opts Options, spool segmentPoolProvider[T]) *segmentedQ[T] {
	q := &segmentedQ[T]{pageSize: opts.SegmentSize}

	capacity := opts.PoolCapacity
	if capacity <= 0 {
		capacity = opts.SegmentCount * 4
	}
	if spool == nil{					//next = seg.next.Load()
		q.pool = NewSegmentPool[T](opts.SegmentSize, int(opts.SegmentCount), int(capacity), DefaultFastPutGet, DefaultFastPutGet) 
	} else {
		q.pool = spool
	}

	first := q.pool.Get()
	first.consumer.head.Store(0)
    
	first.resetForUse()
	
	q.head.Store(first)
	q.tail.Store(first)
	return q
}

func (q *segmentedQ[T])StatSnapshot()string{
	return q.pool.StatSnapshot()
}

func (q *segmentedQ[T])Close() {
	q.pool.Close()
}

func (q *segmentedQ[T]) Push(v Job[T]) error {
	for {
	    seg := q.tail.Load()
    	if seg == nil {
    	    return segErrorNilSegment
    	}

		word := seg.loadWord()
    
		if word.state() != segOpen {

    	    next := seg.next.Load()
    	    if next == nil {
    	        newSeg := q.pool.Get()
    	        if !seg.next.CompareAndSwap(nil, newSeg) {
    	            //q.pool.Put(newSeg)		// TODO: DEBUG, normally the segment should be returned to the pool
					continue
    	        }
    	    }

    	    q.tail.CompareAndSwap(seg, seg.next.Load())
    	    runtime.Gosched()
    	    continue
    	}

		gen, ok := seg.tryAddRef(true);
		if !ok{
			runtime.Gosched()
			continue
		}

		word = seg.loadWord()
    	if word.state() != segOpen {
    	    q.releaseRef(seg)
    	    continue
    	}

		r := word.reserve()

		if r >= segReserve(q.pageSize) {
		    seg.casWord(word, segToClosed(word))
		
		    next := seg.next.Load()
		    if next == nil {
		        newSeg := q.pool.Get()
		        if seg.next.CompareAndSwap(nil, newSeg) {
					next = newSeg
				} else {
		            //q.pool.Put(newSeg)  /// TODO: DEBUG, normally the segment should be returned to the pool
		            next = seg.next.Load()
		        }
		    }
		    q.tail.CompareAndSwap(seg, next)
		    q.releaseRef(seg)
		    continue
		}

	    newWord := incReserve( word )
	    if !seg.casWord(word, newWord) {
			q.releaseRef(seg)
	        continue
	    }
	    seg.buf[r] = v
	    atomic.StoreUint32(&seg.ready[r],  uint32(gen))
		q.releaseRef(seg)
	    return nil
	}
}

func (q *segmentedQ[T]) BatchPop() (Batch[T], bool) {
	for {
        seg := q.head.Load()
        if seg == nil {
			return Batch[T]{}, false
        }
		
		gen, ok := seg.tryAddRef(false);
		if !ok{
			runtime.Gosched()
			continue
		}
        word := seg.loadWord()
        state := word.state()
        reserve := uint32(word.reserve())
		
        h := seg.consumer.head.Load()
		
        end := h
        for end < reserve && atomic.LoadUint32(&seg.ready[end]) == uint32(gen) {
			end++
        }
		
        if end > h {
			
			if seg.consumer.head.CompareAndSwap(h, end) {
				return Batch[T]{Jobs: seg.buf[h:end], Seg: seg, End: end}, true
            }
			
			q.releaseRef(seg)
            continue
        }
		
        if state == segOpen {
			if reserve >= q.pageSize {
				seg.casWord(word, segToClosed(word))
            }
			q.releaseRef(seg)
            return Batch[T]{}, false
        }
		
		if h == reserve {
			next := seg.next.Load()
			
		    if state == segClosed {
				if next == nil {
					q.releaseRef(seg)
		            return Batch[T]{}, false
		        }
				
		        moved := q.head.CompareAndSwap(seg, next)
		        if moved || q.head.Load() != seg {
					for {
						old := seg.loadWord()
		                if old.state() != segClosed {
							break
		                }
		                if seg.casWord(old, segToDetach(old)) {
							break
		                }
		            }
		        }
				
				q.releaseRef(seg)
		        continue
		    }
			
		    if state == segDetached {
				if next == nil {
					q.releaseRef(seg)
		            return Batch[T]{}, false
		        }
			
		        q.head.CompareAndSwap(seg, next)
				q.releaseRef(seg)
		        continue
		    }
		}
    }
}

func (q *segmentedQ[T]) OnBatchDone(b Batch[T]) {

	seg := b.Seg
	if seg == nil {
		return
	}
	q.releaseRef(seg)
}

func (s *segment[T]) tryAddRef(producer bool) (segGen, bool) {
	// TODO: for debugging
	//s.mu.Lock()
	//defer s.mu.Unlock()


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
 
func (q *segmentedQ[T]) releaseRef(s *segment[T]) {
	r := s.refs.Add(-1)
	if r < 0 {
		print(q.DebugHead())
		panic("negative refs")
	}
	if r != 0 {
		return
	}

	//q.tryRecycle(s) // TODO: commented for memory recalamation disabling, debugging
}

func (q *segmentedQ[T]) tryRecycle(s *segment[T]) {
	// TODO: for debugging
	s.mu.Lock()
	defer s.mu.Unlock()

	if s == nil {
		return
	}
	if s.refs.Load() != 0 {
		return
	}
	if s.loadWord().state() != segDetached {
		return
	}
	if q.head.Load() == s || q.tail.Load() == s {
		return
	}
	
	s.next.Store(nil)
	q.pool.Put(s)
}

func (s *segment[T]) resetForUse() {
    s.consumer.head.Store(0)
    s.producer.tail.Store(0)
    s.next.Store(nil)
    s.refs.Store(int64(0)) 
	s.casWord(s.loadWord(),segReset(s.loadWord()))
}

func (q *segmentedQ[T]) Len() int {
    return 0
}

func (q *segmentedQ[T]) MaybeHasWork() bool {
	seg := q.head.Load()
	if seg == nil {
		return false
	}
	h := uint32(seg.consumer.head.Load())
	r:= uint32(seg.loadWord().reserve())
	return r > h || seg.next.Load() != nil
}

func (q *segmentedQ[T]) DebugHead() string {
    head := q.head.Load()
    tail := q.tail.Load()
	headWord := head.loadWord()
    
    h := head.consumer.head.Load()
    r := head.loadWord().reserve()
    g := headWord.gen()//head.gen.Load()
    next := head.next.Load()
    refs := head.refs.Load()

    
	tailNext := tail.next.Load()
	var res strings.Builder
	fmt.Fprintf(&res, "head: h=%d r=%d g=%d refs=%d  next==nil=%v tailNext==nil=%v tail==head=%v  headWord=%b\n",
	h, r, g, refs, next == nil, tailNext == nil, tail == head, headWord)
	
	res .WriteString(" ready=[")
	for i := range r {
	    fmt.Fprintf(&res, "%d:%d ", i, atomic.LoadUint32(&head.ready[i]))
	}
	res .WriteString("]\n")
	return res.String() 
}
