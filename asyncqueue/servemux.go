package asyncqueue

import (
	"github.com/liuxiaozhicn/async-queue-go/pkg/queue"
	"sync"
)

// ServeMux stores handlers by queue name.
type ServeMux struct {
	mu       sync.RWMutex
	handlers map[string]queue.Handler
}

// NewServeMux creates a ServeMux and optionally registers jobs directly.
func NewServeMux() *ServeMux {
	mux := &ServeMux{handlers: make(map[string]queue.Handler)}
	return mux
}

// Handle registers a handler for a queue.
func (r *ServeMux) Handle(name string, handler queue.Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[name] = handler
}

// Get returns the handler for a queue.
func (r *ServeMux) Get(name string) (queue.Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[name]
	return h, ok
}
