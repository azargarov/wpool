package workerpool_test

import (
	"context"
	wp "github.com/azargarov/wpool"
	"math/rand"
	"runtime"
	"sort"
	"sync/atomic"
	"testing"
	"time"
)

type fairnessPayload struct {
	start int64
	prio  wp.JobPriority
}


func BenchmarkPool_Fairness(b *testing.B) {
	workers := getenvInt("WORKERS", runtime.GOMAXPROCS(0))
	segCount := getenvInt("SEGCOUNT", 64)

	opts := wp.Options{
		Workers:      workers,
		QT:           wp.SegmentedQueue,
		SegmentSize:  4096,
		SegmentCount: uint32(segCount),
		PoolCapacity: 4096,
	}

	pool := wp.NewPoolFromOptions[*wp.NoopMetrics, fairnessPayload](
		&wp.NoopMetrics{},
		opts,
	)
	defer pool.Shutdown(context.Background())

	var executed atomic.Int64
	var submitted atomic.Int64
	var idx atomic.Int64

	type record struct {
		prio wp.JobPriority
		dur  int64
	}

	records := make([]record, b.N)

	job := wp.Job[fairnessPayload]{
		Fn: func(p fairnessPayload) error {
			end := time.Now().UnixNano()
			dur := end - p.start

			i := idx.Add(1) - 1
			if i < int64(len(records)) {
				records[i] = record{
					prio: p.prio,
					dur:  dur,
				}
			}

			executed.Add(1)
			return nil
		},
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		maxInflight := int64(workers * 32)

		for pb.Next() {
			for submitted.Load()-executed.Load() > maxInflight {
        		runtime.Gosched()
    		}
			// realistic skewed distribution
			x := r.Float64()
			var prio wp.JobPriority

			switch {
			case x < 0.1:
				prio = 60 // high
			case x < 0.4:
				prio = 30 // medium
			default:
				prio = 5 // low
			}

			payload := fairnessPayload{
				start: time.Now().UnixNano(),
				prio:  prio,
			}

			j := job
			j.Payload = payload
			j.SetPriority(prio)

			if err := pool.Submit(j); err != nil {
				panic(err)
			}
			submitted.Add(1)
		}
	})

	for executed.Load() < submitted.Load() {
		time.Sleep(100 * time.Microsecond)
	}

	total := int(idx.Load())
	samples := records[:total]

	high := make([]int64, 0)
	medium := make([]int64, 0)
	low := make([]int64, 0)

	for _, r := range samples {
		switch {
		case r.prio >= 48:
			high = append(high, r.dur)
		case r.prio >= 16:
			medium = append(medium, r.dur)
		default:
			low = append(low, r.dur)
		}
	}

	report := func(name string, arr []int64) {
		if len(arr) == 0 {
			return
		}
		sort.Slice(arr, func(i, j int) bool { return arr[i] < arr[j] })

		p := func(q float64) time.Duration {
			pos := int(float64(len(arr)-1) * q)
			return time.Duration(arr[pos])
		}

		b.Logf(
			"%s → count=%d p50=%v p90=%v p99=%v",
			name,
			len(arr),
			p(0.50),
			p(0.90),
			p(0.99),
		)
	}

	report("HIGH", high)
	report("MEDIUM", medium)
	report("LOW", low)
}
