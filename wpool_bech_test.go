package workerpool_test

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"slices"
	//"sort"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	wp "github.com/azargarov/go-utils/wpool"
)

var benchQueueTypes = []wp.QueueType{
	wp.Fifo,
	wp.BucketQueue,
	wp.Priority,
}

func newBenchPool(qt wp.QueueType, workers, queueSize int) *wp.Pool[int] {
	opts := wp.Options{
		Workers:    workers,
		QueueSize:  queueSize,
		AgingRate:  0.3,
		RebuildDur: 10 * time.Millisecond,
		QT:         qt,
	}
	return wp.NewPool[int](opts, wp.RetryPolicy{Attempts: 1})
}

// 1) Submit-path throughput: fixed number of jobs, variable submitters

// runSubmitters pushes exactly jobsToRun jobs into the pool using `submitters` goroutines.
func runSubmitters(pool *wp.Pool[int], submitters, jobsToRun int) {
	var wg sync.WaitGroup
	wg.Add(submitters)

	perSubmitter := jobsToRun / submitters
	job := wp.Job[int]{Fn: func(int) error { return nil }}

	for range submitters {
		go func() {
			defer wg.Done()
			for range perSubmitter {
				for {
					if err := pool.Submit(job, 0); err == nil {
						break
					}
					runtime.Gosched()
				}
			}
		}()
	}

	wg.Wait()
}

// BenchmarkSubmitPath measures how fast different queue types can ingest jobs
// under different numbers of concurrent submitters.
// Submit() + queue push performance.
func BenchmarkSubmitPath(b *testing.B) {
	const jobsToRun = 2_000_000
	submitterCounts := []int{1, 2, 8, 64, 128}

	for _, qt := range benchQueueTypes {
		for _, submitters := range submitterCounts {
			name := fmt.Sprintf("%s/%d_submitters", qt, submitters)
			b.Run(name, func(b *testing.B) {
				workers := runtime.NumCPU()
				queueSize := jobsToRun * 2

				pool := newBenchPool(qt, workers, queueSize)
				defer pool.Shutdown(context.Background())

				b.ReportAllocs()
				b.ResetTimer()

				start := time.Now()
				runSubmitters(pool, submitters, jobsToRun)
				elapsed := time.Since(start)

				b.StopTimer()

				mps := float64(jobsToRun) / elapsed.Seconds() / 1e6
				b.ReportMetric(mps, "Mjobs/sec")
				b.Logf("qt=%s submitters=%d → %.2f Mjobs/sec in %.2fs",
					qt, submitters, mps, elapsed.Seconds())
			})
		}
	}
}

//
// 2) End-to-end throughput: submit → queue → worker → execute → done
//

// BenchmarkEndToEndThroughput measures how many jobs per second can be fully
// processed end-to-end. This includes worker scheduling and execution.
func BenchmarkEndToEndThroughput(b *testing.B) {
	for _, qt := range benchQueueTypes {
		b.Run(qt.String(), func(b *testing.B) {
			const jobsToRun = 1_000_000

			workers := runtime.GOMAXPROCS(0) //runtime.NumCPU()
			queueSize := jobsToRun * 2

			pool := newBenchPool(qt, workers, queueSize)
			defer pool.Shutdown(context.Background())

			var done int64
			job := wp.Job[int]{
				Fn: func(int) error {
					atomic.AddInt64(&done, 1)
					return nil
				},
			}

			b.ReportAllocs()
			b.ResetTimer()
			start := time.Now()

			for range jobsToRun {
				for {
					if err := pool.Submit(job, 0); err == nil {
						break
					}
					runtime.Gosched()
				}
			}

			// wait until workers finish
			for atomic.LoadInt64(&done) != int64(jobsToRun) {
				runtime.Gosched()
			}

			elapsed := time.Since(start)
			b.StopTimer()

			mps := float64(jobsToRun) / elapsed.Seconds() / 1e6
			b.ReportMetric(mps, "Mjobs/sec")
			b.Logf("qt=%s end-to-end → %.2f Mjobs/sec in %.2fs",
				qt, mps, elapsed.Seconds())
		})
	}
}

