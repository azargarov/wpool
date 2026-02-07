package workerpool

type ScheduledBatch[T any] struct {
	Batch[T]
	Bucket uint8
}

type scheduler[T any] interface {
	Push(job Job[T], prio BucketPriority) error
	Pop() (ScheduledBatch[T], bool)
	OnBatchDone(b ScheduledBatch[T])
}

