package workerpool_test

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	wp "github.com/azargarov/wpool"
)


func firstBadIDsAtomic(seen []atomic.Uint32, want uint32, limit int) []string {
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

func TestSegmentedQueue_ExactOnce_MPMC(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	type tc struct {
		name      string
		producers int
		consumers int
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
				Workers:      runtime.GOMAXPROCS(0),
				QT:           wp.SegmentedQueue,
				SegmentSize:  uint32(tc.segSize),
				SegmentCount: uint32(tc.segCount),
				PoolCapacity: 512,
				PinWorkers:   false,
			}

			q := wp.NewSegmentedQ[int](opts, nil)
			defer q.Close()

			seen := make([]atomic.Uint32, tc.n)

			var submitted atomic.Int64
			var consumed atomic.Int64

			var prodWG sync.WaitGroup
			var consWG sync.WaitGroup
			var doneWG sync.WaitGroup

			start := make(chan struct{})
			stop := make(chan struct{})

			errCh := make(chan string, 128)
			reportErr := func(msg string) {
				select {
				case errCh <- msg:
				default:
				}
			}

			// Producers
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

				prodWG.Add(1)
				go func(from, to int) {
					defer prodWG.Done()
					<-start

					for id := from; id < to; id++ {
						j := wp.Job[int]{
							Payload: id,
							Fn:      nil, // queue-only test; no worker pool execution
						}
						if err := q.Push(j); err != nil {
							reportErr(fmt.Sprintf("push failed: id=%d err=%v", id, err))
							return
						}
						submitted.Add(1)
					}
				}(from, to)
			}

			// Consumers
			for c := 0; c < tc.consumers; c++ {
				consWG.Add(1)
				go func() {
					defer consWG.Done()
					<-start

					for {
						// Fast exit: all jobs consumed.
						if consumed.Load() >= int64(tc.n) {
							return
						}

						select {
						case <-stop:
							return
						default:
						}

						batch, ok := q.BatchPop()
						if !ok {
							runtime.Gosched()
							continue
						}

						for _, job := range batch.Jobs {
							id := job.Payload
							if id < 0 || id >= tc.n {
								reportErr(fmt.Sprintf("out-of-range id=%d", id))
								continue
							}

							n := seen[id].Add(1)
							if n != 1 {
								reportErr(fmt.Sprintf("duplicate consume: id=%d count=%d", id, n))
							}
							consumed.Add(1)
						}

						q.OnBatchDone(batch)
					}
				}()
			}

			doneWG.Add(1)
			go func() {
				defer doneWG.Done()
				prodWG.Wait()

				deadline := time.Now().Add(15 * time.Second)
				for time.Now().Before(deadline) {
					if consumed.Load() >= int64(tc.n) {
						close(stop)
						return
					}
					time.Sleep(2 * time.Millisecond)
				}
				close(stop)
			}()

			close(start)

			doneWG.Wait()
			consWG.Wait()

			select {
			case msg := <-errCh:
				t.Fatalf("queue exact-once error: %s\nsubmitted=%d consumed=%d\nhead=%s",
					msg, submitted.Load(), consumed.Load(), q.DebugHead())
			default:
			}

			if submitted.Load() != int64(tc.n) {
				t.Fatalf("submitted mismatch: got=%d want=%d\nhead=%s",
					submitted.Load(), tc.n, q.DebugHead())
			}

			if consumed.Load() != int64(tc.n) {
				bad := firstBadIDsAtomic(seen, 1, 20)
				t.Fatalf("consumed mismatch: got=%d want=%d\nhead=%s\nbad ids=%v",
					consumed.Load(), tc.n, q.DebugHead(), bad)
			}

			bad := firstBadIDsAtomic(seen, 1, 20)
			if len(bad) > 0 {
				t.Fatalf("exact-once violation\nsubmitted=%d consumed=%d\nhead=%s\nbad ids=%v",
					submitted.Load(), consumed.Load(), q.DebugHead(), bad)
			}
		})
	}
}

func TestSegmentedQueue_ExactOnce_Bursty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	opts := wp.Options{
		Workers:      runtime.GOMAXPROCS(0),
		QT:           wp.SegmentedQueue,
		SegmentSize:  1,
		SegmentCount: 1,
		PoolCapacity: 512,
	}

	q := wp.NewSegmentedQ[int](opts, nil)
	defer q.Close()

	const total = 60_000
	seen := make([]atomic.Uint32, total)

	var consumed atomic.Int64
	stop := make(chan struct{})
	errCh := make(chan string, 128)

	reportErr := func(msg string) {
		select {
		case errCh <- msg:
		default:
		}
	}

	var consWG sync.WaitGroup
	for i := 0; i < 16; i++ {
		consWG.Add(1)
		go func() {
			defer consWG.Done()
			for {
				if consumed.Load() >= total {
					return
				}
				select {
				case <-stop:
					return
				default:
				}

				batch, ok := q.BatchPop()
				if !ok {
					runtime.Gosched()
					continue
				}

				for _, job := range batch.Jobs {
					id := job.Payload
					n := seen[id].Add(1)
					if n != 1 {
						reportErr(fmt.Sprintf("duplicate consume: id=%d count=%d", id, n))
					}
					consumed.Add(1)
				}
				q.OnBatchDone(batch)
			}
		}()
	}

	id := 0
	for round := 0; id < total; round++ {
		burst := (round % 3) + 1 // 1,2,3,1,2,3...
		for i := 0; i < burst && id < total; i++ {
			j := wp.Job[int]{Payload: id}
			if err := q.Push(j); err != nil {
				t.Fatalf("push failed: id=%d err=%v", id, err)
			}
			id++
		}
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if consumed.Load() >= total {
			close(stop)
			consWG.Wait()

			select {
			case msg := <-errCh:
				t.Fatalf("queue exact-once error: %s\nconsumed=%d\nhead=%s",
					msg, consumed.Load(), q.DebugHead())
			default:
			}

			bad := firstBadIDsAtomic(seen, 1, 20)
			if len(bad) > 0 {
				t.Fatalf("exact-once violation\nconsumed=%d\nhead=%s\nbad ids=%v",
					consumed.Load(), q.DebugHead(), bad)
			}
			return
		}
		time.Sleep(2 * time.Millisecond)
	}

	close(stop)
	consWG.Wait()

	bad := firstBadIDsAtomic(seen, 1, 20)
	t.Fatalf("timeout waiting for consume\nconsumed=%d want=%d\nhead=%s\nbad ids=%v",
		consumed.Load(), total, q.DebugHead(), bad)
}