//
// 3) Simple latency stats helper
//

type latencyStats struct {
	mu      sync.Mutex
	samples []time.Duration
}

func (ls *latencyStats) record(d time.Duration) {
	ls.mu.Lock()
	ls.samples = append(ls.samples, d)
	ls.mu.Unlock()
}

func (ls *latencyStats) summary() (min, max, p50, p95, p99 time.Duration) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	if len(ls.samples) == 0 {
		return 0, 0, 0, 0, 0
	}

	s := make([]time.Duration, len(ls.samples))
	copy(s, ls.samples)
	slices.Sort(s)

	min = s[0]
	max = s[len(s)-1]

	get := func(p float64) time.Duration {
		if len(s) == 1 {
			return s[0]
		}
		idx := int(math.Round(p * float64(len(s)-1))) // 0..1 → index
		if idx < 0 {
			idx = 0
		}
		if idx >= len(s) {
			idx = len(s) - 1
		}
		return s[idx]
	}

	p50 = get(0.50)
	p95 = get(0.95)
	p99 = get(0.99)
	return
}

//
// 4) Submit latency under light contention
//

// BenchmarkSubmitLatency measures how long Submit() itself takes under a light
// parallel load (2 submitters). This captures queue push + locks/atomics.
func BenchmarkSubmitLatency(b *testing.B) {
	const totalOps = 1_000_000
	const submitters = 8

	for _, qt := range benchQueueTypes {
		b.Run(qt.String(), func(b *testing.B) {
			workers := runtime.NumCPU()
			queueSize := totalOps * 2

			pool := newBenchPool(qt, workers, queueSize)
			defer pool.Shutdown(context.Background())
			time.Sleep(500)

			var ls latencyStats
			job := wp.Job[int]{Fn: func(int) error { return nil }}

			perSubmitter := totalOps / submitters

			b.ReportAllocs()
			b.ResetTimer()

			var wg sync.WaitGroup
			wg.Add(submitters)

			for range submitters {
				go func() {
					defer wg.Done()
					for range perSubmitter {
						start := time.Now()
						for {
							if err := pool.Submit(job, 0); err == nil {
								break
							}
							runtime.Gosched()
						}
						ls.record(time.Since(start))
					}
				}()
			}

			wg.Wait()
			b.StopTimer()

			min, max, p50, p95, p99 := ls.summary()
			b.Logf("qt=%s submit latency: min=%v p50=%v p95=%v p99=%v max=%v",
				qt, min, p50, p95, p99, max)
		})
	}
}

//
// 5) End-to-end latency with workers
//

// BenchmarkEndToEndLatency measures latency from Submit() until the job
// actually executes (as seen by the worker).
func BenchmarkEndToEndLatency(b *testing.B) {
	const totalOps = 1_000_000

	for _, qt := range benchQueueTypes {
		b.Run(qt.String(), func(b *testing.B) {
			workers := runtime.NumCPU()
			queueSize := totalOps * 2

			pool := newBenchPool(qt, workers, queueSize)
			defer pool.Shutdown(context.Background())

			var ls latencyStats
			var inFlight sync.WaitGroup

			// timeSent[i] = when we submitted job i
			timeSent := make([]time.Time, totalOps)
			var idx int64

			job := wp.Job[int]{
				Fn: func(i int) error {
					start := timeSent[i]
					if !start.IsZero() {
						ls.record(time.Since(start))
					}
					inFlight.Done()
					return nil
				},
			}

			b.ReportAllocs()
			b.ResetTimer()

			inFlight.Add(totalOps)

			// single submitter to avoid too much noise
			for range totalOps {
				id := int(atomic.AddInt64(&idx, 1) - 1)
				timeSent[id] = time.Now()

				for {
					if err := pool.Submit(job, float64(id)); err == nil {
						break
					}
					runtime.Gosched()
				}
			}

			inFlight.Wait()
			b.StopTimer()

			min, max, p50, p95, p99 := ls.summary()
			b.Logf("qt=%s end-to-end latency: min=%v p50=%v p95=%v p99=%v max=%v",
				qt, min, p50, p95, p99, max)
		})
	}
}

