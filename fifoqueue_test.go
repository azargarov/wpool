package workerpool

import (
	"testing"
	"time"
)

func TestFifoGrow_NoWrap(t *testing.T) {
	capacity := 4
	newSize := 5
	q := newFifoQueue[int](capacity)

	for i := 1; i <= capacity; i++ {
		q.Push(Job[int]{Payload: i}, 0, time.Now())
	}

	if q.size != capacity {
		t.Fatalf("expected size=4, got %d", q.size)
	}

	q.Push(Job[int]{Payload: 5}, 0, time.Now())

	if q.capacity <= capacity {
		t.Fatalf("grow() didn't increase capacity, got %d", q.capacity)
	}

	if q.size != newSize {
		t.Fatalf("after grow: expected size=%d, got %d", newSize, q.size)
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
	q := newFifoQueue[int](capacity)

	q.Push(Job[int]{Payload: 1}, 0, time.Now())
	q.Push(Job[int]{Payload: 2}, 0, time.Now())
	q.Push(Job[int]{Payload: 3}, 0, time.Now())

	// Делаем wrap-around: Pop → head=1
	j, _ := q.Pop(time.Now())
	if j.Payload != 1 {
		t.Fatalf("expected to pop 1, got %d", j.Payload)
	}

	//  head=1 tail=3
	q.Push(Job[int]{Payload: 4}, 0, time.Now())
	q.Push(Job[int]{Payload: 5}, 0, time.Now())

	// queue state:
	// [5,2,3,4]
	// head=1 tail=1 size=4 (full, wrap-around)

	// Следующий push вызывает grow()
	q.Push(Job[int]{Payload: 6}, 0, time.Now())

	if q.capacity <= capacity {
		t.Fatalf("grow() didn't increase capacity")
	}

	if q.size != capacity+1 {
		t.Fatalf("expected size=%d after grow, got %d", capacity+1, q.size)
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
	q := newFifoQueue[int](capacity)
	for i := 1; i <= size; i++ {
		q.Push(Job[int]{Payload: i}, 0, time.Now())
	}

	if q.size != size {
		t.Fatalf("expected size %d, got %d", size, q.size)
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
