package workerpool_test

import (
	"context"
	"fmt"
	//"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	wp "github.com/azargarov/wpool"
)

func TestPool_Health_Stress(t *testing.T) {
	type workFn struct {
		name string
		fn   wp.JobFunc[int]
	}

	emptyWork := func(int) error { return nil }

	cpuWork := func(int) error {
		x := 0
		for i := range 100000 {
			x += i * i
		}
		_ = x
		return nil
	}

	works := []workFn{
		{"empty", emptyWork},
		{"cpu", cpuWork},
	}

	cases := []struct {
		name         string
		workers      int
		segmentSize  uint32
		segmentCount uint32
		jobs         int
		producers    int
	}{
		{"tiny_seg", 16, 16, 32, 200_000, 8},
		{"normal_seg", 16, 4096, 32, 200_000, 8},
	}

	for _, tc := range cases {
		tc := tc
		for _, wf := range works {
			wf := wf

			t.Run(tc.name+"_"+wf.name, func(t *testing.T) {
				//t.Parallel()

				opts := wp.Options{
					Workers:      tc.workers,
					QT:           wp.SegmentedQueue,
					SegmentSize:  tc.segmentSize,
					SegmentCount: tc.segmentCount,
					PoolCapacity: 512,
				}

				pool := wp.NewPoolFromOptions[*wp.AtomicMetrics, int](&wp.AtomicMetrics{}, opts)
				//defer pool.Shutdown(context.Background())

				defer func() {
					if err := pool.Shutdown(context.Background()); err != nil {
						t.Errorf("pool shutdown failed: %v", err)
					}
				}()

				var submitted atomic.Int64
				var executed atomic.Int64
				var dupes atomic.Int64

				seen := make([]atomic.Uint32, tc.jobs)

				var wg sync.WaitGroup
				wg.Add(tc.jobs)

				jobFn := func(id int) error {
					if !seen[id].CompareAndSwap(0, 1) {
						dupes.Add(1)
					}
					_ = wf.fn(id)
					executed.Add(1)
					wg.Done()
					return nil
				}

				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()

				// progress watchdog
				watchdogDone := make(chan struct{})
				go func() {
					ticker := time.NewTicker(1 * time.Second)
					defer ticker.Stop()

					lastExec := executed.Load()
					lastMove := time.Now()

					for {
						select {
						case <-watchdogDone:
							return
						case <-ctx.Done():
							return
						case <-ticker.C:
							cur := executed.Load()
							if cur != lastExec {
								lastExec = cur
								lastMove = time.Now()
							}
							if time.Since(lastMove) > 10*time.Second {
								panic(fmt.Sprintf(
									"no progress for >10s; submitted=%d executed=%d dupes=%d stats=%s metrics=%s head=%s",
									submitted.Load(),
									executed.Load(),
									dupes.Load(),
									pool.StatSnapshot(),
									pool.MetricsStr(),
									pool.DebugHead(),
								))
							}
						}
					}
				}()

				// producers
				var prodWG sync.WaitGroup
				perProducer := tc.jobs / tc.producers
				rem := tc.jobs % tc.producers

				nextID := 0
				for p := 0; p < tc.producers; p++ {
					n := perProducer
					if p < rem {
						n++
					}
					start := nextID
					nextID += n

					prodWG.Add(1)
					go func(start, n int) {
						defer prodWG.Done()
						for i := 0; i < n; i++ {
							id := start + i
							j := wp.Job[int]{Payload: id, Fn: jobFn}
							if err := pool.Submit(j); err != nil {
								panic(err)
							}
							submitted.Add(1)
						}
					}(start, n)
				}

				prodWG.Wait()

				done := make(chan struct{})
				go func() {
					wg.Wait()
					close(done)
				}()

				select {
				case <-done:
				case <-ctx.Done():
					t.Fatalf("timeout: submitted=%d executed=%d dupes=%d stats=%s metrics=%s head=%s",
						submitted.Load(),
						executed.Load(),
						dupes.Load(),
						pool.StatSnapshot(),
						pool.MetricsStr(),
						pool.DebugHead(),
					)
				}

				close(watchdogDone)

				if got, want := int(submitted.Load()), tc.jobs; got != want {
					t.Fatalf("submitted=%d want=%d", got, want)
				}
				if got, want := int(executed.Load()), tc.jobs; got != want {
					t.Fatalf("executed=%d want=%d", got, want)
				}
				if dupes.Load() != 0 {
					t.Fatalf("duplicates=%d", dupes.Load())
				}

				for i := 0; i < tc.jobs; i++ {
					if seen[i].Load() != 1 {
						t.Fatalf("job %d missing", i)
					}
				}
			})
		}
	}
}
