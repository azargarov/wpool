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

const enableRecycle = false
const maxSpinToYeld = 30
const maxCASmissesBeforeGiveup = 8


// cachePad is used to prevent false sharing between hot fields.
type cachePad = cpu.CacheLinePad

const (
	// DefaultSegmentSize is the default number of jobs per segment.
	// It should be large enough to amortize allocation costs but
	// small enough to fit comfortably in cache.
	DefaultSegmentSize = 4096

	DefaultFastPutGet = 4096
	detachedMask uint64 = 1 << 63
	refMask      uint64 = ^detachedMask
)

// DefaultSegmentCount defines the default number of preallocated segments.
// It scales with GOMAXPROCS to reduce contention under load.
var DefaultSegmentCount uint32 = uint32(runtime.GOMAXPROCS(0) * 16)
var (
	segErrorNilSegment = errors.New("NULL segment")
)
// producerView contains fields frequently modified by producers.
type producerView struct {
	tail    	uint32
	reserve 	uint32
}

// consumerView contains fields frequently modified by consumers.
type consumerView struct {
	head 		uint32
	//inflight 	atomic.Int32
	//state       uint64        //hi32=head low32=inflight
}

type segment[T any] struct {
	producer 	producerView
	consumer 	consumerView
	next  		atomic.Pointer[segment[T]]
	gen      	atomic.Uint32
	refs 		atomic.Uint64
	ready 		[]uint32
	buf   		[]Job[T]
	mu 			sync.Mutex
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
	seg.gen.Store(1)
	return &seg
}

// NewSegmentedQ initializes a segmented queue with preallocated segments.
func NewSegmentedQ[T any](opts Options, spool segmentPoolProvider[T]) *segmentedQ[T] {
	q := &segmentedQ[T]{pageSize: opts.SegmentSize}

	capacity := opts.PoolCapacity
	if capacity <= 0 {
		capacity = opts.SegmentCount * 4
	}
	if spool == nil{
		q.pool = NewSegmentPool[T](opts.SegmentSize, int(opts.SegmentCount), int(capacity), DefaultFastPutGet, DefaultFastPutGet) 
	} else {
		q.pool = spool
	}


	first := q.pool.Get()
	atomic.StoreUint32(&first.consumer.head, 0)
	atomic.StoreUint32(&first.producer.reserve, 0)
	//first.inflight.Store(0)
    
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
	spins:=0
	for {
		spins ++
		if spins%maxSpinToYeld ==0 {
			runtime.Gosched()
		}

		seg := q.tail.Load()
		if seg == nil {
			return segErrorNilSegment
		}
		
		if !seg.tryAddRefProducer() {
			// detached; help tail advance
			next := seg.next.Load()
			if next != nil { 
				q.tail.CompareAndSwap(seg, next) 
			}
			//runtime.Gosched()
			continue
		}
		//double check tail
		//if q.tail.Load() != seg{
		//	seg.releaseRef()
		//	runtime.Gosched()
		//	continue
		//}
		spin1:=0
		g := seg.gen.Load()
		for {
			
			r := atomic.LoadUint32(&seg.producer.reserve)
			if r >= q.pageSize {
				break
			}
			if atomic.CompareAndSwapUint32(&seg.producer.reserve, r, r+1) {
				seg.buf[r] = v
				atomic.StoreUint32(&seg.ready[r], g)
				seg.releaseRef()
				return nil
			}
			spin1++
			if spin1%maxSpinToYeld == 0{
				runtime.Gosched()
			}
		}

		next := seg.next.Load()
		if next == nil {
			newSeg := q.pool.Get()
			newSeg.resetForUse()
			if seg.next.CompareAndSwap(nil, newSeg) {
				next = newSeg
			} else {
				q.pool.Put(newSeg)
				next = seg.next.Load()
			}
		}

		q.tail.CompareAndSwap(seg, next)
		seg.releaseRef() 
	}
}
func (q *segmentedQ[T]) BatchPop() (Batch[T], bool) {
	for {
		spins ++
		if spins >= maxCASmissesBeforeGiveup {
			return Batch[T]{}, false
		}

		seg := q.head.Load()
		if seg == nil {
			return Batch[T]{}, false
		}

		if !seg.tryAddRefConsumer() {
			continue
		}

		h := atomic.LoadUint32(&seg.consumer.head)
		r := atomic.LoadUint32(&seg.producer.reserve)
		g := seg.gen.Load()

		end := h
		for end < min(r, q.pageSize) && atomic.LoadUint32(&seg.ready[end]) == g {
			end++
		}

		if end > h {
			if atomic.CompareAndSwapUint32(&seg.consumer.head, h, end) {
				seg.releaseRef()
				return Batch[T]{Jobs: seg.buf[h:end], Seg: seg, End: end}, true
			}
			seg.releaseRef()
			continue
		}

		if h < min(r, q.pageSize) {
			seg.releaseRef()
			continue
		}

		next := seg.next.Load()
		if next != nil {
			// Revalidate with a fresh reserve snapshot before skipping this segment.
			r2 := atomic.LoadUint32(&seg.producer.reserve)
			if h < min(r2, q.pageSize) {
				seg.releaseRef()
				continue
			}

			if q.head.CompareAndSwap(seg, next) {
				seg.detach()
			}
			seg.releaseRef()
			continue
		}

		if q.tail.Load() != seg {
			seg.releaseRef()
			continue
		}

		seg.releaseRef()
		return Batch[T]{}, false
	}
}

