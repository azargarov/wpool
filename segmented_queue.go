package workerpool

import (
	"errors"
	"runtime"
	"strings"
	"sync/atomic"

	"fmt"

	"golang.org/x/sys/cpu"
)

//func (q *segmentedQ[T]) tryReserve(seg *segment[T]) (slot segReserve, gen segGen, ok bool)
//func (q *segmentedQ[T]) helpAdvanceTail(seg *segment[T])

type cachePad = cpu.CacheLinePad

const (
	DefaultSegmentSize = 64
	DefaultFastPutGet  = 32
)

var DefaultSegmentCount uint32 = uint32(runtime.GOMAXPROCS(0) * 16)
var (
	errNilSegment = errors.New("NULL segment")
)

type segmentedQ[T any] struct {
	head     atomic.Pointer[segment[T]]
	tail     atomic.Pointer[segment[T]]
	pool     segmentPoolProvider[T]
	pageSize uint32
	lmb      *limbo[T]   
}

func NewSegmentedQ[T any](opts Options, spool segmentPoolProvider[T]) *segmentedQ[T] {
	q := &segmentedQ[T]{pageSize: opts.SegmentSize}

	capacity := opts.PoolCapacity
	if capacity <= 0 {
		capacity = opts.SegmentCount * 2
	}
	if spool == nil {
		q.pool = NewSegmentPool[T](opts.SegmentSize, int(opts.SegmentCount), int(capacity))
	} else {
		q.pool = spool
	}

	first := q.pool.Get()
	first.consumer.head.Store(0)

	first.resetForUse()

	q.head.Store(first)
	q.tail.Store(first)
	q.lmb = NewLimbo[T]()
	for i := range(q.lmb.buf){
		limboSeg := mkSegment[T](q.pageSize)
		limboSeg.casWord(limboSeg.loadWord(), withState(limboSeg.loadWord(), segDetached))
		q.lmb.buf[i] = limboSeg
	}
	q.lmb.head = len(q.lmb.buf) -1
	return q
}

func (q *segmentedQ[T]) StatSnapshot() string {
	return q.pool.StatSnapshot()
}

func (q *segmentedQ[T]) Close() {
	print(q.pool.StatSnapshot())
	q.pool.Close()
}

func (q *segmentedQ[T]) Push(v Job[T]) error {
	for {
		seg := q.tail.Load()
		if seg == nil {
			return errNilSegment
		}

		word := seg.loadWord()
		genOrig := word.gen()

		if word.state() != segOpen {
			next := seg.next.Load()
			if next == nil {
				newSeg := q.pool.Get()
				if !seg.next.CompareAndSwap(nil, newSeg) {
					w:= newSeg.loadWord()
					wold := w
					w= withState(w, segDetached)
					newSeg.casWord(wold, w)
					q.pool.Put(newSeg)		
					continue
				}
			}
			q.tail.CompareAndSwap(seg, seg.next.Load())
			continue
		}

		gen, ok := seg.tryAddRef(true)
		if !ok || (gen != genOrig) {
			runtime.Gosched()
			continue
		}

		word = seg.loadWord()
		genOrig = word.gen()
		if word.state() != segOpen {
			seg.releaseRef()
			continue
		}

		r := word.reserve()
		if r >= segReserve(q.pageSize) {
			seg.casWord(word, segToClosed(word))
			next := seg.next.Load()
			if next == nil {
				if !seg.inPool.Load(){   // if another thread already put segment into Poll
					newSeg := q.pool.Get()
					if seg.next.CompareAndSwap(nil, newSeg) {
						next = newSeg
					} else {
						w:= newSeg.loadWord()
						wold := w
						w= withState(w, segDetached)
						newSeg.casWord(wold, w)
						q.pool.Put(newSeg)		  

						next = seg.next.Load()
					}
				}else{
					panic("Push: segment in pool")
				}
			}
			q.tail.CompareAndSwap(seg, next)
			seg.releaseRef()
			continue
		}
		
		w3 := seg.loadWord()
		if w3.gen() != genOrig || (w3.state() != segOpen){
			seg.releaseRef()
			continue
		}
		
		newWord := incReserve(word)
		if !seg.casWord(word, newWord) {
			seg.releaseRef()
			continue
		}

		seg.buf[r] = v
		atomic.StoreUint32(&seg.ready[r], uint32(gen))
		seg.releaseRef()
		return nil
	}
}

func (q *segmentedQ[T]) BatchPop(batch *Batch[T]) ( bool) {
	for {
		seg := q.head.Load()
		if seg == nil {
			return false
		}

		gen, ok := seg.tryAddRef(false)
		if !ok {
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
				batch.Jobs = append(batch.Jobs[:0], seg.buf[h:end]...)
				batch.Seg =  seg
				batch.End = end

				var zero Job[T]
				for i := h; i < end; i++ {
					seg.buf[i] = zero
				}
				return  true
			}
			seg.releaseRef()
			continue
		}

		if state == segOpen {
			next := seg.next.Load()
			if reserve >= q.pageSize  || next != nil{
				if seg.casWord(word, segToClosed(word)){
					if next != nil{
						q.head.CompareAndSwap(seg, next)
					}
				}
			}
			seg.releaseRef()
			return  false
		}

		if h == reserve {
			next := seg.next.Load()

			if state == segClosed {
				if next == nil {
					seg.releaseRef()
					// TODO: help to advance
					return  false
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
				seg.releaseRef()
				continue
			}

			if state == segDetached {
				if next == nil {
					seg.releaseRef()
					return  false
				}

				q.head.CompareAndSwap(seg, next)
				seg.releaseRef()
				continue
			}
		}
		seg.releaseRef()
		runtime.Gosched()
		continue
	}
}

