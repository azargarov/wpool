package workerpool_test

import(
	wp "github.com/azargarov/wpool"
	"testing"

)

func BenchmarkSegmentPool_GetPut_(b *testing.B) {

	pool := wp.NewSegmentPool[int](4096, 64, 128, 1024, 1024)
	defer pool.Close()

	b.Run("Sequential", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			seg := pool.Get()
			pool.Put(seg)
		}
	})

	b.Run("Parallel_GOMAXPROCS", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				seg := pool.Get()
				pool.Put(seg)
			}
		})
	})

	b.Run("HighContention_1000", func(b *testing.B) {
		b.ReportAllocs()
		b.SetParallelism(1000)
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				seg := pool.Get()
				pool.Put(seg)
			}
		})
	})
}
