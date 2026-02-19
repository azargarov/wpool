package workerpool_test

import (
	wp "github.com/azargarov/wpool"
	"runtime"
	"testing"
	"sync"
)

func BenchmarkSegmentedQueue_PushOnly(b *testing.B) {
	workers := runtime.GOMAXPROCS(0) * 2

	opts := wp.Options{
		Workers:      workers,
		QT:           wp.SegmentedQueue,
		SegmentSize:  1024,
		SegmentCount: 2,
		PinWorkers:   true,
	}
	q := wp.NewSegmentedQ[int](opts, nil)
	baseJob := wp.Job[int]{Fn: func(int) error { return nil }}

	b.ReportAllocs()

	for b.Loop() {
		q.Push(baseJob)
	}
}

func BenchmarkSegmentedQueue_PopOnly(b *testing.B) {
	workers := runtime.GOMAXPROCS(0) * 2

	opts := wp.Options{
		Workers:      workers,
		QT:           wp.SegmentedQueue,
		SegmentSize:  1024,
		SegmentCount: 2,
		PinWorkers:   false,
	}
	q := wp.NewSegmentedQ[int](opts, nil)
	job := wp.Job[int]{Fn: func(int) error { return nil }}

	const prefill = 4096
	for range prefill {
		q.Push(job)
	}

	b.ReportAllocs()

	for b.Loop() {
		_, ok := q.BatchPop()
		if !ok {
			b.Fatal("queue unexpectedly empty")
		}
		q.Push(job)
	}
}

func BenchmarkSegmentedQueue_PushPop(b *testing.B) {

	workers := runtime.GOMAXPROCS(0)

	opts := wp.Options{
		Workers:      workers,
		QT:           wp.SegmentedQueue,
		SegmentSize:  1024,
		SegmentCount: 32,
		PoolCapacity: 64,
		PinWorkers:   true,
	}
	q := wp.NewSegmentedQ[int](opts, nil)
	job := wp.Job[int]{Fn: func(int) error { return nil }}

	b.ReportAllocs()

	for b.Loop() {
		q.Push(job)
		_, ok := q.BatchPop()
		if !ok {
			panic("Failed pop")
		}
	}
}

func BenchmarkRBQ_PushPop(b *testing.B) {

	workers := runtime.GOMAXPROCS(0)

	opts := wp.Options{
		Workers:      workers,
		QT:           wp.RevolvingBucketQueue,
		SegmentSize:  2048,
		SegmentCount: 32,
		PoolCapacity: 256,
		PinWorkers:   true,
	}
	q := wp.NewSegmentedQ[int](opts, nil)
	job := wp.Job[int]{Fn: func(int) error { return nil }}

	b.ReportAllocs()

	for b.Loop() {
		q.Push(job)
		_, ok := q.BatchPop()
		if !ok {
			panic("Failed pop")
		}
	}
}

func TestSegmentedQ_ABA(t *testing.T) {
    q := wp.NewSegmentedQ[int](wp.Options{SegmentSize: 4}, nil)
    
    var wg sync.WaitGroup
    errors := make(chan error, 100)
    
    // Aggressive Push/Pop/Recycle cycle
    for i := range 100 {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            for j := range 10000 {
                if err := q.Push(wp.Job[int]{Payload: id*10000 + j}); err != nil {
                    errors <- err
                    return
                }
            }
        }(i)
    }
    
    for range 50 {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for range 20000 {
                batch, ok := q.BatchPop()
                if ok {
                    q.OnBatchDone(batch)
                }
                runtime.Gosched()
            }
        }()
    }
    
    wg.Wait()
    close(errors)
    
    for err := range errors {
        t.Fatal(err)
    }
}


