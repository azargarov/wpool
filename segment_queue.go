package workerpool

import(
	"sync/atomic"
	//"sync"
	"time"
)

const (
	DefaultSegmentSize  = 2048
	DefaultSegmentCount = 32512
)

type cacheLinePad struct{
	_ [64]byte
}

type segment[T any] struct{
	head 	uint32
	_ 		cacheLinePad
	tail 	uint32
	_ 		cacheLinePad
	next	atomic.Pointer[segment[T]]
	_ 		cacheLinePad
	buf 	[]Job[T]
}


type segmentedQ[T any] struct{
	head 		atomic.Pointer[segment[T]]
	tail 		atomic.Pointer[segment[T]]

	pageSize	uint32
}

func NewSegmentedQ[T any](segSize uint32) *segmentedQ[T]{
	q := &segmentedQ[T]{
		pageSize: segSize,
	}

	var first, prev *segment[T]

	for i:=0; i < DefaultSegmentCount; i++{
		s:= &segment[T]{
			buf: make([]Job[T], segSize),
		}
		if prev != nil{
			prev.next.Store(s)
		}else{
			first = s
		}
		prev = s
	}

	q.head.Store(first)
	q.tail.Store(first)
	return q
}


func (q *segmentedQ[T]) Push(v Job[T], basePrio int, now time.Time) bool{
	for {
		tail := q.tail.Load()
		t := atomic.LoadUint32(&tail.tail)

		if t < q.pageSize {
			if atomic.CompareAndSwapUint32(&tail.tail, t, t+1) {
				tail.buf[t] = v
				return true
			}
			continue
		}

		next := tail.next.Load()
		if next == nil {
			return false
		} 
		q.tail.CompareAndSwap(tail, next)
		
	}
}

// NOTE:
// Segments are never reclaimed in the base queue
// Once published via next, a segment remains valid 
// for the lifetime of the queue
func (q *segmentedQ[T]) Pop(_ time.Time) (Job[T], bool) {
var zero Job[T]

	for {
		head := q.head.Load()
		h := atomic.LoadUint32(&head.head)
		t := atomic.LoadUint32(&head.tail)
		
		if h < t {
			if atomic.CompareAndSwapUint32(&head.head, h, h+1) {
				return head.buf[h], true
			}
			continue
		}
		next := head.next.Load()
		if next == nil {
			return zero, false
		}

		q.head.CompareAndSwap(head, next) 

	}
}

//BatchPop returns all currently availiable items
//from the current head segment.
//A segment is treated as a natural execution batch
func 	(q *segmentedQ[T])BatchPop() ([]Job[T], bool){
	var zero []Job[T]
	for {
		head := q.head.Load()
		h := atomic.LoadUint32(&head.head)
		t := atomic.LoadUint32(&head.tail)

		if h < t {
			if atomic.CompareAndSwapUint32(&head.head, h, t) {
				return head.buf[h:t], true
			}
			continue
		}
		next := head.next.Load()
		if next == nil {
			return zero, false
		}

		q.head.CompareAndSwap(head, next) 
	}
}

func 	(q *segmentedQ[T])Tick(now time.Time){}

func 	(q *segmentedQ[T])Len() int{return 0}

func 	(q *segmentedQ[T])MaxAge() time.Duration{return 0}