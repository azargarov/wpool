package workerpool_test

import(
	wp "github.com/azargarov/wpool"
	"testing"
	"sync"

)

func BenchmarkSegmentPool_GetPut(b *testing.B) {
    pool := wp.NewSegmentPool[int](4096, 64, 128, 1024, 1024)
    defer pool.Close()
    
    b.Run("Sequential", func(b *testing.B) {
        for range(b.N){
            seg := pool.Get()
            pool.Put(seg)
        }
    })
    
    b.Run("Parallel_GOMAXPROCS", func(b *testing.B) {
        b.RunParallel(func(pb *testing.PB) {
            for pb.Next() {
                seg := pool.Get()
                pool.Put(seg)
            }
        })
    })
    
    b.Run("Contention_1000_goroutines", func(b *testing.B) {
        var wg sync.WaitGroup
        for range 1000 {
            wg.Add(1)
            go func() {
                defer wg.Done()
                for j := 0; j < b.N/1000; j++ {
                    seg := pool.Get()
                    pool.Put(seg)
                }
            }()
        }
        wg.Wait()
    })
}