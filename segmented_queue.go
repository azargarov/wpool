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
    if spool != nil {
        q.pool = spool
    } else {
        q.pool = NewSegmentPool[T](opts.SegmentSize, int(opts.SegmentCount), int(opts.PoolCapacity))
    }
	first := q.pool.Get()
	first.resetForUse()
	q.head.Store(first)
	q.tail.Store(first)
	q.lmb = NewLimbo[T](q.pageSize)
	return q
}

func (q *segmentedQ[T]) Push(v Job[T]) error {
	for {
		seg := q.tail.Load()
		if seg == nil {
			return errNilSegment
		}
 
		word := seg.loadWord()
 
		// Segment is not open: advance tail and retry
		if word.state() != segOpen {
			next := seg.next.Load()
			if next == nil {
				q.ensureNextSegment(seg)
			}
			q.tail.CompareAndSwap(seg, seg.next.Load())
			continue
		}
 
		// Acquire a reference so the segment cannot be recycled under us
		gen, ok := seg.tryAddRef(true)
		if !ok {
			runtime.Gosched()
			continue
		}
		// Re-read word now that we hold a ref; verify the gen hasn't changed.
		word = seg.loadWord()
		if word.gen() != gen || word.state() != segOpen {
			seg.releaseRef()
			runtime.Gosched()
			continue
		}
 
		// Claim a slot
		r := word.reserve()
		if r >= segReserve(q.pageSize) {
			// Segment is full — close it, link a successor, advance tail.
			q.tryCloseAndAdvanceTail(seg, word)
			seg.releaseRef()
			continue
		}
 
		newWord := incReserve(word)
		if !seg.casWord(word, newWord) {
			// Another producer took the slot; no need to check gen again —
			// if the segment were recycled the CAS would already have failed.
			seg.releaseRef()
			continue
		}
 
		// Publish the item
		seg.buf[r] = v
		atomic.StoreUint32(&seg.ready[r], uint32(gen))
		seg.releaseRef()
		return nil
	}
}


func (q *segmentedQ[T]) BatchPop(batch *Batch[T]) bool {
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
 
		// Try to claim a run of ready slots
		end := scanReady(seg, h, reserve, gen)
		if end > h {
			if seg.consumer.head.CompareAndSwap(h, end) {
				batch.Jobs = append(batch.Jobs[:0], seg.buf[h:end]...)
				batch.Seg = seg
				batch.End = end
				var zero Job[T]
				for i := h; i < end; i++ {
					seg.buf[i] = zero
				}
				return true
			}
			// Another consumer moved head; retry with the same seg.
			seg.releaseRef()
			continue
		}
 
		// No ready slots available right now
		switch state {
		case segOpen:
			// If the segment looks full or already has a successor,
			// help close it so producers stop targeting it.
			next := seg.next.Load()
			if reserve >= q.pageSize || next != nil {
				if seg.casWord(word, segToClosed(word)) {
					if next != nil {
						q.head.CompareAndSwap(seg, next)
					}
				}
			}
			seg.releaseRef()
			return false
 
		case segClosed:
			if h < reserve {
				// Items are still in flight from producers; give them time.
				seg.releaseRef()
				runtime.Gosched()
				continue
			}
			// Segment is fully consumed.
			next := seg.next.Load()
			if next == nil {
				seg.releaseRef()
				// TODO: help producers advance
				return false
			}
			moved := q.head.CompareAndSwap(seg, next)
			if moved || q.head.Load() != seg {
				tryDetachClosed(seg)
			}
			seg.releaseRef()
			continue
 
		case segDetached:
			if h < reserve {
				seg.releaseRef()
				runtime.Gosched()
				continue
			}
			next := seg.next.Load()
			if next == nil {
				seg.releaseRef()
				return false
			}
			q.head.CompareAndSwap(seg, next)
			seg.releaseRef()
			continue
		}
 
		// Unknown state — yield and retry.
		seg.releaseRef()
		runtime.Gosched()
	}
}

func (q *segmentedQ[T]) OnBatchDone(b *Batch[T]) {
	seg := b.Seg
	if seg == nil {
		panic("OnBatchDone: nil batch.Seg")
	}
	if seg.refs.Load() <= 0 {
		print(DebugSeg(seg))
		panic("OnBatchDone: non-positive refs before release")
	}
	seg.done.Add(int64(len(b.Jobs)))
	if seg.releaseRef() {
		q.onZeroRefs(seg)
	}
}

func (q *segmentedQ[T]) onZeroRefs(s *segment[T]) {
	if s != nil {
		q.tryRecycle(s)
	}
}

func (q *segmentedQ[T]) tryRecycle(s *segment[T]) {
	switch {
	case s.loadWord().state() != segDetached:
		return
	case q.head.Load() == s || q.tail.Load() == s:
		return
	case s.refs.Load() != 0:
		return
	case s.inPool.Load():
		return
	case s.done.Load() < int64(q.pageSize):
		return
	}
	s.next.Store(nil)
	if old := q.lmb.Retire(s); old != nil && old.refs.Load() == 0 {
		q.pool.Put(old)
	}
}

func (q *segmentedQ[T]) StatSnapshot() string {
	return q.pool.StatSnapshot()
}

func (q *segmentedQ[T]) Close() {
	print(q.pool.StatSnapshot())
	q.pool.Close()
}

// ensureNextSegment tries to attach a fresh segment to seg.next.
// If the CAS loses (another goroutine won), the freshly-obtained segment is
// marked detached and returned to the pool.
// Returns the segment that is now attached to seg.next (never nil on success).
func (q *segmentedQ[T]) ensureNextSegment(seg *segment[T]) *segment[T] {
	newSeg := q.pool.Get()
	if seg.next.CompareAndSwap(nil, newSeg) {
		return newSeg
	}
	// Lost the race — mark newSeg detached so nothing tries to use it, then recycle.
	for {
		w := newSeg.loadWord()
		if newSeg.casWord(w, withState(w, segDetached)) {
			break
		}
	}
	q.pool.Put(newSeg)
	return seg.next.Load()
}

// tryCloseAndAdvanceTail closes seg (if not already closed), ensures a next segment
// exists, and tries to swing q.tail forward.
func (q *segmentedQ[T]) tryCloseAndAdvanceTail(seg *segment[T], word segWord) {
	// Mark the segment closed so no new reserves are issued.
	seg.casWord(word, segToClosed(word))
 
	next := seg.next.Load()
	if next == nil {
		next = q.ensureNextSegment(seg)
	}
	if next != nil {
		q.tail.CompareAndSwap(seg, next)
	}
}

// tryDetachClosed transitions seg from segClosed to segDetached.
// It retries on CAS failure until the state is no longer segClosed.
func tryDetachClosed[T any](seg *segment[T]) {
	for {
		old := seg.loadWord()
		if old.state() != segClosed {
			return
		}
		if seg.casWord(old, segToDetach(old)) {
			return
		}
	}
}
 
// scanReady returns the index one past the last consecutively-ready slot
// starting at h, bounded by reserve.
func scanReady[T any](seg *segment[T], h, reserve uint32, gen segGen) uint32 {
	end := h
	for end < reserve && atomic.LoadUint32(&seg.ready[end]) == uint32(gen) {
		end++
	}
	return end
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


// # Debugging 

//nolint:unused
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

//nolint:unused
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

//nolint:unused
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

