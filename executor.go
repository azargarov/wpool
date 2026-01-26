package workerpool

import (
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
}

func (p *Pool[T, M]) batchProcessJob() int64 {
    var counter int64

    for {
        batch, ok := p.queue.BatchPop()
        if ok {
            p.runBatch(batch.Jobs)
            counter += int64(len(batch.Jobs))
            p.queue.OnBatchDone(batch)
            continue
        }
        return counter
    }
}

