package workerpool

import (
	"time"
)

// RetryPolicy describes how many times and how often a job should be retried.
// Zero values are treated as "use pool defaults".
type RetryPolicy struct {
	// Attempts is the maximum number of tries for a job.
	Attempts int

	// Initial is the first backoff duration.
	Initial time.Duration

	// Max is the cap for backoff duration.
	Max time.Duration
}

// GetDefaultRP returns a pointer to a default retry policy used by the pool.
// Useful in tests or when constructing a pool with the same defaults.
func GetDefaultRP() *RetryPolicy {
	rp := RetryPolicy{
		Attempts: defaultAttempts,
		Initial:  defaultInitialRetry,
		Max:      defauiltMaxRetry,
	}
	return &rp
}
