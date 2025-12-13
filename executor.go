package workerpool
//go:inline
import (
    "runtime"
)
//func (p *Pool[T]) processJob_(){
//
//	for {
//		jobs, ok := p.queue.BatchPop()
//		if !ok {break}
//		for _, j := range jobs {
//		    func(j Job[T]) {
//		        defer func() {
//		            if r := recover(); r != nil {
//		            }
//		            if j.CleanupFunc != nil {
//		                j.CleanupFunc()
//		            }
//		            p.metrics.executed.Add(1)
//		        }()
//		        _ = j.Fn(j.Payload)
//		    }(j)
//		}
//	}
//}
//
//func (p *Pool[T]) processJob() {
//    const spinCount = 64
//
//    for {
//        // Try immediate batch pop
//        jobs, ok := p.queue.BatchPop()
//        if ok {
//            p.runBatch(jobs)
//            continue // stay in hot loop
//        }
//
//        // === Spin before parking ===
//        for i := 0; i < spinCount; i++ {
//            jobs, ok = p.queue.BatchPop()
//            if ok {
//                p.runBatch(jobs)
//                goto continueLoop // restart active loop
//            }
//            runtime.Gosched()
//        }
//
//        // No work found → return to worker → block on p.sem
//        return
//
//    continueLoop:
//        continue
//    }
//}

func (p *Pool[T]) runBatch(jobs []Job[T]) {
    for _, j := range jobs {
        func(j Job[T]) {
            defer func() {
                if r := recover(); r != nil {
                    // optional panic handler
                }
                if j.CleanupFunc != nil {
                    j.CleanupFunc()
                }
                p.metrics.executed.Add(1)
            }()
            _ = j.Fn(j.Payload)
        }(j)
    }
}


func (p *Pool[T]) batchProcessJob() {
    var jobs []Job[T]
    var ok bool
    for {
        jobs, ok = p.queue.BatchPop()
        p.batchDecQueued(int64(len(jobs)))
        if ok {
            p.runBatch(jobs)
            continue
        }
        return
    }
}

func (p *Pool[T]) processJob____() {
    const spinCount = 64

    for {
        jobs, ok := p.queue.BatchPop()
        p.batchDecQueued(int64(len(jobs)))
        if ok {
            p.runBatch(jobs)
            continue
        }

        for i := 0; i < spinCount; i++ {
            jobs, ok = p.queue.BatchPop()
            if ok {
                p.batchDecQueued(int64(len(jobs)))
                p.runBatch(jobs)
                goto continueLoop
            }
            runtime.Gosched()
        }

        return // no work found even after spin

    continueLoop:
        continue
    }
}


func (p *Pool[T]) processJob_() {
    const spinCount = 64

    for {
        // === Fast path ===
        if jobs, ok := p.queue.BatchPop(); ok {
            p.batchDecQueued(int64(len(jobs)))
            p.runBatch(jobs)
            continue
        }

        // === Spin a little (Go auto-emits PAUSE on 1.21+) ===
        for range spinCount {
            if jobs, ok := p.queue.BatchPop(); ok {
                p.batchDecQueued(int64(len(jobs)))
                p.runBatch(jobs)
                goto nextIteration
            }
        }


        runtime.Gosched()

    nextIteration:
    }
}