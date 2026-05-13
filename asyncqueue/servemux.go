package asyncqueue

import (
	"github.com/liuxiaozhicn/async-queue-go/pkg/queue"
	"sync"
)

// ServeMux stores queue handlers by logical queue name.
//
// It is concurrency-safe for registration and read access.
type ServeMux struct {
	mu       sync.RWMutex
	handlers map[string]queue.Handler
}

// NewServeMux creates an empty, concurrency-safe handler registry.
func NewServeMux() *ServeMux {
	mux := &ServeMux{handlers: make(map[string]queue.Handler)}
	return mux
}

// Handle registers or replaces a handler for queue name.
func (r *ServeMux) Handle(name string, handler queue.Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[name] = handler
}

// Get returns the handler bound to queue name.
//
// The second return value reports whether a binding exists.
func (r *ServeMux) Get(name string) (queue.Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[name]
	return h, ok
}
