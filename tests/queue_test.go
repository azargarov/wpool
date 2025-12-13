package workerpool_test

import(
	"time"
	"testing"
	wp "github.com/azargarov/wpool"

)

func BenchmarkBucketQueue_PushOnly(b *testing.B) {

    q := wp.NewBucketQueue[int](3, 100)
    baseJob := wp.Job[int]{Fn: func(int) error { return nil }}
    now := time.Now()

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        q.Push(baseJob, 0, now)
    }
}

func BenchmarkFifoQueue_PushOnly(b *testing.B) {

    q := wp.NewFifoQueue[int](3000)
    baseJob := wp.Job[int]{Fn: func(int) error { return nil }}
    now := time.Now()

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        q.Push(baseJob, 0, now)
    }
}

func BenchmarkBucketQueue_PopOnly(b *testing.B) {
    q := wp.NewBucketQueue[int](0, 100)
    job := wp.Job[int]{Fn: func(int) error { return nil }}
    now := time.Now()

    const prefill = 4096 
    for range prefill {
        q.Push(job, 0, now)
    }

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        _, ok := q.Pop(now)
        if !ok {
            b.Fatal("queue unexpectedly empty")
        }
        q.Push(job, 0, now)
    }
}

func BenchmarkBucketQueue_PushPop(b *testing.B) {
    q := wp.NewBucketQueue[int](3, 100)
    job := wp.Job[int]{Fn: func(int) error { return nil }}
    now := time.Now()

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        q.Push(job, 0, now)
        _, _ = q.Pop(now)
    }
}
