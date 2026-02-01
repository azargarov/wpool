package workerpool

import (
	"context"
	"errors"
    "fmt"
)

var ErrPoolPanic   = errors.New("workerpool: job panic. ")

func (p *Pool[T, M]) runBatch(jobs []Job[T]) {
	for _, j := range jobs {
		p.runJob(j)
	}
}

func (p *Pool[T, M]) runJob(j Job[T]) (err error) {

    if j.Ctx == nil {
		j.Ctx = context.Background()
	}

    if j.CleanupFunc != nil{
	    defer func() {
			defer func() { _ = recover() }()
            j.CleanupFunc()
            }()
    }

    defer p.metrics.IncExecuted()

	select {
	case <-j.Ctx.Done():
		return j.Ctx.Err()
	default:
	}

	if j.Fn == nil {
		return ErrNilFunc
	}

	defer func() {
		if r := recover(); r != nil {
			err = errors.Join(ErrPoolPanic, fmt.Errorf("panic: %v",r))
		}
	}()

    return j.Fn(j.Payload)
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

