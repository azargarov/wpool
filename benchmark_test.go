package workerpool_test

import (
	"runtime"
	"testing"
	"time"
	"context"
	"sync/atomic"
	"sync"
	"os"

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
	opts.SegmentSize = 64
	opts.SegmentCount =32
	opts.PoolCapacity = 256
	opts.PinWorkers = false

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
	workers := getenvInt("WORKERS", runtime.GOMAXPROCS(0) ) 
	segSize := getenvInt("SEGSIZE",32)
	segCount := getenvInt("SEGCOUNT", 32)
	pinned := getenvInt("PINNED", 1) > 0
	maxProducers := getenvInt("PRODUCERS", runtime.GOMAXPROCS(0)) 

	cases := []struct {
		name         string
		workers      int
		segmentSize  int
		segmentCount int
		pinned       bool
		work         func(any)error
		queueType    wp.QueueType
	}{
		//{"RBQ/C8", workers, segSize, segCount, pinned, emptyWork, wp.RevolvingBucketQueue},
		{"SEGQ/emptyWork", workers, segSize, segCount, pinned, emptyWork, wp.SegmentedQueue},
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
				int32(maxProducers),
				tc.work,
			)
		})
	}
}

func BenchmarkPool_Throughput(b *testing.B) {
	segSize := 2048
	maxProducers := getenvInt("PRODUCERS", runtime.GOMAXPROCS(0))

	cases := []struct {
		name         string
		workers      int
		segmentSize  int
		segmentCount int
		pinned       bool
		queueType    wp.QueueType
	}{
		//{"RBQ/C 8 ", runtime.GOMAXPROCS(0), segSize, 8, false, wp.RevolvingBucketQueue},
		//{"RBQ/C16 ", runtime.GOMAXPROCS(0), segSize, 16, false, wp.RevolvingBucketQueue},
		//{"RBQ/C32 ", runtime.GOMAXPROCS(0), segSize, 32, false, wp.RevolvingBucketQueue},
		//{"RBQ/C128", runtime.GOMAXPROCS(0), segSize, 128, false, wp.RevolvingBucketQueue},
		//{"RBQ/C32P", runtime.GOMAXPROCS(0), segSize, 32, true, wp.RevolvingBucketQueue},
		//{"SEG/C8 ", runtime.GOMAXPROCS(0), segSize, 8, false, wp.SegmentedQueue},
		//{"SEG/C16 ", runtime.GOMAXPROCS(0), segSize, 16, false, wp.SegmentedQueue},
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
						int32(maxProducers),
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
	maxProducers int32,
	fn wp.JobFunc[any],
) {
	opts := wp.Options{
		Workers:      workers,
		QT:           qt,
		SegmentSize:  uint32(segSize),
		SegmentCount: uint32(segCount),
		PoolCapacity: 128,
		PinWorkers:   pinned,
	}

	pool := wp.NewPoolFromOptions[*wp.NoopMetrics, int](
		&wp.NoopMetrics{},
		opts,
	)

	var execWG sync.WaitGroup
	execWG.Add(b.N)

	jobFn := func(id int) error {
		fn(0)
		execWG.Done()
		return nil
	}

	if os.Getenv("OBSERVER") == "1" {
		done := make(chan struct{})
		defer close(done)
		go observer(5*time.Second, done, pool)
	}

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()

	var submitted int64
	var prodWG sync.WaitGroup

	for i := 0; i < int(maxProducers); i++ {
		prodWG.Add(1)
		go func() {
			defer prodWG.Done()
			for {
				n := atomic.AddInt64(&submitted, 1)
				if n > int64(b.N) {
					return
				}

				j := wp.Job[int]{Payload: 1, Fn: jobFn}
				j.SetPriority(1)

				for {
					if err := pool.Submit(j); err == nil {
						break
					}
					runtime.Gosched()
				}
			}
		}()
	}

	prodWG.Wait()
	execWG.Wait()

	elapsed := time.Since(start)
	b.StopTimer()

	_ = pool.Shutdown(context.Background())

	b.ReportMetric(float64(b.N)/elapsed.Seconds()/1e3, "kj/s")
}

func observer(t time.Duration, done chan struct{} ,p *wp.Pool[int, *wp.NoopMetrics]){

	ticker := time.NewTicker(t)
	for {
		select {
		case <- done:
			return
		case <- ticker.C:
			print(p.StatSnapshot())
			println(p.MetricsStr())
			println(p.DebugHead())
			
		}
	}

}

//GOMAXPROCS=1 WORKERS=16 perf c2c record --  go test  -run=^$ -bench=BenchmarkPool_Single -benchmem -count=1 -v
//GOMAXPROCS=16 WORKERS=16 taskset -c 0-15 perf c2c record --all-user -o perf-c2c.data -- \
//  ./wpool.test -test.run=^$ -test.bench=BenchmarkPool_Single -test.benchmem -test.count=3 -test.v
//
//perf c2c report -i perf-c2c.data --stdio
