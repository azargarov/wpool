package workerpool_test

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	wp "github.com/azargarov/wpool"
)

// brRunPoolEndToEndBounded intentionally keeps submission retries, bounded capacity,
// and end-to-end completion in the measured region.
// This is the closest to your current benchmark semantics.
func brRunPoolEndToEndBounded(
	b *testing.B,
	workers, producers, segSize, segCount int,
	qt wp.QueueType,
	pinned bool,
	poolCapacity int,
	fn wp.JobFunc[any],
) {
	opts := wp.Options{
		Workers:      workers,
		QT:           qt,
		SegmentSize:  uint32(segSize),
		SegmentCount: uint32(segCount),
		PoolCapacity: uint32(poolCapacity),
		PinWorkers:   pinned,
	}

	pool := wp.NewPoolFromOptions[*wp.NoopMetrics, int](&wp.NoopMetrics{}, opts)

	b.Cleanup(func() {
		if err := pool.Shutdown(context.Background()); err != nil {
			b.Errorf("pool shutdown failed: %v", err)
		}
	})

	var execWG sync.WaitGroup
	execWG.Add(b.N)

	jobFn := func(int) error {
		_ = fn(0)
		execWG.Done()
		return nil
	}

	if getenvInt("OBSERVER", 0) > 0 {
		done := make(chan struct{})
		b.Cleanup(func() { close(done) })
		go observer(5*time.Second, done, pool)
	}

	var submitted atomic.Int64
	var prodWG sync.WaitGroup

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()

	for range producers {
		prodWG.Add(1)
		go func() {
			defer prodWG.Done()

			for {
				n := submitted.Add(1)
				if n > int64(b.N) {
					return
				}

				j := wp.Job[int]{Payload: 1, Fn: jobFn}

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

	b.ReportMetric(float64(b.N)/elapsed.Seconds()/1e3, "kj/s")
}

// brRunPoolSteadyState isolates scaling better:
// - pool exists before timing
// - producer goroutines exist before timing
// - producers are released by a start barrier
// - optional warmup happens before timing
func brRunPoolSteadyState(
	b *testing.B,
	workers, producers, segSize, segCount int,
	qt wp.QueueType,
	pinned bool,
	poolCapacity int,
	fn wp.JobFunc[any],
) {
	opts := wp.Options{
		Workers:      workers,
		QT:           qt,
		SegmentSize:  uint32(segSize),
		SegmentCount: uint32(segCount),
		PoolCapacity: uint32(poolCapacity),
		PinWorkers:   pinned,
	}

	pool := wp.NewPoolFromOptions[*wp.NoopMetrics, int](&wp.NoopMetrics{}, opts)

	b.Cleanup(func() {
		if err := pool.Shutdown(context.Background()); err != nil {
			b.Errorf("pool shutdown failed: %v", err)
		}
	})

	var execWG sync.WaitGroup
	execWG.Add(b.N)

	jobFn := func(int) error {
		_ = fn(0)
		execWG.Done()
		return nil
	}

	var submitted atomic.Int64
	var prodWG sync.WaitGroup
	startCh := make(chan struct{})

	for range producers {
		prodWG.Add(1)
		go func() {
			defer prodWG.Done()
			<-startCh

			for {
				n := submitted.Add(1)
				if n > int64(b.N) {
					return
				}

				j := wp.Job[int]{Payload: 1, Fn: jobFn}

				for {
					if err := pool.Submit(j); err == nil {
						break
					}
					runtime.Gosched()
				}
			}
		}()
	}

	// Optional warmup to wake workers and touch hot paths before timing.
	warmN := min(poolCapacity/2, 1024)
	for range warmN {
		j := wp.Job[int]{
			Payload: 1,
			Fn:      func(int) error { return nil },
		}
		j.SetPriority(1)

		for {
			if err := pool.Submit(j); err == nil {
				break
			}
			runtime.Gosched()
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()
	close(startCh)

	prodWG.Wait()
	execWG.Wait()

	elapsed := time.Since(start)
	b.StopTimer()

	b.ReportMetric(float64(b.N)/elapsed.Seconds()/1e3, "kj/s")
}

// BenchmarkPool_EndToEndBounded_Redesign keeps the current spirit of your existing
// throughput benchmark, but makes the naming explicit.
func BenchmarkPool_EndToEndBounded(b *testing.B) {
	workers := getenvInt("WORKERS", runtime.GOMAXPROCS(0))
	producers := getenvInt("PRODUCERS", runtime.GOMAXPROCS(0))
	pinned := getenvInt("PINNED", 0) > 0
	segSize := getenvInt("SEGSIZE", 64)
	segCount := getenvInt("SEGCOUNT", 32)
	capacity := getenvInt("POOLCAP", 64)

	for _, wl := range workloads {
		wl := wl
		b.Run(wl.name, func(b *testing.B) {
			brRunPoolEndToEndBounded(
				b,
				workers,
				producers,
				segSize,
				segCount,
				wp.SegmentedQueue,
				pinned,
				capacity,
				wl.fn,
			)
		})
	}
}

func BenchmarkPool_Single(b *testing.B) {
	workers := getenvInt("WORKERS", runtime.GOMAXPROCS(0))
	producers := getenvInt("PRODUCERS", runtime.GOMAXPROCS(0))
	pinned := getenvInt("PINNED", 0) > 0
	segSize := getenvInt("SEGSIZE", 64)
	segCount := getenvInt("SEGCOUNT", 32)
	capacity := getenvInt("POOLCAP", 64)

	wl := workloads[0]
	b.Run(wl.name, func(b *testing.B) {
		brRunPoolEndToEndBounded(
			b,
			workers,
			producers,
			segSize,
			segCount,
			wp.SegmentedQueue,
			pinned,
			capacity,
			wl.fn,
		)
	})

}

// BenchmarkPool_WorkerScale_Redesign answers:
// "What happens when I change workers while producer pressure is fixed?"
func BenchmarkPool_WorkerScale(b *testing.B) {
	segSize := getenvInt("SEGSIZE", 64)
	segCount := getenvInt("SEGCOUNT", 32)
	pinned := getenvInt("PINNED", 0) > 0

	workersList := []int{1, 2, 4, 8, 16}
	if v := getenvInt("WORKERS", 0); v > 0 {
		workersList = []int{v}
	}

	producers := getenvInt("PRODUCERS", 1)

	for _, wl := range workloads {
		// empty is fine here too, but the useful-work cases are the main point
		wl := wl
		b.Run(wl.name, func(b *testing.B) {
			for _, workers := range workersList {
				workers := workers
				name := fmt.Sprintf("w=%d/p=%d", workers, producers)

				b.Run(name, func(b *testing.B) {
					capacity := getenvInt("POOLCAP", max(1024, workers*64))
					brRunPoolSteadyState(
						b,
						workers,
						producers,
						segSize,
						segCount,
						wp.SegmentedQueue,
						pinned,
						capacity,
						wl.fn,
					)
				})
			}
		})
	}
}

// BenchmarkPool_ProducerPressure_Redesign answers:
// "What happens when I increase producers while workers stay fixed?"
func BenchmarkPool_ProducerPressure(b *testing.B) {
	segSize := getenvInt("SEGSIZE", 64)
	segCount := getenvInt("SEGCOUNT", 32)
	pinned := getenvInt("PINNED", 0) > 0

	workers := getenvInt("WORKERS", runtime.GOMAXPROCS(0))

	producersList := []int{1, 2, 4, 8, 16}
	if v := getenvInt("PRODUCERS", 0); v > 0 {
		producersList = []int{v}
	}

	wl := cpuWork

	for _, producers := range producersList {
		producers := producers
		name := fmt.Sprintf("w=%d/p=%d", workers, producers)

		b.Run(name, func(b *testing.B) {
			capacity := getenvInt("POOLCAP", max(1024, workers*64))
			brRunPoolSteadyState(
				b,
				workers,
				producers,
				segSize,
				segCount,
				wp.SegmentedQueue,
				pinned,
				capacity,
				wl,
			)
		})
	}
}

// BenchmarkPool_BoundedBackpressure_Redesign intentionally keeps capacity tight.
// Use this when you want to study submit contention and bounded-queue behavior.
func BenchmarkPool_BoundedBackpressure(b *testing.B) {
	workers := getenvInt("WORKERS", 4)
	producers := getenvInt("PRODUCERS", 4)
	pinned := getenvInt("PINNED", 0) > 0
	segSize := getenvInt("SEGSIZE", 64)
	segCount := getenvInt("SEGCOUNT", 32)
	capacity := getenvInt("POOLCAP", 64)

	wl := cpuWork

	brRunPoolSteadyState(
		b,
		workers,
		producers,
		segSize,
		segCount,
		wp.SegmentedQueue,
		pinned,
		capacity,
		wl,
	)
}

func observer(t time.Duration, done chan struct{}, p *wp.Pool[int, *wp.NoopMetrics]) {
	ticker := time.NewTicker(t)
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			print(p.StatSnapshot())
			println(p.MetricsStr())
			println(p.DebugHead())
		}
	}
}
