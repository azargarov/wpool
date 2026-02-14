package workerpool

type segmentPoolProvider[T any] interface {
    Get() *segment[T]
    Put(*segment[T])
    StatSnapshot() string
}
