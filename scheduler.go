package workerpool

import (
	"container/heap"
	"time"
)

// item is an element stored in the priority queue.
// eff (effective priority) = basePrio + aging*age.
type item[T any] struct {
	job Job[T]

	// user-provided priority on submit
	basePrio float64

	// when the job entered the scheduler
	queuedAt time.Time

	// effective priority used by the heap
	eff float64

	// heap index (required by container/heap)
	index int
}

// submitReq is what the pool feeds into the scheduler.
// We separate it from item so we can attach timestamps inside the scheduler.
type submitReq[T any] struct {
	job      Job[T]
	basePrio float64
	//respCh    chan error // TODO: future blocking submit
}

// scheduler is a dedicated goroutine that:
//   - keeps jobs in a max-heap ordered by effective priority
//   - periodically re-ages jobs so old ones bubble up
//   - dispatches ready jobs to workers
//   - drains the queue on shutdown
func (p *Pool[T]) scheduler() {
	var pq priorityQueue[T]
	heap.Init(&pq)

	ticker := time.NewTicker(p.opts.RebuildDur)
	defer ticker.Stop()

loop:
	for {
		select {
		case <-p.stopCh:
			// pool is shutting down: flush everything we still have
			for pq.Len() > 0 {
				it := heap.Pop(&pq).(*item[T])
				p.dispatch(it.job)
			}
			// no more work will be sent — close worker channel
			close(p.workCh)
			break loop

		case req := <-p.submitCh:
			// new job arrived — wrap it and push into heap
			now := time.Now()
			it := &item[T]{
				job:      req.job,
				basePrio: req.basePrio,
				queuedAt: now,
			}
			it.eff = p.effective(it, now)
			heap.Push(&pq, it)

			p.incSubmitted()
			p.setQueued(pq.Len())

		case <-ticker.C:
			// periodic aging: recompute effective priority for all items
			now := time.Now()
			var maxAge time.Duration
			for _, it := range pq {
				age := now.Sub(it.queuedAt)
				if age > maxAge {
					maxAge = age
				}
				it.eff = p.effective(it, now)
			}
			p.setMaxAge(maxAge)

			// heap order might have changed — rebuild
			heap.Init(&pq)
		default:
			// no control signals — try to dispatch the best job if we have any
			if pq.Len() > 0 {
				it := heap.Pop(&pq).(*item[T])
				p.setQueued(pq.Len())
				p.dispatch(it.job)
			} else {
				// tiny sleep to avoid busy loop when there is no work
				time.Sleep(5 * time.Millisecond)
			}
		}
	}
	// tell Shutdown that scheduler is completely done
	close(p.doneCh)
}

// dispatch sends a job to workers. If all workers are busy, we block until one is free.
func (p *Pool[T]) dispatch(job Job[T]) {
	select {
	case p.workCh <- job:
	default:
		// if all workes are buissy - wait
		p.workCh <- job
	}
}

// effective computes the current effective priority of a job
// based on its base priority and how long it has been waiting.
func (p *Pool[T]) effective(it *item[T], now time.Time) float64 {
	age := now.Sub(it.queuedAt).Seconds()
	return it.basePrio + p.opts.AgingRate*age
}
