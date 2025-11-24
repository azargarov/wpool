package workerpool

import (
	boff "github.com/azargarov/go-utils/backoff"
	lg "github.com/azargarov/go-utils/zlog"
	"time"
)

func (p *Pool[T]) processJob(job Job[T]) {
	var logger lg.ZLogger
	if job.Ctx != nil {
		logger = lg.FromContext(job.Ctx).With(lg.Any("job", job.Payload))
	} else {
		logger = lg.NewDiscard()
	}
	// TODO: implement logging
	//logger.Info("Worker processing job", lg.Int32("active_workers", p.activeWorkers.Load()))

	pol := p.defaultRetry
	if job.Retry != nil {
		// override non-zero per-job values
		if job.Retry.Attempts > 0 {
			pol.Attempts = job.Retry.Attempts
		}
		if job.Retry.Initial > 0 {
			pol.Initial = job.Retry.Initial
		}
		if job.Retry.Max > 0 {
			pol.Max = job.Retry.Max
		}
	}

	// skip backoff if Attempts = 1
	if pol.Attempts <= 1 {
		job.Fn(job.Payload)
		return
	}

	bo := boff.New(pol.Initial, pol.Max, time.Now().UnixNano())

	for attempt := 1; attempt <= pol.Attempts; attempt++ {
		if err := job.Fn(job.Payload); err == nil {
			//logger.Info("Worker finished", lg.Int32("active_workers", p.activeWorkers.Load()))
			return
		} else if attempt == pol.Attempts {
			logger.Error("Worker error", lg.Int("attempt", attempt), lg.Any("error", err))
			return
		} else {
			delay := bo.Next()
			logger.Warn("job attempt failed; backing off",
				lg.Int("attempt", attempt),
				lg.String("sleep", delay.String()),
				lg.Any("error", err),
			)
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-job.Ctx.Done():
				if !timer.Stop() {
					<-timer.C // drain if timer is fired
				}
				logger.Info("Job canceled", lg.Any("reason", job.Ctx.Err()))
				return
			}
		}
	}
}
