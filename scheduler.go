package workerpool

import (
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

// schedQueue defines the common behavior of all internal scheduler queues.
//
// A queue is responsible for storing pending jobs and determining
// which one should be dispatched next. Different implementations
// (such as FIFO or priority-based) define their own ordering logic.
//
// The scheduler goroutine interacts only through this interface,
// making it easy to plug in alternative queueing strategies.
type schedQueue[T any] interface {

	// Push inserts a newly submitted job into the queue.
	//
	// basePrio is the user-provided priority value, and now is the
	// enqueue timestamp. FIFO implementations can ignore both.
	Push(job Job[T], basePrio float64, now time.Time)

	// Pop retrieves and removes the next job to dispatch.
	//
	// It returns the selected job and a boolean flag indicating
	// whether a job was available. If false, the queue is empty.
	Pop(now time.Time) (Job[T], bool)

	// Tick updates internal state periodically.
	//
	// Priority-based queues use it to apply *aging* (increasing
	// effective priority with time), while FIFO queues typically
	// implement it as a no-op.
	Tick(now time.Time)

	// Len returns the current number of jobs waiting in the queue.
	//
	// The scheduler uses this to update runtime metrics.
	Len() int

	// MaxAge reports the maximum waiting time among queued jobs.
	//
	// This metric helps track fairness and queue health. For FIFO
	// queues or strategies that do not track age, it can safely
	// return zero.
	MaxAge() time.Duration
}

func (p *Pool[T]) makeQueue() schedQueue[T] {
	switch p.opts.QT {
	case Fifo:
		return &fifoQueue[T]{}
	case Priority:
		return newPrioQueue[T](p.opts.AgingRate)
	case Conditional:
		// for now fall back to FIFO or implement later
		return &fifoQueue[T]{}
	default:
		return &fifoQueue[T]{}
	}
}

// scheduler is a dedicated goroutine that:
//   - keeps jobs in a max-heap ordered by effective priority
//   - periodically re-ages jobs so old ones bubble up
//   - dispatches ready jobs to workers
//   - drains the queue on shutdown
func (p *Pool[T]) scheduler() {
	q := p.makeQueue()

	ticker := time.NewTicker(p.opts.RebuildDur)
	defer ticker.Stop()

loop:
	for {
		// first, try to dispatch if we have something
		if job, ok := q.Pop(time.Now()); ok {
			p.setQueued(q.Len())
			p.dispatch(job)
			continue
		}

		select {
		case <-p.stopCh:
			// drain queue
			for {
				job, ok := q.Pop(time.Now())
				if !ok {
					break
				}
				p.dispatch(job)
			}
			close(p.workCh)
			break loop

		case req := <-p.submitCh:
			now := time.Now()
			q.Push(req.job, req.basePrio, now)
			p.incSubmitted()
			p.setQueued(q.Len())
			if age := q.MaxAge(); age > 0 {
				p.setMaxAge(age)
			}
		case <-ticker.C:
			now := time.Now()
			q.Tick(now)
			p.setQueued(q.Len())
			if age := q.MaxAge(); age > 0 {
				p.setMaxAge(age)
			}
		}
	}

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
