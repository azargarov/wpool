package workerpool

//go:inline

import (
	"math/bits"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxBatch = defaultBatch
	BQminPriority = 0
	BQmaxPriority = 60
	initialBucketSize = 4192
)

type Prio int

type ringBucket[T any] struct {
	buf       []item[T]
	head      atomic.Uint64 
	tail      atomic.Uint64
	mask      uint64 // len(buf) - 1, buf size must be power of 2
	mu 		  sync.Mutex
}

func nextPow2(n int) int {
	if n <= 1 {
		return 1
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n++
	return n
}

type bucketQueue[T any] struct {
	buckets []ringBucket[T]
	aging   int     			// aging rate applied via computeBucket
	seq     atomic.Uint64       // insertion counter for age-based decay
	length  atomic.Int64        // total items across all buckets
	bitmap  atomic.Uint64 		//uint64
	batch   []Job[T]
}


func NewBucketQueue[T any](aging int, initialBSize int) *bucketQueue[T] {
	size := BQmaxPriority - BQminPriority + 1
	if initialBSize <= 0 {
		initialBSize = 128
	}
	capacity := nextPow2(initialBSize)

	buckets := make([]ringBucket[T], size)
	for i := range buckets {
		buckets[i].buf = make([]item[T], capacity)
		buckets[i].mask = uint64(capacity - 1)
	}

	return &bucketQueue[T]{
		buckets: buckets,
		aging:   aging,
		batch: make([]Job[T], maxBatch),
	}
}


func ComputeBucket(base, rate int, seq uint64) Prio {

	score := base
	if rate > 0 {
		score = base - int(seq>>rate)
	}
	if score < BQminPriority {
		score = BQminPriority
	}
	if score > BQmaxPriority {
		score = BQmaxPriority
	}

	return Prio(score)
}


func (q *bucketQueue[T]) Tick(_ time.Time) {}

func (b *ringBucket[T])Len() uint64{
	head := b.head.Load()
	tail := b.tail.Load()
	used := tail - head
	return used
}

func (q *bucketQueue[T]) Len() int {
	return int(q.length.Load())
}

func (q *bucketQueue[T]) MaxAge() time.Duration {
	return 0
}
func (q *bucketQueue[T]) Push(job Job[T], basePrio int, now time.Time) bool {
	seq := q.seq.Add(1)
	idx := int(ComputeBucket(basePrio, q.aging, seq)) // 0..BQmaxPriority

	b := &q.buckets[idx]
	b.mu.Lock()

	head := b.head.Load()
	tail := b.tail.Load()
	used := tail - head
	capacity := uint64(len(b.buf))

	if used >= capacity {
		// full; TODO: grow instead of dropping
		b.mu.Unlock()
		return false
	}

	pos := int(tail & b.mask)
	b.buf[pos] = item[T]{
		job:      job,
		basePrio: basePrio,
		queuedAt: now,
		prio:     Prio(idx),
	}
	b.tail.Store(tail + 1)
	
	if used == 0 {
	    q.bitmap.Or(uint64(1) << uint(idx))
	}
	
	b.mu.Unlock()
	q.length.Add(1)
	return true
}

func (q *bucketQueue[T]) Pop(_ time.Time) (Job[T], bool) {
	for {
		bitmap := q.bitmap.Load()
		if bitmap == 0 {
			return Job[T]{}, false
		}

		idx := bits.TrailingZeros64(bitmap)
		if idx >= len(q.buckets) { 
			q.bitmap.Store(0)
			return Job[T]{}, false
		}

		b := &q.buckets[idx]
		b.mu.Lock()

		head := b.head.Load()
		tail := b.tail.Load()

		if head == tail {
			q.bitmap.And(^uint64(1 << uint(idx)))
			b.mu.Unlock()
			continue
		}

		pos := int(head & b.mask)
		it := b.buf[pos]
		b.buf[pos] = item[T]{} 

		b.head.Store(head + 1)

		if head + 1 == tail {
			q.bitmap.And(^uint64(1 << uint(idx)))
		}

		b.mu.Unlock()
		q.length.Add(-1)
		return it.job, true
	}
}

func (q *bucketQueue[T]) BatchPop() ([]Job[T], bool) {
	for {
		bitmap := q.bitmap.Load()
		if bitmap == 0 {
			return nil, false
		}

		idx := bits.TrailingZeros64(bitmap)
		if idx >= len(q.buckets) {
			q.bitmap.Store(0)
			return nil, false
		}

		b := &q.buckets[idx]
		b.mu.Lock()

		head := b.head.Load()
		tail := b.tail.Load()

		if head == tail {
			q.bitmap.And(^uint64(1 << uint(idx)))
			b.mu.Unlock()
			continue
		}

		available := tail - head
		used := available
		if used > maxBatch {
			used = maxBatch
		}

		mask := b.mask

		for i := uint64(0); i < used; i++ {
			pos := int((head + i) & mask)
			q.batch[i] = b.buf[pos].job
			b.buf[pos] = item[T]{}
		}

		b.head.Store(head + used)
		
		if head+used == tail {
			q.bitmap.And(^uint64(1 << uint(idx)))
		}
		
		b.mu.Unlock()
		q.length.Add(-int64(used))

		return q.batch[:used], true
	}
}