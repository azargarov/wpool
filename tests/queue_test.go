package workerpool_test

import(
	"time"
	"testing"
	wp "github.com/azargarov/wpool"

)

func BenchmarkBucketQueue_PushOnly(b *testing.B) {

    q := wp.NewSegmentedQ[int](64, 64)
    baseJob := wp.Job[int]{Fn: func(int) error { return nil }}
    now := time.Now()

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        q.Push(baseJob, 0, now)
    }
}

func BenchmarkBucketQueue_PopOnly(b *testing.B) {
    q := wp.NewSegmentedQ[int](64, 64)
    job := wp.Job[int]{Fn: func(int) error { return nil }}
    now := time.Now()

    const prefill = 4096 
    for range prefill {
        q.Push(job, 0, now)
    }

    b.ReportAllocs()
    

    for b.Loop() {
        _, ok := q.Pop(now)
        if !ok {
            b.Fatal("queue unexpectedly empty")
        }
        q.Push(job, 0, now)
    }
}

func BenchmarkBucketQueue_PushPop(b *testing.B) {
    q := wp.NewSegmentedQ[int](64, 64)
    job := wp.Job[int]{Fn: func(int) error { return nil }}
    now := time.Now()

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        q.Push(job, 0, now)
        _, _ = q.Pop(now)
    }
}
