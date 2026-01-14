package workerpool

import (
	"runtime"
)

func (p *Pool[T, M]) runBatch(jobs []Job[T]) {
	for _, j := range jobs {
		p.runJob(j)
	}
}

func (p *Pool[T, M]) runJob(j Job[T]) {
	defer func() {
		if r := recover(); r != nil {
			//  TODO:  panic handler
		}
		if j.CleanupFunc != nil {
			j.CleanupFunc()
		}
		p.metrics.IncExecuted()
	}()
	// TODO: do not drop error
	_ = j.Fn(j.Payload)
	//p.metrics.IncExecuted()
}

// TODO: REMOVE IT
func (p *Pool[T, M]) batchProcessJob_old() int64 {
	var jobs []Job[T]
	var ok bool
	var counter int64
	for {
		jobs, ok = p.queue.BatchPop()
		if ok {
			p.runBatch(jobs)
			counter += int64(len(jobs))
			continue
		}
		return counter
	}
}

func (p *Pool[T, M]) batchProcessJob() int64 {
	var counter int64
	emptySpins := 0

	for {
		jobs, ok := p.queue.BatchPop()
		if ok {
			//emptySpins = 0
			p.runBatch(jobs)
			counter += int64(len(jobs))
			continue
		}

		if p.pendingJobs.Load() > 0 && emptySpins < 50 {
			emptySpins++
			runtime.Gosched()
			continue
		}
		return counter
	}
}
