package workerpool

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	//"os"
)

const (
	// defaultPushBatch is the minimum number of pending jobs
	// required before a worker wake-up is triggered eagerly.
	// Smaller values reduce latency, larger values improve batching.
	defaultWakeMinJobs = 128

	// batchTimerInterval is the periodic interval used by the batch timer
	// to ensure progress even if no new submissions arrive.
	defaultFlushInterval = 50 * time.Microsecond

	missRateToThrottle = 0.6
)

var (
	// ErrClosed is returned when submitting a job to a pool
	// that has already been shut down.
	ErrClosed = errors.New("workerpool: pool is closed")
)

// ErrorHandler is a user-provided callback invoked on internal
// or job-level errors.
type ErrorHandler func(e error)

// WakeupWorker is a lightweight signal channel used to wake
// an idle worker.
type WakeupWorker chan struct{}

type workerStat struct {
	casMiss atomic.Uint64
	_       cachePad

	casSucc atomic.Uint64
	_       cachePad
}

type poolMeta[M MetricsPolicy] struct {
	metrics         M
	opts            Options
	OnInternalError ErrorHandler
	OnJobError      ErrorHandler
	metricsMu       sync.Mutex
	wStats          []workerStat
}

type Pool[T any, M MetricsPolicy] struct {
	pendingJobs atomic.Int64
	shutdown    atomic.Bool

	queue         schedQueue[T]
	idleWorkers   chan WakeupWorker
	wakes         []WakeupWorker
	workersActive []atomic.Bool

	doneCh      chan struct{}
	workersDone chan struct{}
	stopOnce    sync.Once
	wgWorkers   sync.WaitGroup
	ticker      *time.Ticker

	meta poolMeta[M]
}

func (w *workerStat) Reset() {
	w.casMiss.Store(0)
	w.casSucc.Store(0)
}

func (w *workerStat) GetRate() float32 {
	total := float32(w.casMiss.Load()) + float32(w.casSucc.Load())
	if total == 0 {
		return 0
	}
	return float32(w.casMiss.Load()) / total
}

func (p *Pool[T, M]) StatSnapshot() string {
	return p.queue.StatSnapshot()
}

func (p *Pool[T, M]) GetIdleLen() int64 {
	return int64(len(p.idleWorkers))
}

func NewPool[M MetricsPolicy, T any](metrics M, opts ...Option) *Pool[T, M] {
	o := Options{}
	for _, opt := range opts {
		opt(&o)
	}
	o.FillDefaults()

	return NewPoolFromOptions[M, T](metrics, o)
}

func NewPoolFromOptions[M MetricsPolicy, T any](metrics M, opts Options) *Pool[T, M] {
	opts.FillDefaults()

	p := &Pool[T, M]{
		doneCh:        make(chan struct{}),
		idleWorkers:   make(chan WakeupWorker, opts.Workers),
		workersActive: make([]atomic.Bool, opts.Workers),
	}
	p.meta.opts = opts

	switch opts.QT {
	case SegmentedQueue:
		p.queue = NewSegmentedQ[T](opts, nil)
		//case RevolvingBucketQueue:
		//	p.queue = NewRevolvingBucketQ[T](opts)
	}
	p.meta.metrics = metrics
	p.wakes = make([]WakeupWorker, opts.Workers)
	for i := 0; i < opts.Workers; i++ {
		p.wakes[i] = make(WakeupWorker, 1)
	}

	// Start workers.
	p.meta.wStats = make([]workerStat, opts.Workers)
	for i := 0; i < opts.Workers; i++ {
		p.wgWorkers.Add(1)
		p.setWorkerState(i, true)
		p.meta.wStats[i] = workerStat{}
		go func(id int) {
			p.batchWorker(id, &p.wgWorkers, &p.meta.wStats[i])
		}(i)
	}

	// Track worker completion.
	p.workersDone = make(chan struct{})
	go func() {
		p.wgWorkers.Wait()
		close(p.workersDone)
	}()

	p.ticker = time.NewTicker(p.meta.opts.FlushInterval)
	go p.batchTimer(p.ticker)

	return p
}

