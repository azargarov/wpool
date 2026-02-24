package workerpool_test

import (
	"runtime"
	"sync"
	"testing"

	wp "github.com/azargarov/wpool"
)

func TestSegmentedQ_ABAUnderConcurrency(t *testing.T) {
	opts := wp.Options{
		SegmentSize: 4,
	}
	q := wp.NewSegmentedQ[int](opts, nil)

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	const producers = 100
	const consumers = 50
	const pushesPerProducer = 10000
	const popsPerConsumer = 20000

	// Producers
	for i := range producers {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			for j := range pushesPerProducer {
				err := q.Push(wp.Job[int]{Payload: id*pushesPerProducer + j})
				if err != nil {
					errCh <- err
					return
				}
			}
		}(i)
	}

	// Consumers
	for range consumers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < popsPerConsumer; j++ {
				batch, ok := q.BatchPop()
				if ok {
					q.OnBatchDone(batch)
				}
				runtime.Gosched()
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("queue error: %v", err)
	}
}
