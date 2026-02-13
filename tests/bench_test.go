package workerpool_test

import (
	"context"
	"fmt"
	wp "github.com/azargarov/wpool"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
	"math/rand"
)

func getenvInt(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func startPprof() {
	go func() {
		log.Println("pprof listening on :6060")
		_ = http.ListenAndServe(":6060", nil)
	}()
}

func getPool(opt wp.Options) *wp.Pool[int, *wp.NoopMetrics] {
	m := &wp.NoopMetrics{}
	return wp.NewPoolFromOptions[*wp.NoopMetrics, int](m, opt)
}

func BenchmarkPool(b *testing.B) {
	segSize := 4096
	cases := []struct {
		name         string
		workers      int
		segmentSize  int
		segmentCount int
		pinned       bool
	}{
		{"W=GOMAX_S,C=8", runtime.GOMAXPROCS(0), segSize, 8, false},
		{"W=GOMAX_S,C=16", runtime.GOMAXPROCS(0), segSize, 16, false},
		{"W=2xGOMAX_S,C=32", runtime.GOMAXPROCS(0) , segSize, 32, false},
		{"W=GOMAX_S,C=32,PINNED", runtime.GOMAXPROCS(0) , segSize, 32, true},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			PoolBench(b, tc.workers, tc.segmentSize, tc.segmentCount, wp.RevolvingBucketQueue ,tc.pinned)
		})
	}
}

func BenchmarkPool_single(b *testing.B) {
	workers := getenvInt("WORKERS", runtime.GOMAXPROCS(0)) 
	segmentSize := getenvInt("SEGSIZE", 4096)
	segmentCount := getenvInt("SEGCOUNT", 128)
	pinned := getenvInt("PINNED", 0) > 0

	b.Run(
		fmt.Sprintf("W=%d,C=%d,PINNED=%t", workers, segmentCount, pinned),
		func(b *testing.B) {
			PoolBench(b, workers, segmentSize, segmentCount,wp.RevolvingBucketQueue ,pinned)
		},
	)
}
func BenchmarkPool_segmented_single(b *testing.B) {
	workers := getenvInt("WORKERS", runtime.GOMAXPROCS(0)) *4 
	segmentSize := getenvInt("SEGSIZE", 2048)
	segmentCount := getenvInt("SEGCOUNT", 1024)
	pinned := getenvInt("PINNED", 0) > 0

	b.Run(
		fmt.Sprintf("W=%d,C=%d,PINNED=%t", workers, segmentCount, pinned),
		func(b *testing.B) {
			PoolBench(b, workers, segmentSize, segmentCount,wp.SegmentedQueue ,pinned)
		},
	)
}
// PoolBench measures sustained throughput of the worker pool.
//
// It intentionally reports Mjobs/sec using manual timing.
// testing.B's ns/op is not representative for long-running,
// parallel throughput benchmarks.
func PoolBench(b *testing.B, workers, segSize, segCount int, qt wp.QueueType,  pinned bool) {
	if os.Getenv("PPROF") == "1" {
		startPprof()
		runtime.SetBlockProfileRate(1)
		runtime.SetMutexProfileFraction(1)
	}

	opts := wp.Options{
		Workers:      workers,
		QT:           qt,
		SegmentSize:  uint32(segSize),
		SegmentCount: uint32(segCount),
		PoolCapacity: 1024,
		PinWorkers:   pinned,
	}

	pool := getPool(opts)
	defer func() {
		pool.Shutdown(context.Background())
		}()
		
		var executed atomic.Int64
		var submitted atomic.Int64
		
		job := wp.Job[int]{Fn: func(int) error {
			executed.Add(1)
			for i := range(1){
				_ = i * i
			}
			return nil
		}}
		
		if os.Getenv("OBSERVER") == "1" {
			done := make(chan struct{})
			defer close(done)
			go observer(5*time.Second, &executed, &submitted, done, pool)
		}
		
		b.ResetTimer()
		start := time.Now()
		//i:= 1
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				prio := (wp.JobPriority)(rand.Intn(62) + 1)
				job.SetPriority(prio)
				if err := pool.Submit(job); err != nil {
					panic(err)
				}
				submitted.Add(1)
			}
		})
		
		// Wait for all submitted jobs to complete.
		for executed.Load() < submitted.Load() {
			time.Sleep(100 * time.Microsecond)
		}
		
		elapsed := time.Since(start)
		mps := float64(executed.Load()) / elapsed.Seconds() / 1e6
		b.ReportMetric(mps, "Mjobs/sec")
		
		b.Logf(
			"MyPoolBench → %.2f Mjobs/sec in %.2fs, jobs total: %d",
			mps,
			elapsed.Seconds(),
			executed.Load(),
		)
	b.Log(pool.StatSnapshot())
}

func observer(
	delay time.Duration,
	executed *atomic.Int64,
	submitted *atomic.Int64,
	done <-chan struct{},
	pool *wp.Pool[int, *wp.NoopMetrics],
) {
	t := time.NewTicker(delay)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			fmt.Printf(
				"Idle Workers: %d, Active Workers: %d\n",
				pool.GetIdleLen(),
				pool.ActiveWorkers(),
			)
			fmt.Printf(
				"progress: executed=%d submitted=%d\n",
				executed.Load(),
				submitted.Load(),
			)
			wp.ShedDumpStats()
		case <-done:
			return
		}
	}
}