func (q *segmentedQ[T]) OnBatchDone(b Batch[T]) {

	seg := b.Seg
	if seg == nil {
		return
	}
	//n := seg.consumer.inflight.Add(-1)

	//if n < 0 {
	//	panic("Inflight went negative")
	//}
	q.tryRecycle(seg)
}



func (s *segment[T]) tryAddRefProducer() bool {
	if !enableRecycle {
		return true
	}
	spins:=0
	for {
		r := s.refs.Load()
		
        // already detached
        if r&detachedMask != 0 {
			return false
        }
		
        if s.refs.CompareAndSwap(r, r+1) {
			return true
        }
		spins++
		if spins%maxSpinToYeld == 0{
			runtime.Gosched()
		}
    }
}

func (s *segment[T]) tryAddRefConsumer() bool {
	if !enableRecycle {
		return true
	}
	spins:=0
    for {
        r := s.refs.Load()

        count := r & refMask
        if count == refMask { 
            panic("refcount overflow")
        }

        newVal := (r & detachedMask) | (count + 1)

        if s.refs.CompareAndSwap(r, newVal) {
            return true
        }
		spins++
        if spins%maxSpinToYeld == 0 {
            runtime.Gosched()
        }
    }
}

func (s *segment[T]) releaseRef() {
    if !enableRecycle {
		return
	}
	spins:=0
	for {
		r := s.refs.Load()
		
        count := r & refMask
        if count == 0 {
			panic("releaseRef: negative refcount")
        }
		
        newVal := (r & detachedMask) | (count - 1)
		
        if s.refs.CompareAndSwap(r, newVal) {
			return
        }
		spins++
		if spins%maxSpinToYeld == 0{
			runtime.Gosched()
		}
    }
}

func (s *segment[T]) reclaimable() bool {
    refs := s.refs.Load()
    return refs&detachedMask != 0 &&
           refs&refMask == 0 //&&
           //s.consumer.inflight.Load() == 0
}

func (s *segment[T]) detach() {
	if !enableRecycle { return }
	s.refs.Or(detachedMask)
}

func (q *segmentedQ[T]) tryRecycle(seg *segment[T]) {
	if !enableRecycle { return } 
    //if seg.consumer.inflight.Load() != 0 {
	//	return
    //}

    if q.head.Load() == seg || q.tail.Load() == seg {
        return
    }

    r := seg.refs.Load()
    // must be detached
    if r&detachedMask == 0 ||r&refMask != 0  {
        return
    }

	atomic.StoreUint32(&seg.consumer.head, 0)
    atomic.StoreUint32(&seg.producer.reserve, 0)
    seg.next.Store(nil)
    //seg.inflight.Store(0)

    newGen := seg.gen.Add(1)
    if newGen == 0 {
        seg.gen.Store(1)
    }

    // Reset refs to attached, zero count
    seg.refs.Store(detachedMask)
    q.pool.Put(seg)
}

func (s *segment[T]) resetForUse() {
    atomic.StoreUint32(&s.consumer.head, 0)
    atomic.StoreUint32(&s.producer.reserve, 0)
    s.next.Store(nil)
    //s.consumer.inflight.Store(0)
    s.refs.Store(0) 
	newGen := s.gen.Add(1)
    if newGen == 0 {
        s.gen.Store(1)
    }
}

func (q *segmentedQ[T]) Len() int {
    total := 0
    seg := q.head.Load()
    
    for seg != nil && total < 1000 { 
        h := atomic.LoadUint32(&seg.consumer.head)
        r := atomic.LoadUint32(&seg.producer.reserve)
        total += int(r - h)
        
        seg = seg.next.Load()
    }
    return total
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

func (q *segmentedQ[T]) DebugHead() string {
    head := q.head.Load()
    tail := q.tail.Load()
    
    h := atomic.LoadUint32(&head.consumer.head)
    r := atomic.LoadUint32(&head.producer.reserve)
    g := head.gen.Load()
    next := head.next.Load()
    refs := head.refs.Load()
    //inflight := head.consumer.inflight.Load()

    //tr := atomic.LoadUint32(&tail.producer.reserve)
    //tg := tail.gen.Load()
    //trefs := tail.refs.Load()
    
	tailNext := tail.next.Load()
	var res strings.Builder
	fmt.Fprintf(&res, "head: h=%d r=%d g=%d refs=%d inflight=%d next==nil=%v tailNext==nil=%v tail==head=%v",
	h, r, g, refs, -1, next == nil, tailNext == nil, tail == head)
	
	//fmt.Fprintf(&res, " ready= %v", head.ready)
	res .WriteString(" ready=[")
	for i := range r {
	    fmt.Fprintf(&res, "%d:%d ", i, atomic.LoadUint32(&head.ready[i]))
	}
	res .WriteString("]")
	return res.String() 
}
