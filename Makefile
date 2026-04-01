GO              ?= go
PKG             ?= ./...
TEST_PKG        ?= ./...
OUT             ?= ./out

GOFLAGS         ?=
GCFLAGS         ?=
DEBUG_FLAGS     ?= OBSERVER=1

RACE_COUNT      ?= 10
STRESS_COUNT    ?= 100
HEALTH_COUNT    ?= 20
TIMEOUT         ?= 30m

# Primary benchmark names from the redesign suite
BENCH_E2E       ?= BenchmarkPool_EndToEndBounded
BENCH_SCALE     ?= BenchmarkPool_WorkerScale
BENCH_PRESSURE  ?= BenchmarkPool_ProducerPressure
BENCH_BACKP     ?= BenchmarkPool_BoundedBackpressure

# Optional / legacy / micro benches
BENCH_LATENCY   ?= BenchmarkPool_Latency
BENCH_FAIRNESS  ?= BenchmarkPool_Fairness
BENCH_QUEUE     ?= BenchmarkSegmentedQueue_(PushOnly|PopOnly|PushPop)
BENCH_LEGACY    ?= BenchmarkPool_(Single|Throughput)

QUEUE_TESTS     := TestSegmentedQueue_ExactOnce_(MPMC|Bursty)
POOL_TESTS      := TestPool_Health_Stress
UNIT_TESTS      := TestFillDefaults|TestPoolQueues
CORRECTNESS     := $(QUEUE_TESTS)|$(POOL_TESTS)

.PHONY: test-all all help out lint test test-unit test-race \
        test-queue test-pool test-correctness test-correctness-stress test-health \
        bench bench-e2e bench-scale bench-pressure bench-backpressure \
        bench-latency bench-fairness bench-queue bench-legacy \
        bench-fast bench-stress bench-long bench-debug \
        pprof-cpu pprof-mem perf clean

all: test

help:
	@echo "Targets:"
	@echo "  lint                    fmt + vet + golangci-lint"
	@echo "  test                    all tests"
	@echo "  test-all                all test targets"
	@echo "  test-one <test_name>    a specific test"
	@echo "  test-unit               small functional tests"
	@echo "  test-race               all tests with -race"
	@echo "  test-queue              exact-once queue tests"
	@echo "  test-pool               pool stress health test"
	@echo "  test-correctness        queue + pool stress correctness"
	@echo "  test-correctness-stress correctness suite, repeated"
	@echo "  test-health             health suite under multiple GOMAXPROCS"
	@echo "  bench                   primary redesign benchmark set"
	@echo "  bench-e2e               end-to-end bounded benchmark"
	@echo "  bench-scale             worker scaling benchmark"
	@echo "  bench-pressure          producer pressure benchmark"
	@echo "  bench-backpressure      bounded backpressure benchmark"
	@echo "  bench-latency           latency benchmark"
	@echo "  bench-fairness          fairness benchmark"
	@echo "  bench-queue             queue microbenchmarks"
	@echo "  bench-legacy            old pool benchmarks"
	@echo "  bench-fast              quick smoke benchmark run"
	@echo "  bench-stress            more stable benchmark run"
	@echo "  bench-long              publishable-ish benchmark run"
	@echo "  bench-debug             benchmark with cpu/mem profiles"
	@echo "  perf                    perf stat over legacy single benchmark"
	@echo "  pprof-cpu / pprof-mem   open profiles in browser"
	@echo "  clean                   remove generated output"

out:
	@mkdir -p $(OUT)

lint:
	$(GO) fmt $(PKG)
	$(GO) vet $(PKG)
	golangci-lint run $(PKG)

TEST_NAME ?= TestPool_Health_Stress

test-one:
	go test ./... -run '^$(TEST_NAME)$$' -count=1 -v

test-all: test test-race test-unit test-queue test-pool test-correctness test-health

test:
	$(GO) test $(GOFLAGS) $(TEST_PKG) -v

test-unit:
	$(GO) test $(GOFLAGS) $(TEST_PKG) -run '$(UNIT_TESTS)' -count=1 -v

test-race:
	$(GO) test $(GOFLAGS) $(TEST_PKG) -race -count=1 -v

test-queue:
	$(GO) test $(GOFLAGS) $(TEST_PKG) -run '$(QUEUE_TESTS)' -race -count=$(RACE_COUNT) -timeout=$(TIMEOUT) -v

test-pool:
	$(GO) test $(GOFLAGS) $(TEST_PKG) -run '$(POOL_TESTS)' -race -count=$(RACE_COUNT) -timeout=$(TIMEOUT) -v

test-correctness:
	$(GO) test $(GOFLAGS) $(TEST_PKG) -run '$(CORRECTNESS)' -race -count=$(RACE_COUNT) -timeout=$(TIMEOUT) -v

