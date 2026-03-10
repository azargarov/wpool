PKG          := ./...
TEST_PKG     := 
TEST_OUT     := ./out
BENCH_FULL   := BenchmarkPool_Throughput
BENCH_SINGLE := BenchmarkPool_Single
DEBUG_FLAGS  := OBSERVER=1
GOFLAGS      :=
GCFLAGS      := all=-l=4 -B

.PHONY: lint test bench bench-fast bench-stress bench-long \
        bench-debug pprof-mem pprof-cpu perf clean test-correctness \
		test-correctness-stress test-queue test-pool


lint:
	go fmt $(PKG)
	go vet $(PKG)
	golangci-lint run $(PKG)

test:
	go test $(TEST_PKG) -v


bench:
	go test $(TEST_PKG) -run=^$$ -bench=$(BENCH_FULL) -benchmem -count=1 -v

bench-single:
	go test $(TEST_PKG) -run=^$$ -bench=$(BENCH_SINGLE) -benchmem -count=1 -v

bench-fast:
	go test $(TEST_PKG) -run=^$$ -bench=$(BENCH_SINGLE) -benchmem \
		-benchtime=100ms -count=300

bench-stress:
	go test $(TEST_PKG) -run=^$$ -bench=$(BENCH_SINGLE) -benchmem \
		-benchtime=3s -count=30

bench-long:
	go test $(TEST_PKG) -run=^$$ -bench=$(BENCH_SINGLE) -benchmem \
		-benchtime=10s -count=5

bench-debug:
	$(DEBUG_FLAGS) go test -tags=debug $(TEST_PKG) -run=^$$ -bench=$(BENCH_SINGLE) -benchmem \
		-count=1 -cpuprofile $(TEST_OUT)/cpu.out -memprofile $(TEST_OUT)/mem.out -o $(TEST_OUT)/tests.test -v

pprof-mem:
	go tool pprof -http=:8080 $(TEST_OUT)/mem.out

pprof-cpu:
	go tool pprof -http=:8080 $(TEST_OUT)/cpu.out

test-queue:
	go test -run 'TestSegmentedQueue_ExactOnce_MPMC|TestSegmentedQueue_ExactOnce_Bursty' -race -count=10 -v

test-pool:
	go test -run 'TestPool_ExactOnce_SmallSegments' -race -count=10 -v

test-correctness:
	go test -run 'TestSegmentedQueue_ExactOnce_MPMC|TestSegmentedQueue_ExactOnce_Bursty|TestPool_ExactOnce_SmallSegments' -race -count=10 -v

test-correctness-stress:
	go test -run 'TestSegmentedQueue_ExactOnce_MPMC|TestSegmentedQueue_ExactOnce_Bursty|TestPool_ExactOnce_SmallSegments' -race -count=100 -v -timeout=30m	

perf:
	perf stat \
		-e cycles,instructions \
		-e stalled-cycles-frontend \
		-e cache-references,cache-misses \
		-e branches,branch-misses \
		-e cpu/event=0xcd,umask=0x01/ \
		go test $(TEST_PKG) -run=^$$ -bench=$(BENCH_SINGLE) -benchmem -count=4

clean:
	rm -f $(TEST_OUT)/*
