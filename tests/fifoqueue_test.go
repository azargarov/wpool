package workerpool_test

import (
	"testing"
	"time"
	wp "github.com/azargarov/wpool"
)

func TestFifoGrow_NoWrap(t *testing.T) {
	capacity := 4
	newSize := 5
	q := wp.NewFifoQueue[int](capacity)

	for i := 1; i <= capacity; i++ {
		q.Push(wp.Job[int]{Payload: i}, 0, time.Now())
	}

	if q.Size() != capacity {
		t.Fatalf("expected size=4, got %d", q.Size())
	}

	q.Push(wp.Job[int]{Payload: 5}, 0, time.Now())

	if q.Capacity() <= capacity {
		t.Fatalf("grow() didn't increase capacity, got %d", q.Capacity())
	}

	if q.Size() != newSize {
		t.Fatalf("after grow: expected size=%d, got %d", newSize, q.Size())
	}

	for expected := 1; expected <= newSize; expected++ {
		j, ok := q.Pop(time.Now())
		if !ok {
			t.Fatalf("Pop returned false, expected %d", expected)
		}
		if j.Payload != expected {
			t.Fatalf("FIFO order broken: expected %d, got %d", expected, j.Payload)
		}
	}
}

func TestFifoGrow_WithWrap(t *testing.T) {
	capacity := 4
	q := wp.NewFifoQueue[int](capacity)

	q.Push(wp.Job[int]{Payload: 1}, 0, time.Now())
	q.Push(wp.Job[int]{Payload: 2}, 0, time.Now())
	q.Push(wp.Job[int]{Payload: 3}, 0, time.Now())

	j, _ := q.Pop(time.Now())
	if j.Payload != 1 {
		t.Fatalf("expected to pop 1, got %d", j.Payload)
	}

	q.Push(wp.Job[int]{Payload: 4}, 0, time.Now())
	q.Push(wp.Job[int]{Payload: 5}, 0, time.Now())

	q.Push(wp.Job[int]{Payload: 6}, 0, time.Now())

	if q.Capacity() <= capacity {
		t.Fatalf("grow() didn't increase capacity")
	}

	if q.Size() != capacity+1 {
		t.Fatalf("expected size=%d after grow, got %d", capacity+1, q.Size())
	}

	expected := []int{2, 3, 4, 5, 6}
	for i, exp := range expected {
		j, ok := q.Pop(time.Now())
		if !ok {
			t.Fatalf("Pop %d returned false", i)
		}
		if j.Payload != exp {
			t.Fatalf("FIFO order broken at %d: expected %d, got %d", i, exp, j.Payload)
		}
	}
}

func TestFifoGrow_MultipleGrows(t *testing.T) {
	capacity := 4
	size := 50
	q := wp.NewFifoQueue[int](capacity)
	for i := 1; i <= size; i++ {
		q.Push(wp.Job[int]{Payload: i}, 0, time.Now())
	}

	if q.Size() != size {
		t.Fatalf("expected size %d, got %d", size, q.Size())
	}

	for i := 1; i <= size; i++ {
		j, ok := q.Pop(time.Now())
		if !ok {
			t.Fatalf("Pop returned false at %d", i)
		}
		if j.Payload != i {
			t.Fatalf("FIFO mismatch at %d: expected %d, got %d", i, i, j.Payload)
		}
	}
}
