package workerpool_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	wp "github.com/azargarov/wpool"
)

func waitTimeout(wg *sync.WaitGroup, d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		wg.Wait()
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}


func TestPool_ExactOnce_Bursty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	opts := wp.Options{
		Workers:      16,
		QT:           wp.SegmentedQueue,
		SegmentSize:  1,
		SegmentCount: 1,
		PoolCapacity: 512,
	}

	pool := wp.NewPoolFromOptions[*wp.AtomicMetrics, int](
		&wp.AtomicMetrics{},
		opts,
	)
	defer pool.Shutdown(context.Background())

	const rounds = 20_000
	seen := make([]atomic.Uint32, rounds*3)

	var execWG sync.WaitGroup
	errCh := make(chan string, 128)

	reportErr := func(msg string) {
		select {
		case errCh <- msg:
		default:
		}
	}

	jobFn := func(id int) error {
		defer execWG.Done()
		c := seen[id].Add(1)
		if c != 1 {
			reportErr(fmt.Sprintf("duplicate execution: id=%d count=%d", id, c))
		}
		return nil
	}

	id := 0
	for r := 0; r < rounds; r++ {
		// Repeated 1,2,3 burst sizes
		burst := (r % 3) + 1
		for i := 0; i < burst; i++ {
			j := wp.Job[int]{Payload: id, Fn: jobFn}
			execWG.Add(1)
			if err := pool.Submit(j); err != nil {
				execWG.Done()
				t.Fatalf("submit failed: id=%d err=%v", id, err)
			}
			id++
		}
	}

	if !waitTimeout(&execWG, 15*time.Second) {
		bad := firstBadIDs(seen[:id], 1, 20)
		t.Fatalf("timeout waiting for executions\nmetrics: %s\nhead: %s\nbad ids: %v",
			pool.MetricsStr(),
			pool.DebugHead(),
			bad,
		)
	}

	select {
	case msg := <-errCh:
		t.Fatalf("execution error: %s\nmetrics: %s\nhead: %s", msg, pool.MetricsStr(), pool.DebugHead())
	default:
	}

	bad := firstBadIDs(seen[:id], 1, 20)
	if len(bad) > 0 {
		t.Fatalf("exact-once violation\nmetrics: %s\nhead: %s\nbad ids: %v",
			pool.MetricsStr(),
			pool.DebugHead(),
			bad,
		)
	}
}

func TestPool_ExactOnce_SmallSegments(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	type tc struct {
		name      string
		workers   int
		producers int
		segSize   int
		segCount  int
		n         int
	}

	cases := []tc{
		{"seg1", 16, 16, 1, 1, 100_000},
		{"seg2", 16, 16, 2, 1, 100_000},
		{"seg3", 16, 16, 3, 1, 100_000},
		{"seg5", 16, 16, 5, 1, 100_000},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			opts := wp.Options{
				Workers:      tc.workers,
				QT:           wp.SegmentedQueue,
				SegmentSize:  uint32(tc.segSize),
				SegmentCount: uint32(tc.segCount),
				PoolCapacity: 512,
				PinWorkers:   false,
			}

			pool := wp.NewPoolFromOptions[*wp.AtomicMetrics, int](
				&wp.AtomicMetrics{},
				opts,
			)
			defer pool.Shutdown(context.Background())

			seen := make([]atomic.Uint32, tc.n)
			var submitted atomic.Int64
			var executed atomic.Int64

			errCh := make(chan string, 128)
			reportErr := func(msg string) {
				select {
				case errCh <- msg:
				default:
				}
			}

			jobFn := func(id int) error {
				n := seen[id].Add(1)
				if n != 1 {
					reportErr(fmt.Sprintf("duplicate execution: id=%d count=%d", id, n))
				}
				executed.Add(1)
				return nil
			}

			var submitWG sync.WaitGroup
			start := make(chan struct{})

			chunk := tc.n / tc.producers
			rem := tc.n % tc.producers
			nextFrom := 0

			for p := 0; p < tc.producers; p++ {
				from := nextFrom
				size := chunk
				if p < rem {
					size++
				}
				to := from + size
				nextFrom = to

				submitWG.Add(1)
				go func(from, to int) {
					defer submitWG.Done()
					<-start

					for id := from; id < to; id++ {
						j := wp.Job[int]{Payload: id, Fn: jobFn}
						if err := pool.Submit(j); err != nil {
							reportErr(fmt.Sprintf("submit failed: id=%d err=%v", id, err))
							return
						}
						submitted.Add(1)
					}
				}(from, to)
			}

			close(start)
			submitWG.Wait()

			deadline := time.Now().Add(15 * time.Second)
			for time.Now().Before(deadline) {
				select {
				case msg := <-errCh:
					t.Fatalf("execution error: %s\nmetrics: %s\nhead: %s",
						msg, pool.MetricsStr(), pool.DebugHead())
				default:
				}

				if executed.Load() == submitted.Load() {
					break
				}
				time.Sleep(2 * time.Millisecond)
			}

			if executed.Load() != submitted.Load() {
				bad := firstBadIDs(seen, 1, 20)
				t.Fatalf("timeout waiting for executions\nsubmitted=%d executed=%d\nmetrics: %s\nhead: %s\nbad ids: %v",
					submitted.Load(), executed.Load(),
					pool.MetricsStr(), pool.DebugHead(), bad)
			}

			select {
			case msg := <-errCh:
				t.Fatalf("execution error: %s\nmetrics: %s\nhead: %s",
					msg, pool.MetricsStr(), pool.DebugHead())
			default:
			}

			bad := firstBadIDs(seen[:submitted.Load()], 1, 20)
			if len(bad) > 0 {
				t.Fatalf("exact-once violation\nsubmitted=%d executed=%d\nmetrics: %s\nhead: %s\nbad ids: %v",
					submitted.Load(), executed.Load(),
					pool.MetricsStr(), pool.DebugHead(), bad)
			}
		})
	}
}

func firstBadIDs(seen []atomic.Uint32, want uint32, limit int) []string {
	out := make([]string, 0, limit)
	for id := range seen {
		got := seen[id].Load()
		if got != want {
			out = append(out, fmt.Sprintf("id=%d seen=%d", id, got))
			if len(out) == limit {
				break
			}
		}
	}
	return out
}
func TestPool_SubmitCompletes_SmallSegments(t *testing.T) {
	opts := wp.Options{
		Workers:      16,
		QT:           wp.SegmentedQueue,
		SegmentSize:  3,
		SegmentCount: 1,
		PoolCapacity: 64,
	}

	pool := wp.NewPoolFromOptions[*wp.AtomicMetrics, int](&wp.AtomicMetrics{}, opts)
	defer pool.Shutdown(context.Background())

	const n = 100_000
	const producers = 16

	start := make(chan struct{})
	var submitWG sync.WaitGroup

	chunk := n / producers
	rem := n % producers
	nextFrom := 0

	for p := 0; p < producers; p++ {
		from := nextFrom
		size := chunk
		if p < rem {
			size++
		}
		to := from + size
		nextFrom = to

		submitWG.Add(1)
		go func(from, to int) {
			defer submitWG.Done()
			<-start
			for id := from; id < to; id++ {
				j := wp.Job[int]{Payload: id, Fn: func(int) error { return nil }}
				if err := pool.Submit(j); err != nil {
					t.Errorf("submit failed: %v", err)
					return
				}
			}
		}(from, to)
	}

	close(start)

	done := make(chan struct{})
	go func() {
		defer close(done)
		submitWG.Wait()
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("submitters hung\nmetrics: %s\nhead: %s", pool.MetricsStr(), pool.DebugHead())
	}
}