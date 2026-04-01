package workerpool_test

import (
	"context"
	wp "github.com/azargarov/wpool"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

type benchPayload struct {
	start time.Time //int64 // UnixNano
}

func BenchmarkPool_Latency(b *testing.B) {
	workers := getenvInt("WORKERS", runtime.GOMAXPROCS(0))
	segSize := getenvInt("SEGSIZE", wp.DefaultSegmentSize)
	segCount := getenvInt("SEGCOUNT", 32)
	pinned := getenvInt("PINNED", 0) > 0

	opts := wp.Options{
		Workers:       workers,
		QT:            wp.SegmentedQueue,
		SegmentSize:   uint32(segSize),
		SegmentCount:  uint32(segCount),
		PoolCapacity:  64,
		PinWorkers:    pinned,
		FlushInterval: 50 * time.Microsecond,
	}

	pool := wp.NewPoolFromOptions[*wp.NoopMetrics, benchPayload](
		&wp.NoopMetrics{},
		opts,
	)

	defer func() {
		if err := pool.Shutdown(context.Background()); err != nil {
			b.Errorf("pool shutdown failed: %v", err)
		}
	}()

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

	b.RunParallel(func(pb *testing.PB) {
		maxInflight := (int64)(workers * 32)

		for pb.Next() {
			for submitted.Load()-executed.Load() > maxInflight {
				runtime.Gosched()
			}

			payload := benchPayload{
				start: time.Now(),
			}

			j := job
			j.Payload = payload

			if err := pool.Submit(j); err != nil {
				panic(err)
			}
			submitted.Add(1)
		}
	})

	waitUntilB(b, 10*time.Second, func() bool {
		return executed.Load() == submitted.Load()
	})

	b.StopTimer()

	total := int(idx.Load())
	if total == 0 {
		b.Fatal("no latencies recorded")
	}

	samples := latencies[:total]

	slices.Sort(samples)

	//p50 := percentile(samples, 0.50)
	//p90 := percentile(samples, 0.90)
	//p99 := percentile(samples, 0.99)

	//b.Logf(
	//	"Latency → p50=%v p90=%v p99=%v | total=%d",
	//	p50,
	//	p90,
	//	p99,
	//	total,
	//)
}
