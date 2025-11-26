package workerpool

import (
    "time"
    "math/bits"

)

const (
	// minPriority is the lowest possible priority bucket index.
	BQminPriority = 0

	// maxPriority is the highest bucket index. The queue contains
	// maxPriority - minPriority + 1 buckets.
	// limited by uint64 - 64 bits
	BQmaxPriority = 60

	// initialBucketSize pre-allocates each bucket with this capacity.
	// This helps reduce allocations under heavy load.
	initialBucketSize = 100
)

// Prio is the internal integer priority bucket index.
type Prio int

// bucketQueue is a fixed-range, O(1) priority queue implementation.
//
// It divides the priority space into a fixed number of discrete buckets.
// Each job is placed into one of these buckets based on its "effective"
// priority, which is computed once at insertion time.
//
// Unlike the heap-based priority queue, bucketQueue does *not* perform
// periodic re-aging: the priority decay is computed using a monotonically
// increasing sequence number. Older jobs effectively get pushed into
// lower buckets, ensuring fairness while maintaining O(1) push/pop.
//
// NOTE: bucketQueue is NOT thread-safe.
// It assumes all operations occur from a single scheduler goroutine.
// Push and Pop must never be called concurrently from multiple goroutines.
type bucketQueue[T any] struct {
	buckets [][]item[T] // array of buckets
	maxPrio Prio        // highest bucket currently containing jobs
	aging   float64     // aging rate applied via computeBucket
	seq     uint64      // insertion counter for age-based decay
	length  int         // total items across all buckets
    bitmap  uint64
}

// newBucketQueue allocates a new bucket-based priority queue.
//
// aging controls how fast priorities decay as seq grows.
// initialBSize sets the per-bucket initial capacity.
func newBucketQueue[T any](aging float64, initialBSize int) *bucketQueue[T] {
	size := BQmaxPriority - BQminPriority + 1
	buckets := make([][]item[T], size)

	if initialBSize > 0 {
		for i := range buckets {
			buckets[i] = make([]item[T], 0, initialBSize)
		}
	}

	return &bucketQueue[T]{
		buckets: buckets,
		maxPrio: BQminPriority,
		aging:   aging,
	}
}

// computeBucket calculates the “aging” score for a job and maps it into a
// discrete bucket index.
//
// NOTE: The aging logic here is intentionally *inverted* compared to
// classical priority aging.
//
//   - In traditional aging models, waiting longer INCREASES a job’s priority.
//   - In BucketQueue, lower scores map to HIGHER priority buckets (bucket 0 = highest priority)
//
// This means that although the score decreases over time, the *effective*
// priority of the job actually goes UP.
//
// The formula:
//
//	score = basePrio - agingRate * seq
//
// works as follows:
//   - `seq` is the logical age (position) of the job in the queue.
//   - As `seq` grows, `score` decreases.
//   - Lower `score` → higher priority bucket → job is processed sooner.
//
// Intuition:
//   - Fresh job: high score  → lower priority → placed in a later bucket.
//   - Old job:   low score   → higher priority → pulled forward.
//
// Finally, the score is clamped to the [minPriority, maxPriority] range.
func computeBucket(base, rate float64, seq uint64) Prio {
	score := base - rate*float64(seq)

	if score < BQminPriority {
		score = BQminPriority
	}
	if score > BQmaxPriority {
		score = BQmaxPriority
	}

	return Prio(score)
}

// Push inserts a job into the appropriate bucket.
//
// The priority is computed once using computeBucket(), so aging is
// static and does not require periodic rebalancing.
func (q *bucketQueue[T]) Push(job Job[T], basePrio float64, now time.Time) {
	seq := q.seq
	q.seq++
	if q.seq == 0 {
		// Handle potential wrap, unlikely 
		q.seq = 1
	}

	prio := computeBucket(basePrio, q.aging, seq)

	it := item[T]{
		job:      job,
		basePrio: basePrio,
		queuedAt: now,
		prio:     prio,
	}

	idx := int(prio)
	if idx < BQminPriority {
		idx = BQminPriority
	} else if idx > BQmaxPriority {
		idx = BQmaxPriority
	}

	q.buckets[idx] = append(q.buckets[idx], it)
    q.bitmap |= (1 << uint(prio))
	q.length++

	if prio > q.maxPrio {
		q.maxPrio = prio
	}
}

// Pop removes and returns the next highest-priority job.
//
// The queue scans downward from maxPrio until it finds a non-empty bucket.
// Since maxPrio is updated eagerly, the scan is typically only one bucket.
func (q *bucketQueue[T]) Pop(_ time.Time) (Job[T], bool) {
    if q.bitmap == 0 {
        return Job[T]{}, false
    }

    idx := bits.TrailingZeros64(q.bitmap)
    bucket := &q.buckets[idx]
    n := len(*bucket)
    if n == 0 {
        q.bitmap &^= 1 << uint(idx)
        return Job[T]{}, false
    }

    // pop last element
    it := (*bucket)[n-1]
    *bucket = (*bucket)[:n-1]
    q.length--

    // empty backet → clear bit
    if len(*bucket) == 0 {
        q.bitmap &^= 1 << uint(idx)
    }

    return it.job, true
}

// Tick is a no-op for bucketQueue, since aging is computed at insertion.
func (q *bucketQueue[T]) Tick(_ time.Time) {}

// Len returns the total number of jobs currently stored.
func (q *bucketQueue[T]) Len() int {
	return q.length
}

// MaxAge always returns zero because bucketQueue does not track
// per-job wait durations. This satisfies schedQueue.
func (q *bucketQueue[T]) MaxAge() time.Duration {
	return 0
}
