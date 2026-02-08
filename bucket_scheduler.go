package workerpool

import (
	"errors"
	"math/bits"
	"sync"
	"sync/atomic"
	"runtime"
)

var (
	ErrInvalidPriority = errors.New("bucket queue: invalid priority")
	ErrPushToActive    = errors.New("bucket queue: push to active bucket")
	ErrPushSegmentedQ  = errors.New("bucket queue: failed to push into segmented queue")
)

const (
	MinBucketPriority JobPriority = 1
	MaxBucketPriority JobPriority = 63
	BucketCount          		  = 64
)

type bucket[T any] struct {
	q *segmentedQ[T]
}

type revolveState struct {
	// base defines which priority is currently mapped to bucket 0.
	// Logical priority p maps to physical bucket (p - base) & 63.
	base atomic.Uint64
	_    cachePad

	// nonEmptyMask tracks which physical buckets have work.
	// Bit i == 1 means buckets[i] may have work.
	nonEmptyMask atomic.Uint64
	_    cachePad

}

type RevolvingBucketQ[T any] struct {
	buckets [BucketCount]bucket[T]

	state revolveState

	// fast-path hint to avoid work when empty
	hasWork atomic.Bool
	rotateMu sync.Mutex


}

type RevolvingBucketOptions struct {
	SegmentSize  uint32
	SegmentCount uint32
	PoolCapacity int
}

func NewRevolvingBucketQ[T any](opts Options) *RevolvingBucketQ[T] {
	rq := &RevolvingBucketQ[T]{}

	for i := range BucketCount {
		rq.buckets[i].q = NewSegmentedQ[T](opts)
	}

	rq.state.base.Store(0)
	rq.state.nonEmptyMask.Store(0)
	rq.hasWork.Store(false)

	return rq
}

func (rq *RevolvingBucketQ[T]) Push(job Job[T]) ( error) {

	p := job.GetPriority()
	if p < MinBucketPriority || p > MaxBucketPriority {
		return ErrInvalidPriority
	}

	base := uint8(rq.state.base.Load() & 63)
    idx := (base + uint8(p)) & 63 

	err := rq.buckets[idx].q.Push(job)
	if err != nil {
		return ErrPushSegmentedQ
	}
	// debug
	schedDdgIncBucketPush(idx)
	schedDbgIncTotalPush()

	// Mark bucket as non-empty
	mask := uint64(1) << idx
	for {
		old := rq.state.nonEmptyMask.Load()
		if rq.state.nonEmptyMask.CompareAndSwap(old, old|mask) {
			break
		}
	}
	rq.hasWork.Store(true)

	return nil
}

func (rq *RevolvingBucketQ[T]) BatchPop() (Batch[T], bool) {
	if !rq.hasWork.Load() {
	    if rq.state.nonEmptyMask.Load() == 0 {
        	return Batch[T]{}, false
    	}
    	rq.hasWork.Store(true) 
	}

	for {
		base := uint8(rq.state.base.Load() & 63)
		batch, ok := rq.buckets[base].q.BatchPop()
		if ok {
			batch.Meta = base
			//debug
			schedDbgIncPops(base)
			schedDbgAddTotalPops(uint64(len(batch.Jobs)))

			return batch, true
		}
		// debug
		schedDbgIncPopMisses(base)

		if rq.buckets[base].q.MaybeHasWork() {
			// debug
			schedDbgIncRotateAborts()
		    runtime.Gosched()
		    continue
		}

		if !rq.rotate() {
			return Batch[T]{}, false
		}
	}
}

func (rq *RevolvingBucketQ[T]) rotate() bool {
	// debug
	schedDbgIncRotateCalls()
    rq.rotateMu.Lock()
    defer rq.rotateMu.Unlock()

    for {
        base := uint8(rq.state.base.Load() & 63)
        old := rq.state.nonEmptyMask.Load()

        cleared := old &^ (uint64(1) << base)

        if cleared == 0 {
            if rq.state.nonEmptyMask.CompareAndSwap(old, 0) {
                rq.hasWork.Store(false)
				// debug
				schedDbgIncMaskClears()
                return false
            }
            continue
        }

        next := uint8(bits.TrailingZeros64(cleared))
        if next >= 64 {
            return false
        }

        if rq.state.nonEmptyMask.CompareAndSwap(old, cleared) {
            rq.state.base.Store(uint64(next))
			// debug
			schedDbgIncRotateMoves()
            return true
        }
    }
}

func (rq *RevolvingBucketQ[T]) OnBatchDone(b Batch[T]) {

	bucket, ok := b.Meta.(uint8)
	if ! ok{
		panic("Could not convert any to uint8")
	}
    rq.buckets[bucket].q.OnBatchDone(b)
}

// Len returns an approximate number of jobs in the queue.
// Currently unimplemented.
func (rq *RevolvingBucketQ[T])  Len() int { 
    return 0
}

// MaybeHasWork performs a fast, approximate check for available work.
func (rq *RevolvingBucketQ[T])  MaybeHasWork() bool {
	return false
}