//
// 6) Mixed workload (fast + slow jobs)
//

// BenchmarkMixedWorkload simulates a realistic mix:
// - 70% fast jobs
// - 30% slower CPU-bound jobs
func BenchmarkMixedWorkload(b *testing.B) {
	const totalOps = 200_000
	const slowEvery = 5 // every 3th job is slow
	var prio float64
	for _, qt := range benchQueueTypes {
		b.Run(qt.String(), func(b *testing.B) {
			workers := runtime.GOMAXPROCS(0)
			queueSize := totalOps + 1

			pool := newBenchPool(qt, workers, queueSize)
			defer pool.Shutdown(context.Background())

			var done int64

			fastJob := wp.Job[int]{
				Fn: func(int) error {
					atomic.AddInt64(&done, 1)
					return nil
				},
			}

			slowJob := wp.Job[int]{
				Fn: func(int) error {
					// tiny CPU load
					for i := range 50 {
						_ = math.Sqrt(float64(i * i))
					}
					atomic.AddInt64(&done, 1)
					return nil
				},
			}

			b.ReportAllocs()
			b.ResetTimer()
			start := time.Now()

			for i := range totalOps {
				var j wp.Job[int]
				if i%slowEvery == 0 {
					j = slowJob
					prio = 0
				} else {
					j = fastJob
					prio = 90
				}

				for {
					if err := pool.Submit(j, prio); err == nil {
						break
					}
					runtime.Gosched()
				}
			}

			for atomic.LoadInt64(&done) != totalOps {
				runtime.Gosched()
			}

			elapsed := time.Since(start)
			b.StopTimer()

			mps := float64(totalOps) / elapsed.Seconds() / 1e6
			b.ReportMetric(mps, "Mjobs/sec")
			b.Logf("qt=%s mixed workload → %.2f Mjobs/sec in %.2fs",
				qt, mps, elapsed.Seconds())
		})
	}
}

func BenchmarkManyPriorities(b *testing.B) {
	const totalOps = 500_000
	const slowEvery = 3
	const maxPrio = wp.BQmaxPriority

	for _, qt := range benchQueueTypes {
		b.Run(qt.String(), func(b *testing.B) {
			workers := runtime.GOMAXPROCS(0)
			pool := newBenchPool(qt, workers, totalOps+1)
			defer pool.Shutdown(context.Background())

			var done int64

			fastJob := wp.Job[int]{Fn: func(int) error {
				atomic.AddInt64(&done, 1)
				return nil
			}}

			slowJob := wp.Job[int]{Fn: func(int) error {
				for i := range 10_000 {
					_ = i * i
				}
				atomic.AddInt64(&done, 1)
				return nil
			}}

			b.ResetTimer()
			start := time.Now()

			for i := range totalOps {
				var job wp.Job[int]
				var prio float64

				if i%slowEvery == 0 {
					job = slowJob
					prio = float64(rand.Intn(maxPrio))
				} else {
					job = fastJob
					prio = 0 //
				}

				for pool.Submit(job, prio) != nil {
					runtime.Gosched()
				}
			}

			for atomic.LoadInt64(&done) != totalOps {
				runtime.Gosched()
			}

			elapsed := time.Since(start)
			b.StopTimer()

			mps := float64(totalOps) / elapsed.Seconds() / 1e6
			b.ReportMetric(mps, "Mjobs/sec")

			b.Logf("qt=%s → %.2f Mjobs/sec (%.2fs)",
				qt, mps, elapsed.Seconds())
		})
	}
}
func BenchmarkRawChan(b *testing.B) {
	ch := make(chan struct{}, 1024)
	go func() {
		for range ch {
		}
	}()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch <- struct{}{}
	}
}
