package workerpool_test

import (
	"slices"
	"context"
	//"fmt"
	wp "github.com/azargarov/wpool"
	"math/rand"
	//"os"
	"runtime"
	//"sort"
	//"strconv"
	"sync/atomic"
	"testing"
	"time"
)

type benchPayload struct {
	start time.Time//int64 // UnixNano
}


func BenchmarkPool_Latency(b *testing.B) {
	workers := getenvInt("WORKERS", runtime.GOMAXPROCS(0)) 
	segSize := getenvInt("SEGSIZE", wp.DefaultSegmentSize)
	segCount := getenvInt("SEGCOUNT", 32)
	pinned := getenvInt("PINNED", 0) > 0
    fixedPrio := getenvInt("FIXEDP", 0) > 0

    maxPrio := 62
    if fixedPrio {
        maxPrio = 1
    }

	opts := wp.Options{
		Workers:      workers,
		QT:           wp.RevolvingBucketQueue,
		SegmentSize:  uint32(segSize),
		SegmentCount: uint32(segCount),
		PoolCapacity: 4096,
		PinWorkers:   pinned,
		FlushInterval: 50*time.Microsecond,
	}

	pool := wp.NewPoolFromOptions[*wp.NoopMetrics, benchPayload](
		&wp.NoopMetrics{},
		opts,
	)
	defer pool.Shutdown(context.Background())

	var executed atomic.Int64
	var submitted atomic.Int64
	var idx atomic.Int64

	latencies := make([]int64, b.N)

	job := wp.Job[benchPayload]{
		Fn: func(p benchPayload) error {
			dur := time.Since(p.start).Nanoseconds()
			
			i := idx.Add(1) - 1
			if i < int64(len(latencies)) {
				latencies[i] = dur
			}

			executed.Add(1)
			return nil
		},
	}

	b.ResetTimer()
	start := time.Now()
	
	b.RunParallel(func(pb *testing.PB) {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		maxInflight := (int64)(workers * 32)
		
		for pb.Next() {
			for submitted.Load()-executed.Load() > maxInflight {
				runtime.Gosched()
			}	

			prio := wp.JobPriority(r.Intn(maxPrio) + 1)
			payload := benchPayload{
				start: time.Now(),
			}
			
			j := job
			j.Payload = payload
			j.SetPriority(prio)

			//start = time.Now()	
			if err := pool.Submit(j); err != nil {
				panic(err)
			}
			submitted.Add(1)
		}
	})

	waitUntilB(b, 10*time.Second, func() bool {
		return executed.Load() == int64(b.N)
	})

	elapsed := time.Since(start)
	mps := float64(executed.Load()) / elapsed.Seconds() / 1e6
	b.ReportMetric(mps, "Mjobs/sec")


	total := int(idx.Load())
	if total == 0 {
		b.Fatal("no latencies recorded")
	}

	samples := latencies[:total]

	slices.Sort(samples)

	p50 := percentile(samples,0.50)
	p90 := percentile(samples,0.90)
	p99 := percentile(samples,0.99)

	b.ReportMetric(float64(p50.Nanoseconds()), "p50_ns")
	b.ReportMetric(float64(p90.Nanoseconds()), "p90_ns")
	b.ReportMetric(float64(p99.Nanoseconds()), "p99_ns")

	b.Logf(
		"Latency → p50=%v p90=%v p99=%v | %.2f Mjobs/sec | total=%d",
		p50,
		p90,
		p99,
		mps,
		total,
	)
}
