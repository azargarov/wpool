package workerpool_test

import (
	"runtime"
	"testing"

	wp "github.com/azargarov/wpool"
)

func defaultSegmentedOptions(workers int) wp.Options {
	return wp.Options{
		Workers:      workers,
		QT:           wp.SegmentedQueue,
		SegmentSize:  1024,
		SegmentCount: 16,
		PoolCapacity: 1024,
	}
}

func BenchmarkSegmentedQueue_PushOnly(b *testing.B) {
	opts := defaultSegmentedOptions(runtime.GOMAXPROCS(0) * 2)
	opts.SegmentCount = 2
	opts.PinWorkers = true

	q := wp.NewSegmentedQ[int](opts, nil)
	job := wp.Job[int]{Fn: func(int) error { return nil }}

	b.ReportAllocs()

	for b.Loop() {
		if err := q.Push(job); err != nil {
			b.Fatalf("push failed: %v", err)
		}
	}
}

func BenchmarkSegmentedQueue_PopOnly(b *testing.B) {
	opts := wp.Options{
		Workers:      runtime.GOMAXPROCS(0),
		QT:           wp.SegmentedQueue,
		SegmentSize:  1024,
		SegmentCount: 2,
		PinWorkers:   false,
	}
	q := wp.NewSegmentedQ[int](opts, nil)
	job := wp.Job[int]{Fn: func(int) error { return nil }}

	batch := wp.Batch[int]{}
	batch.Jobs = make([]wp.Job[int], 0, 128)

	const prefill = 4096
	for range prefill {
		_ = q.Push(job)
	}

	b.ReportAllocs()

	for b.Loop() {
		ok := q.BatchPop(&batch)
		if !ok {
			b.Fatal("queue unexpectedly empty")
		}
		q.OnBatchDone(&batch)
		res := q.Push(job)
		if res != nil {
			b.Fatal("Push failed")
		}
	}
}

func BenchmarkSegmentedQueue_PushPop(b *testing.B) {
	opts := defaultSegmentedOptions(runtime.GOMAXPROCS(0))
	opts.SegmentCount = 32
	opts.PoolCapacity = 64
	opts.PinWorkers = true

	q := wp.NewSegmentedQ[int](opts, nil)
	job := wp.Job[int]{Fn: func(int) error { return nil }}

	batch := wp.Batch[int]{}
	batch.Jobs = make([]wp.Job[int], 0, 128)

	b.ReportAllocs()

	for b.Loop() {
		if err := q.Push(job); err != nil {
			b.Fatalf("push failed: %v", err)
		}

		ok := q.BatchPop(&batch)
		if !ok {
			b.Fatal("pop failed")
		}
		q.OnBatchDone(&batch)
	}
}
