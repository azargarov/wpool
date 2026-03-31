package workerpool

import(
	"sync"
)

const defLimboSize = 64


type limbo[T any] struct {
    head 	 int
	mu   	 sync.Mutex
    buf 	 []*segment[T]
}

func NewLimbo[T any]( pageSize uint32 ) *limbo[T]{
	l := limbo[T]{} 
	l.buf = make([]*segment[T],defLimboSize)
	for i := range(l.buf){
		limboSeg := mkSegment[T](pageSize)
		limboSeg.casWord(limboSeg.loadWord(), withState(limboSeg.loadWord(), segDetached))
		l.buf[i] = limboSeg
	}
	l.head = len(l.buf) -1
	return &l
}
