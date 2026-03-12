package asyncqueue

import "sync"

// ServeMux stores handlers by queue name.
type ServeMux struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewServeMux creates a ServeMux and optionally registers jobs directly.
func NewServeMux(jobs ...Job) *ServeMux {
	mux := &ServeMux{handlers: make(map[string]Handler)}
	for _, job := range jobs {
		if job == nil {
			continue
		}
		mux.handlers[job.GetType()] = WrapJob(job)
	}
	return mux
}

// Register registers a handler for a queue.
func (r *ServeMux) Register(queueName string, handler Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[queueName] = handler
}

// Get returns the handler for a queue.
func (r *ServeMux) Get(queueName string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[queueName]
	return h, ok
}
