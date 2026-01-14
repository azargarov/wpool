package workerpool

import (
)


type schedQueue[T any] interface {

	Push(job Job[T]) bool

	BatchPop() ([]Job[T], bool)

	Len() int
}

func (p *Pool[T, M]) makeQueue() schedQueue[T] {
	switch p.opts.QT {
	case SegmentedQueue:
		return NewSegmentedQ[T](uint32(p.opts.SegmentSize), p.opts.SegmentCount)

	default:
		return NewSegmentedQ[T](DefaultSegmentSize, DefaultSegmentCount)

	}
}