func (q *segmentedQ[T]) OnBatchDone(b *Batch[T]) {
	seg := b.Seg
	if seg == nil {
		panic("OnBatchDone: nil batch.Seg")
	}

	refs := seg.refs.Load()
	if refs <= 0 {
		print(DebugSeg(seg))
		panic("OnBatchDone: non-positive refs before release")
	}


	seg.done.Add(int64(len(b.Jobs)))
	res := seg.releaseRef()
	if res{
		q.onZeroRefs(seg)
	}
}

//nolint:unused
func (q *segmentedQ[T]) onZeroRefs(s *segment[T]) {
	if s == nil {
		return
	}
	q.tryRecycle(s)
}

//nolint:unused
func (q *segmentedQ[T]) tryRecycle(s *segment[T]) {

	if s.loadWord().state() != segDetached {
		return
	}
	if q.head.Load() == s || q.tail.Load() == s {
		return
	}
	if s.refs.Load() != 0 {
		return 
	}
	if s.inPool.Load() {
		return
	}
	if s.done.Load() < (int64(q.pageSize)) {
		return
	}
	s.next.Store(nil)

	old := q.lmb.Retire(s)
	if old != nil && old.refs.Load() == 0 {
    	q.pool.Put(old)
	}
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
	r := uint32(seg.loadWord().reserve())
	return r > h || seg.next.Load() != nil
}


func DebugSeg[T any](s *segment[T]) string {
	headWord := s.loadWord()
	state := headWord.state()

	h := s.consumer.head.Load()
	r := s.loadWord().reserve()
	g := headWord.gen()
	next := s.next.Load()
	refs := s.refs.Load()
	inPool := s.inPool.Load()
	done:= s.done.Load()
	//id:= s.id
	

	var res strings.Builder
	fmt.Fprintf(&res, "segment: id=%d h=%d r=%d g=%d refs=%d state=%d done=%d inPool=%t next==nil=%v headWord=%b\n",
		0, h, r, g, refs, state, done, inPool,next == nil, headWord)

	res.WriteString(" ready=[")
	for i := range r {
		fmt.Fprintf(&res, "%d:%d ", i, atomic.LoadUint32(&s.ready[i]))
	}
	res.WriteString("]\n")

	return res.String()
}

func dbgSegId[T any](tag string, s *segment[T]) {
    if s == nil {
        println(tag, "nil")
        return
    }
    println(tag,
        //"id=", s.id,
        " ptr=", s,
        " state=", s.loadWord().state(),
        " gen=", s.loadWord().gen(),
        " reserve=", s.loadWord().reserve(),
        " refs=", s.refs.Load(),
        " done=", s.done.Load(),
        " inPool=", s.inPool.Load(),
    )
}

func (q *segmentedQ[T]) DebugHead() string {
	head := q.head.Load()
	tail := q.tail.Load()
	headWord := head.loadWord()
	state := headWord.state()

	h := head.consumer.head.Load()
	r := head.loadWord().reserve()
	g := headWord.gen()
	next := head.next.Load()
	refs := head.refs.Load()
	inPool := head.inPool.Load()
	done:= head.done.Load()
	

	tailNext := tail.next.Load()
	var res strings.Builder
	fmt.Fprintf(&res, "head: h=%d r=%d g=%d refs=%d state=%d done=%d inPool=%t next==nil=%v tailNext==nil=%v tail==head=%v  headWord=%b\n",
		h, r, g, refs, state, done, inPool,next == nil, tailNext == nil, tail == head, headWord)

	res.WriteString(" ready=[")
	for i := range r {
		fmt.Fprintf(&res, "%d:%d ", i, atomic.LoadUint32(&head.ready[i]))
	}
	res.WriteString("]\n\n")

	tail = q.tail.Load()
	tailWord := head.loadWord()
	state = tailWord.state()

	t := tail.consumer.head.Load()
	r = tail.loadWord().reserve()
	g = tailWord.gen()
	next = tail.next.Load()
	refs = tail.refs.Load()
	inPool = tail.inPool.Load()
	done = tail.done.Load()

	fmt.Fprintf(&res, "tail: h=%d r=%d g=%d refs=%d state=%d done=%d inPool=%t next==nil=%v tailNext==nil=%v tail==head=%v  tailWord=%b\n",
		t, r, g, refs, state, done, inPool, next == nil, tailNext == nil, tail == head, tailWord)
	return res.String()
}


