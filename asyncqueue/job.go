package asyncqueue

// Job is the interface that every job struct must implement.
// GetType returns the queue name this job is bound to.
type Job interface {
	GetType() string
}