test-correctness-stress:
	$(GO) test $(GOFLAGS) $(TEST_PKG) -run '$(CORRECTNESS)' -race -count=$(STRESS_COUNT) -timeout=$(TIMEOUT) -v

test-health:
	GOMAXPROCS=1  $(GO) test $(GOFLAGS) $(TEST_PKG) -run '^$(POOL_TESTS)$$'  -count=$(HEALTH_COUNT) -timeout=$(TIMEOUT) -v
	GOMAXPROCS=8  $(GO) test $(GOFLAGS) $(TEST_PKG) -run '^$(POOL_TESTS)$$'  -count=$(HEALTH_COUNT) -timeout=$(TIMEOUT) -v
	$(GO) test $(GOFLAGS) $(TEST_PKG) -run '^$(QUEUE_TESTS)$$' -count=50 -timeout=$(TIMEOUT) -v

bench:
	$(GO) test $(GOFLAGS) $(TEST_PKG) -run=^$$ \
		-bench='$(BENCH_E2E)|$(BENCH_SCALE)|$(BENCH_PRESSURE)|$(BENCH_BACKP)' \
		-benchmem -count=1 -v

bench-e2e:
	$(GO) test $(GOFLAGS) $(TEST_PKG) -run=^$$ -bench='$(BENCH_E2E)' -benchmem -count=1 -v

bench-scale:
	$(GO) test $(GOFLAGS) $(TEST_PKG) -run=^$$ -bench='$(BENCH_SCALE)' -benchmem -count=1 -v

bench-pressure:
	$(GO) test $(GOFLAGS) $(TEST_PKG) -run=^$$ -bench='$(BENCH_PRESSURE)' -benchmem -count=1 -v

bench-backpressure:
	$(GO) test $(GOFLAGS) $(TEST_PKG) -run=^$$ -bench='$(BENCH_BACKP)' -benchmem -count=1 -v

bench-latency:
	$(GO) test $(GOFLAGS) $(TEST_PKG) -run=^$$ -bench='$(BENCH_LATENCY)' -benchmem -count=1 -v

bench-fairness:
	$(GO) test $(GOFLAGS) $(TEST_PKG) -run=^$$ -bench='$(BENCH_FAIRNESS)' -benchmem -count=1 -v

bench-queue:
	$(GO) test $(GOFLAGS) $(TEST_PKG) -run=^$$ -bench='$(BENCH_QUEUE)' -benchmem -count=1 -v

bench-legacy:
	$(GO) test $(GOFLAGS) $(TEST_PKG) -run=^$$ -bench='$(BENCH_LEGACY)' -benchmem -count=1 -v

bench-fast:
	$(GO) test $(GOFLAGS) $(TEST_PKG) -run=^$$ \
		-bench='$(BENCH_E2E)|$(BENCH_SCALE)' \
		-benchmem -benchtime=200ms -count=20

bench-stress:
	$(GO) test $(GOFLAGS) $(TEST_PKG) -run=^$$ \
		-bench='$(BENCH_E2E)|$(BENCH_SCALE)|$(BENCH_PRESSURE)' \
		-benchmem -benchtime=3s -count=10

bench-long:
	$(GO) test $(GOFLAGS) $(TEST_PKG) -run=^$$ \
		-bench='$(BENCH_E2E)|$(BENCH_SCALE)|$(BENCH_PRESSURE)|$(BENCH_BACKP)' \
		-benchmem -benchtime=10s -count=5

bench-debug: out
	$(DEBUG_FLAGS) $(GO) test $(GOFLAGS) -tags=debug $(TEST_PKG) -run=^$$ \
		-bench='$(BENCH_E2E)' -benchmem -count=1 -v \
		-cpuprofile $(OUT)/cpu.out \
		-memprofile $(OUT)/mem.out \
		-o $(OUT)/tests.test \
		$(if $(GCFLAGS),-gcflags='$(GCFLAGS)')

pprof-cpu:
	$(GO) tool pprof -http=:8080 $(OUT)/tests.test $(OUT)/cpu.out

pprof-mem:
	$(GO) tool pprof -http=:8080 $(OUT)/tests.test $(OUT)/mem.out

# Keep perf on the legacy single benchmark unless/until you switch your perf workflow.
perf:
	perf stat \
		-e cycles,instructions \
		-e stalled-cycles-frontend \
		-e cache-references,cache-misses \
		-e branches,branch-misses \
		-e stalled-cycles-backend \
		-e cpu/event=0xcd,umask=0x01/ \
		$(GO) test $(GOFLAGS) $(TEST_PKG) -run=^$$ \
		-bench='BenchmarkPool_Single' -benchmem -count=6

clean:
	rm -rf $(OUT)