func (p *Pool[T, M]) Shutdown(ctx context.Context) error {
	p.stopOnce.Do(func() {
		p.shutdown.Store(true)
		close(p.doneCh)
		p.queue.Close()
	})

	select {
	case <-p.workersDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Pool[T, M]) Stop() { _ = p.Shutdown(context.Background()) }

func (p *Pool[T, M]) Submit(job Job[T]) error {
	if p.shutdown.Load() {
		return ErrClosed
	}

	if job.Fn == nil {
		return ErrNilFunc
	}

	meta := job.Meta
	if meta != nil && meta.Ctx != nil {
		select {
		case <-meta.Ctx.Done():
			return meta.Ctx.Err()
		default:
		}
	}

	if err := p.queue.Push(job); err != nil {
		return err
	}

	p.meta.metrics.IncQueued()
	pj := p.pendingJobs.Add(1)

	if pj >= p.meta.opts.WakeMinJobs {
		if p.needNewWorker() {
			p.tryWakeOne()
		}
	}
	return nil
}

func (p *Pool[T, M]) batchWorker(id int, wg *sync.WaitGroup, stat *workerStat) {
	batch := Batch[T]{}
	batch.Jobs = make([]Job[T], 0, p.meta.opts.SegmentSize)
	defer func() {
		p.setWorkerState(id, false)
		wg.Done()
	}()

	if p.meta.opts.PinWorkers {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := PinToCPU(id % runtime.NumCPU()); err != nil {
			p.reportInternalError(err)
		}
	}

	wake := p.wakes[id]

	select {
	case p.idleWorkers <- wake:
	case <-p.doneCh:
		p.ticker.Stop()
		return
	}

	for {
		stat.Reset() // reset all stats before go to idle. for idling worker stats must be 0

		select {
		case <-wake:
		case <-p.doneCh:
			p.ticker.Stop()
			return
		}

		for {
			if p.shutdown.Load() {
				return
			}

			jobsNum := p.batchProcessJob(&batch)
			if jobsNum > 0 {
				p.meta.metrics.BatchDecQueued(jobsNum)
				newPending := p.pendingJobs.Add(-int64(jobsNum))
				if newPending < 0 {
					p.pendingJobs.Store(0)
				}
				stat.casSucc.Add(1)
			} else {
				stat.casMiss.Add(1)
			}

			if p.shutdown.Load() {
				return
			}

			if p.pendingJobs.Load() == 0 {
				break
			}
		}

		select {
		case p.idleWorkers <- wake:
		case <-p.doneCh:
			p.ticker.Stop()
			return
		}
	}
}

func (p *Pool[T, M]) batchTimer(t *time.Ticker) {
	defer t.Stop()

	for {
		select {
		case <-t.C:
			if p.shutdown.Load() || p.pendingJobs.Load() == 0 || len(p.idleWorkers) == 0 {
				continue
			}
			if p.needNewWorker() {
				p.tryWakeOne()
			}
		case <-p.doneCh:
			t.Stop()
			return
		}
	}
}

func (p *Pool[T, M]) tryWakeOne() bool {
	select {
	case w := <-p.idleWorkers:
		select {
		case w <- struct{}{}:
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func (p *Pool[T, M]) needNewWorker() bool {

	totalMiss := float32(0)
	totalSucc := float32(0)
	for i := range p.meta.wStats {
		totalMiss += float32(p.meta.wStats[i].casMiss.Load())
		totalSucc += float32(p.meta.wStats[i].casSucc.Load())
	}
	total := totalMiss + totalSucc
	if total == 0 {
		return true
	}
	missRate := totalMiss / total
	return missRate < missRateToThrottle
}

func (p *Pool[T, M]) Metrics() *M {
	p.meta.metricsMu.Lock()
	defer p.meta.metricsMu.Unlock()
	return &p.meta.metrics
}

func (p *Pool[T, M]) MetricsStr() string {
	return p.meta.metrics.String() + fmt.Sprintf(", Penidng jobs: %d", p.pendingJobs.Load()) +
		fmt.Sprintf(", idle workers : %d", p.GetIdleLen()) + fmt.Sprintf(", active workers: %d", p.ActiveWorkers())
}

func (p *Pool[T, M]) DebugHead() string {
	return p.queue.DebugHead()
}

func (p *Pool[T, M]) ActiveWorkers() int {
	count := 0
	for i := range p.workersActive {
		if p.workersActive[i].Load() {
			count++
		}
	}
	return count
}

func (p *Pool[T, M]) setWorkerState(id int, state bool) {
	p.workersActive[id].Store(state)
}
