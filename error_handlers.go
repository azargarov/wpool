package workerpool

import ()

// reportInternalError reports an internal pool error.
//
// Internal errors are non-job-related failures such as
// worker setup issues or unexpected runtime conditions.
// If no handler is registered, the error is silently ignored.
func (p *Pool[T, M]) reportInternalError(e error) {
	if p.OnInternalError != nil {
		p.OnInternalError(e)
	}
}

// reportJobError reports an error returned by a job or
// produced by panic recovery.
//
// Job errors do not stop pool execution and are reported
// asynchronously via the configured handler.
func (p *Pool[T, M]) reportJobError(err error) {
	if p.OnJobError != nil {
		p.OnJobError(err)
	}
}
