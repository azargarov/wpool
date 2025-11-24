package workerpool

// priorityQueue is a max-heap of *item[T] values ordered by effective priority.
//
// It implements the container/heap interface and provides the core data
// structure used by prioQueue. Higher effective priority values are
// considered "larger" and therefore bubble to the top of the heap.
type priorityQueue[T any] []item[T]

// Len returns the number of items currently stored in the heap.
func (pq priorityQueue[T]) Len() int { return len(pq) }

// Less reports whether element i should sort before element j.
//
// Since this is a max-heap, jobs with higher effective priority (eff)
// are considered "less" in terms of index order so they rise to the top.
func (pq priorityQueue[T]) Less(i, j int) bool {
	return pq[i].eff > pq[j].eff // max-heap
}

// Swap exchanges the elements with indexes i and j and updates their indices.
func (pq priorityQueue[T]) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

// Push appends a new item to the heap.
//
// This method is required by the container/heap interface and should not be
// called directly; use heap.Push(&pq, x) instead.
func (pq *priorityQueue[T]) Push(x any) {
	it := x.(item[T])
	it.index = len(*pq)
	*pq = append(*pq, it)
}

// Pop removes and returns the last item from the heap.
//
// This method is required by the container/heap interface and should not be
// called directly; use heap.Pop(&pq) instead.
func (pq *priorityQueue[T]) Pop() any {
	old := *pq
	n := len(old)
	it := old[n-1]
	//old[n-1] = nil // avoid memory leak
	it.index = -1 // mark as removed
	*pq = old[:n-1]
	return it
}
