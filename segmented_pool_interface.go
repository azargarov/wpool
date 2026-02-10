package workerpool

type segmentPoolProvider[T any] interface {
    Get(uint32) *segment[T]
    Put(*segment[T])
}

