package workerpool_test

import(
	"runtime"
	"testing"
	"time"
	"os"
	"strconv"
	"crypto/sha256"
    //"encoding/binary"
	wp "github.com/azargarov/wpool"
)

type workload struct {
    name string
    fn   wp.JobFunc[any]
}

var shaData = []byte("some deterministic payloadsome deterministic payloadsome deterministic payloadsome deterministic payload")

var (

	emptyWork = func(any) error{
		return nil
	}

    cpuWork = func(any) error {
        x := 0
        for i := range 1000 {
            x += i * i
        }
        _ = x
        return nil
    }

    ioWork = func(any) error {
        time.Sleep(5 * time.Microsecond)
        return nil
    }

    shaWork = func(any) error {
        _ = sha256.Sum256(shaData)
        return nil
    }
)

var workloads = []workload{
    {"empty ", emptyWork},
    {"sha256", shaWork},
    {"cpu   ", cpuWork},
    {"io    ", ioWork},
}


func newTestOptions(qt wp.QueueType) wp.Options {
	return wp.Options{
		Workers:      runtime.GOMAXPROCS(0),
		QT:           qt,
		SegmentSize:  1024,
		SegmentCount: 16,
		PoolCapacity: 1024,
	}
}

func newTestPool[T any](t *testing.T, workers int, qt wp.QueueType) *wp.Pool[T, *wp.NoopMetrics] {
	t.Helper()

	opts := newTestOptions(qt)
	opts.Workers = workers

	return wp.NewPoolFromOptions[*wp.NoopMetrics, T](
		&wp.NoopMetrics{},
		opts,
	)
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("condition not satisfied before timeout")
}

func waitUntilB(b *testing.B, timeout time.Duration, cond func() bool) {
	b.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		runtime.Gosched()
	}
	b.Fatal("condition not satisfied before timeout")
}

func percentile(samples []int64, q float64) time.Duration {
	pos := int(float64(len(samples)-1) * q)
	return time.Duration(samples[pos])
}

func getenvInt(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}