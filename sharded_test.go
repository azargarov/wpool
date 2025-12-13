//package workerpool
//
//import (
//	"testing"
//	"time"
//	"context"
//)
//
//
//
//func BenchmarkShardedBucketQueue_(b *testing.B) {
//	q := newShardedBucketQueue[int](32, 3, 128)
//
//	now := time.Now()
//	job := Job[int]{
//		Ctx:     context.Background(),
//		Payload: 1,
//		Fn: func(_ int) error {
//			return nil
//		},
//	}
//
//	b.ResetTimer()
//	start := time.Now()
//
//	b.RunParallel(func(pb *testing.PB) {
//		for pb.Next() {
//			q.Push(job, 10, now)
//			q.Pop(now)
//		}
//	})
//
//	elapsed := time.Since(start)
//	ops := float64(b.N) / elapsed.Seconds()
//	mjobs := ops / 1_000_000
//
//	// print only ONCE here
//	b.Logf("ShardedBucketQueue → %.2f Mjobs/sec (%.2fs)", mjobs, elapsed.Seconds())
//}
//
//func benchMjobs(b *testing.B, name string, start time.Time) {
//	elapsed := time.Since(start)
//	ops := float64(b.N) / elapsed.Seconds()
//	mjobs := ops / 1_000_000
//	b.Logf("%s → %.2f Mjobs/sec (%.2fs)", name, mjobs, elapsed.Seconds())
//}
//
//// Dummy job
//var benchJob = Job[int]{
//	Ctx:     context.Background(),
//	Payload: 1,
//	Fn: func(_ int) error { return nil },
//}
//
//// -----------------------
//// Push Only
//// -----------------------
//
//func BenchmarkShardedBucketQueue_PushOnly(b *testing.B) {
//	q := newShardedBucketQueue[int](32, 1, initialBucketSize)
//
//	now := time.Now()
//
//	b.ResetTimer()
//	start := time.Now()
//
//	b.RunParallel(func(pb *testing.PB) {
//		for pb.Next() {
//			q.Push(benchJob, 0, now)
//		}
//	})
//
//	benchMjobs(b, "ShardedBucketQueue_PushOnly", start)
//}
//
//// -----------------------
//// Pop Only
//// -----------------------
//
//func BenchmarkShardedBucketQueue_PopOnly(b *testing.B) {
//	q := newShardedBucketQueue[int](16, 3, initialBucketSize)
//
//	now := time.Now()
//
//	// Pre-fill queue with N jobs so Pop has something to do
//	for i := 0; i < b.N; i++ {
//		q.Push(benchJob, 0, now)
//	}
//
//	b.ResetTimer()
//	start := time.Now()
//
//	b.RunParallel(func(pb *testing.PB) {
//		for pb.Next() {
//			q.Pop(now)
//		}
//	})
//
//	benchMjobs(b, "ShardedBucketQueue_PopOnly", start)
//}
//
//// -----------------------
//// Mixed: Push + Pop
//// -----------------------
//
//func BenchmarkShardedBucketQueue_Mixed(b *testing.B) {
//	q := newShardedBucketQueue[int](16, 3, 128)
//
//	now := time.Now()
//
//	b.ResetTimer()
//	start := time.Now()
//
//	b.RunParallel(func(pb *testing.PB) {
//		for pb.Next() {
//			q.Push(benchJob, 10, now)
//			q.Pop(now)
//		}
//	})
//
//	benchMjobs(b, "ShardedBucketQueue_Mixed", start)
//}
//
//
//
//
package workerpool