package workerpool

import (
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultShardNum = 16
)

var mu sync.Mutex

type shardedBucketQueue[T any] struct {
	shards []bucketQueue[T]
	_ [64]byte
	popNext atomic.Uint32
	pushNext atomic.Uint32
	nonempty []atomic.Bool
}

func newShardedBucketQueue[T any](numShards int, aging int, initialBSize int) *shardedBucketQueue[T] {
	if numShards <= 0 {
		numShards = defaultShardNum
	}

	s := &shardedBucketQueue[T]{
		shards: make([]bucketQueue[T], numShards),
	}

	for i := range s.shards {
		s.shards[i] = *NewBucketQueue[T](aging, initialBSize)
	}
	s.nonempty = make([]atomic.Bool, numShards)
	return s
}

func (s *shardedBucketQueue[T]) Push(job Job[T], basePrio int, now time.Time) bool {
	//mu.Lock()
	//defer mu.Unlock()

	n := len(s.shards)
	if n == 0 {
		return false
	}
	idx := int(s.pushNext.Add(1)) % len(s.shards)
	sh := &s.shards[idx]
	//sh.Push(job, basePrio, now)

	wasEmpty := !s.nonempty[idx].Load()
	sh.Push(job, basePrio, now)
	if wasEmpty {
    	s.nonempty[idx].Store(true)
	}
	return true
}

func (s *shardedBucketQueue[T]) Pop(now time.Time) (Job[T], bool) {

	//mu.Lock()
	//defer mu.Unlock()

	n := len(s.shards)
	if n == 0 {
		return Job[T]{}, false
	}
	start := int(s.popNext.Add(1)) % len(s.shards)

	for i := range n {
		idx := (start + i) % n
		if !s.nonempty[idx].Load() {
        	continue
    	}
		sh := &s.shards[idx]

		job, ok := sh.Pop(now)

		if ok {
			if s.shards[idx].Len() == 0 {
            	s.nonempty[idx].Store(false)
        	}
			return job, true
		}
	}
	return Job[T]{}, false
}



func (s *shardedBucketQueue[T]) Len() int {
	mu.Lock()
	defer mu.Unlock()

	total := 0
	for i := range s.shards {
		sh := &s.shards[i]
		total += sh.Len()
	}
	return total
}

func (s *shardedBucketQueue[T]) Tick(now time.Time) {
	// no-op: bucketQueue already ages via seq
}

func (s *shardedBucketQueue[T]) MaxAge() time.Duration {
	// You don't currently track per-job age in bucketQueue.
	// If you want this, you'll need to track min(queuedAt) per shard.
	return 0
}

func (s *shardedBucketQueue[T]) BatchPop() ([]Job[T], bool){
	return nil, false
}

