package workerpool_test

import (
	wp "github.com/azargarov/wpool"
	"runtime"
	"testing"
)

func BenchmarkBucketQueue_PushOnly(b *testing.B) {
	workers := runtime.GOMAXPROCS(0) * 2

	opts := wp.Options{
		Workers:      workers,
		QT:           wp.SegmentedQueue,
		SegmentSize:  1024,
		SegmentCount: 2,
		PinWorkers:   true,
	}
	q := wp.NewSegmentedQ[int](opts)
	baseJob := wp.Job[int]{Fn: func(int) error { return nil }}

	b.ReportAllocs()

	for b.Loop() {
		q.Push(baseJob)
	}
}

func BenchmarkBucketQueue_PopOnly(b *testing.B) {
	workers := runtime.GOMAXPROCS(0) * 2

	opts := wp.Options{
		Workers:      workers,
		QT:           wp.SegmentedQueue,
		SegmentSize:  1024,
		SegmentCount: 2,
		PinWorkers:   false,
	}
	q := wp.NewSegmentedQ[int](opts)
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

func BenchmarkBucketQueue_PushPop(b *testing.B) {

	workers := runtime.GOMAXPROCS(0)

	opts := wp.Options{
		Workers:      workers,
		QT:           wp.SegmentedQueue,
		SegmentSize:  512,
		SegmentCount: 2,
		PoolCapacity: 8,
		PinWorkers:   true,
	}
	q := wp.NewSegmentedQ[int](opts)
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
