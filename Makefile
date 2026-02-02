PKG          := ./...
TEST_PKG     := ./tests
TEST_OUT     := ./tests/out
BENCH_FULL   := BenchmarkPool$$
BENCH_SINGLE := BenchmarkPool_single
GOFLAGS      :=
GCFLAGS      := all=-l=4 -B

.PHONY: lint test bench bench-fast bench-stress bench-long \
        bench-debug pprof-mem pprof-cpu perf clean


lint:
	go fmt $(PKG)
	go vet $(PKG)
	golangci-lint run $(PKG)

test:
	go test $(TEST_PKG) -v


bench:
	go test $(TEST_PKG) -run=^$$ -bench=$(BENCH_FULL) -benchmem -count=1 -v

bench-noopt:
	go test $(TEST_PKG) -run=^$$ -bench=$(BENCH_FULL) -benchmem \
		-gcflags="$(GCFLAGS)" -count=1

bench-fast:
	go test $(TEST_PKG) -run=^$$ -bench=$(BENCH_SINGLE) -benchmem \
		-benchtime=30ms -count=30

bench-stress:
	go test $(TEST_PKG) -run=^$$ -bench=$(BENCH_SINGLE) -benchmem \
		-benchtime=3s -count=30

bench-long:
	go test $(TEST_PKG) -run=^$$ -bench=$(BENCH_SINGLE) -benchmem \
		-benchtime=10s -count=5

bench-debug:
	go test -tags=debug $(TEST_PKG) -run=^$$ -bench=$(BENCH_FULL) -benchmem \
		-count=1 -cpuprofile $(TEST_OUT)/cpu.out -memprofile $(TEST_OUT)/mem.out -o $(TEST_OUT)/tests.test

pprof-mem:
	go tool pprof -http=:8080 $(TEST_OUT)/mem.out

pprof-cpu:
	go tool pprof -http=:8080 $(TEST_OUT)/cpu.out

perf:
	perf stat \
		-e cycles,instructions \
		-e stalled-cycles-frontend \
		-e cache-references,cache-misses \
		-e branches,branch-misses \
		-e cpu/event=0xcd,umask=0x01/ \
		go test $(TEST_PKG) -run=^$$ -bench=$(BENCH_FULL) -benchmem -count=4

clean:
	rm -f $(TEST_OUT)/*
