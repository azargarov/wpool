package workerpool_test

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	wp "github.com/azargarov/wpool"

	"log"
	"net/http"
	_ "net/http/pprof" // This magically registers /debug/pprof/*
	"os"
)

func startPprof() {
    go func() {
        log.Println("pprof listening on :6060")
        _ = http.ListenAndServe(":6060", nil)
    }()
}


func getPool(opt wp.Options)* wp.Pool[int, *wp.NoopMetrics]{

    m := &wp.NoopMetrics{}
    return wp.NewPool[int](opt, m)
}




func BenchmarkMyPool_MixedReal(b *testing.B) {

    if os.Getenv("PPROF") == "1" {
        startPprof()
    }
    
    runtime.SetBlockProfileRate(1)
    runtime.SetMutexProfileFraction(1) 
    
    workers := runtime.GOMAXPROCS(0) 
    
    var counter int64
    var submitted int64
    
    opts := wp.Options{
        Workers:        workers,
        QT:             wp.SegmentedQueue,
        SegmentSize:    4096,
        SegmentCount:   16256,
        PinWorkers:     false,
    }
    
    pool := getPool(opts)
    defer pool.Shutdown(context.Background())
    
	var sink int64
    
    job := wp.Job[int]{Fn: func(int) error {
        atomic.AddInt64(&counter, 1)
        var count int64
        var f float64
        for i := range int64(10){
            count += i * i
            f += float64(i) / float64(count)
        }
        atomic.AddInt64(&sink, count)
        ////time.Sleep(time.Microsecond * 10)
        return nil
    }}
    
    b.ResetTimer()
    start := time.Now()
    
    //t := time.NewTicker(2 * time.Second)
    //go func() {
    //    defer t.Stop()
    //    for range t.C {
    //        c := atomic.LoadInt64(&counter)
    //        s := atomic.LoadInt64(&submitted)
    //        fmt.Printf("progress: counter=%d / submitted=%d\n", c, s)
    //        fmt.Println(pool.GetIdleLen())
    //    }
    //}()

    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            err := pool.Submit(job, 0)
            if err != nil {
                panic(err)
            }
            atomic.AddInt64(&submitted, 1)
        }
    })
    
    for atomic.LoadInt64(&counter) != int64(b.N) {
        runtime.Gosched()
    }
    //t.Stop()
    elapsed := time.Since(start)
    mps := float64(counter)/elapsed.Seconds()/1e6
    b.ReportMetric(mps, "Mjobs/sec")
    b.Logf("MyPool_MixedReal → %.2f Mjobs/sec in %.2fs", mps, elapsed.Seconds())

}

//PPROF=1 go test ./tests/ -bench BenchmarkMyPool_MixedReal   -run=^$   -benchtime=60s   -count=1^C
//go tool pprof http://127.0.0.1:6060/debug/pprof/profile?seconds=30
//go tool pprof http://127.0.0.1:6060/debug/pprof/mutex
//go tool pprof http://127.0.0.1:6060/debug/pprof/block

//  watch -n 0.2 "grep 'MHz' /proc/cpuinfo | head -n 8"
// sudo cpupower frequency-set -g performance
//sudo cpupower frequency-set -g schedutil


