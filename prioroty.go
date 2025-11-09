package workerpool

// priorityQueue — max-heap по eff
type priorityQueue[T any] []*item[T]

func (pq priorityQueue[T]) Len() int { return len(pq) }
func (pq priorityQueue[T]) Less(i, j int) bool {
	return pq[i].eff > pq[j].eff // max-heap
}
func (pq priorityQueue[T]) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *priorityQueue[T]) Push(x any) {
	it := x.(*item[T])
	it.index = len(*pq)
	*pq = append(*pq, it)
}

func (pq *priorityQueue[T]) Pop() any {
	old := *pq
	n := len(old)
	it := old[n-1]
	old[n-1] = nil
	it.index = -1
	*pq = old[:n-1]
	return it
}
