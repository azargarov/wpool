package workerpool

import(
	"sync"
)

// segmentPool manages reusable segments to reduce allocation pressure.
type segmentPool[T any] struct {
	mu      sync.Mutex

	maxKeep int64
	free    []*segment[T]
}

// Put returns a detached segment back to the pool.
func (p *segmentPool[T]) Put(seg *segment[T]) {
	p.mu.Lock()
	max := int(p.maxKeep)
	if max <= 0 {
		max = cap(p.free)
	}
	if len(p.free) < max {
		p.free = append(p.free, seg)
	}
	p.mu.Unlock()
	statRecycled()
}

// Get retrieves a segment from the pool or allocates a new one.
func (p *segmentPool[T]) Get(pageSize uint32) *segment[T] {
	p.mu.Lock()
	n := len(p.free)
	if n == 0 {
		p.mu.Unlock()
		return mkSegment[T](pageSize)
	}
	seg := p.free[n-1]
	p.free[n-1] = nil
	p.free = p.free[:n-1]
	p.mu.Unlock()
	statConsumed()
	return seg
}

func NewSegmentPool[T any](capacity uint32, segmentCount uint32, segmentSize uint32) segmentPoolProvider[T]{
    pool := &segmentPool[T]{
        maxKeep: int64(capacity),
        free:    make([]*segment[T], 0, capacity),
    }
	for range segmentCount {
		pool.free = append(pool.free, mkSegment[T](segmentSize))
	}
	return pool
}