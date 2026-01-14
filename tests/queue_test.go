package workerpool_test

import(
	"testing"
	wp "github.com/azargarov/wpool"

)

func BenchmarkBucketQueue_PushOnly(b *testing.B) {

    q := wp.NewSegmentedQ[int](64, 64)
    baseJob := wp.Job[int]{Fn: func(int) error { return nil }}

    b.ReportAllocs()

    for b.Loop() {
        q.Push(baseJob)
    }
}

func BenchmarkBucketQueue_PopOnly(b *testing.B) {
    q := wp.NewSegmentedQ[int](64, 64)
    job := wp.Job[int]{Fn: func(int) error { return nil }}

    const prefill = 4096 
    for range prefill {
        q.Push(job)
    }

    b.ReportAllocs()

    for b.Loop() {
        _, ok := q.BatchPop()
        if !ok {
            b.Fatal("queue unexpectedly empty")
        }
        q.Push(job)
    }
}

func BenchmarkBucketQueue_PushPop(b *testing.B) {
    q := wp.NewSegmentedQ[int](4096, 16256)
    job := wp.Job[int]{Fn: func(int) error { return nil }}

    b.ReportAllocs()

    for b.Loop() {
        q.Push(job)
        _, _ = q.BatchPop()
    }
}
