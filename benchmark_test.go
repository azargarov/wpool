package workerpool_test

import (
	"runtime"
	"testing"
	"time"
	"context"
	"sync/atomic"
	"math/rand"
	"math"

	wp "github.com/azargarov/wpool"
)

var seedCounter atomic.Int64

// -----------------------------------------------------------------------------
// Queue helpers
// -----------------------------------------------------------------------------

//func newTestQueue[T any](opts wp.Options) *wp.segmentedQ[T] {
//	return wp.NewSegmentedQ[T](opts, nil)
//}

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

	q := wp.NewSegmentedQ[int](opts,nil)
	job := wp.Job[int]{Fn: func(int) error { return nil }}

	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
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
	q := wp.NewSegmentedQ[int](opts,nil)
	job := wp.Job[int]{Fn: func(int) error { return nil }}

	const prefill = 4096
	for range prefill {
		_ = q.Push(job)
	}

	b.ReportAllocs()

	for b.Loop() {
		batch, ok := q.BatchPop()
		if !ok {
			b.Fatal("queue unexpectedly empty")
		}
		q.OnBatchDone(batch)
		q.Push(job)
	}
}

func BenchmarkSegmentedQueue_PushPop(b *testing.B) {
	opts := defaultSegmentedOptions(runtime.GOMAXPROCS(0))
	opts.SegmentCount = 32
	opts.PoolCapacity = 64
	opts.PinWorkers = true

	q := wp.NewSegmentedQ[int](opts,nil)
	job := wp.Job[int]{Fn: func(int) error { return nil }}

	b.ReportAllocs()

	for b.Loop() {
		if err := q.Push(job); err != nil {
			b.Fatalf("push failed: %v", err)
		}

		batch, ok := q.BatchPop()
		if !ok {
			b.Fatal("pop failed")
		}
		q.OnBatchDone(batch)
	}
}

func BenchmarkRBQ_PushPop(b *testing.B) {
	opts := defaultSegmentedOptions(runtime.GOMAXPROCS(0))
	opts.QT = wp.RevolvingBucketQueue
	opts.SegmentSize = 2048
	opts.SegmentCount = 32
	opts.PoolCapacity = 256
	opts.PinWorkers = true

	q := wp.NewSegmentedQ[int](opts,nil)
	job := wp.Job[int]{Fn: func(int) error { return nil }}

	b.ReportAllocs()

	for b.Loop() {
		if err := q.Push(job); err != nil {
			b.Fatalf("push failed: %v", err)
		}

		batch, ok := q.BatchPop()
		if !ok {
			b.Fatal("pop failed")
		}
		q.OnBatchDone(batch)
	}
}
func BenchmarkPool_Single(b *testing.B) {
	segSize := 4096

	cases := []struct {
		name         string
		workers      int
		segmentSize  int
		segmentCount int
		pinned       bool
		queueType    wp.QueueType
	}{
		{"RBQ/C8", runtime.GOMAXPROCS(0), segSize, 32, false, wp.RevolvingBucketQueue},
		{"SEGQ/C8", runtime.GOMAXPROCS(0), segSize, 32, false, wp.SegmentedQueue},
	}

	for _, tc := range cases {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			runPoolThroughputBench(
				b,
				tc.workers,
				tc.segmentSize,
				tc.segmentCount,
				tc.queueType,
				tc.pinned,
				emptyWork,
			)
		})
	}
}

func BenchmarkPool_Throughput(b *testing.B) {
	segSize := 2048

	cases := []struct {
		name         string
		workers      int
		segmentSize  int
		segmentCount int
		pinned       bool
		queueType    wp.QueueType
	}{
		{"RBQ/C 8 ", runtime.GOMAXPROCS(0), segSize, 8, false, wp.RevolvingBucketQueue},
		{"RBQ/C16 ", runtime.GOMAXPROCS(0), segSize, 16, false, wp.RevolvingBucketQueue},
		{"RBQ/C32 ", runtime.GOMAXPROCS(0), segSize, 32, false, wp.RevolvingBucketQueue},
		{"RBQ/C128", runtime.GOMAXPROCS(0), segSize, 128, false, wp.RevolvingBucketQueue},
		{"RBQ/C32P", runtime.GOMAXPROCS(0), segSize, 32, true, wp.RevolvingBucketQueue},
		{"SEG/C8 ", runtime.GOMAXPROCS(0), segSize, 8, false, wp.SegmentedQueue},
		{"SEG/C32 ", runtime.GOMAXPROCS(0), segSize, 32, false, wp.SegmentedQueue},
		{"SEG/C64 ", runtime.GOMAXPROCS(0), segSize, 64, false, wp.SegmentedQueue},
	}

	for _, w := range workloads  {
		w := w
		b.Run(w.name, func(b *testing.B) {
			for  _, tc := range cases {
				tc := tc
				b.Run(tc.name, func(b *testing.B) {
					runPoolThroughputBench(
						b,
						tc.workers,
						tc.segmentSize,
						tc.segmentCount,
						tc.queueType,
						tc.pinned,
						w.fn,
					)
				})
			}
		})
	}
}

func runPoolThroughputBench(
	b *testing.B,
	workers, segSize, segCount int,
	qt wp.QueueType,
	pinned bool,
	fn wp.JobFunc[any],
) {
	opts := wp.Options{
		Workers:      workers,
		QT:           qt,
		SegmentSize:  uint32(segSize),
		SegmentCount: uint32(segCount),
		PoolCapacity: 4096,
		PinWorkers:   pinned,
	}

	pool := wp.NewPoolFromOptions[*wp.NoopMetrics, int](
		&wp.NoopMetrics{},
		opts,
	)
	defer pool.Shutdown(context.Background())

	var executed atomic.Int64
	job := wp.Job[int]{
		Fn: func(int) error {
			executed.Add(1)
			fn(struct{}{})
			return nil
		},
	}

	b.ResetTimer()
	start := time.Now()
	b.RunParallel(func(pb *testing.PB) {
		seed := seedCounter.Add(1)
		r := rand.New(rand.NewSource(seed))
		for pb.Next() {
			prio := (wp.JobPriority)(r.Intn(62) + 1)
			j := job
			j.SetPriority(prio)

			if err := pool.Submit(j); err != nil {
				b.Fatalf("submit failed: %v", err)
			}
		}
	})
	waitUntilB(b, 10*time.Second, func() bool {
		return executed.Load() == int64(b.N)
	})
	
	elapsed := time.Since(start)
	secs := elapsed.Seconds()

	jobs := float64(executed.Load())
	kjps := math.Round((jobs / secs)/1e3) 

	//b.ReportMetric(jps/1e6, "Mj/s") 
	b.ReportMetric(kjps, "kj/s") 
	//b.ReportMetric((secs*1e9)/jobs, "ns/job")
}
