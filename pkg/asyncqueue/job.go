package asyncqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
)

// Job is the interface that every job struct must implement.
// GetType returns the queue name this job is bound to.
type Job interface {
	GetType() string
	Handle(context.Context) (Result, error)
}

// WrapJob creates a Handler from a Job prototype. Use it when building a
// HandlerRegistry manually (e.g. inside a queueHandle function).
func WrapJob(job Job) Handler { return wrapJob(job) }

// wrapJob creates a Handler from a Job prototype.
// On each invocation it allocates a fresh instance of the same concrete type,
// Unmarshal the message payload into it, then calls Handle.
func wrapJob(job Job) Handler {
	if job == nil {
		panic("wrapJob: prototype must not be nil")
	}
	t := reflect.TypeOf(job)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return func(ctx context.Context, m *Message) (Result, error) {
		instance, ok := reflect.New(t).Interface().(Job)
		if !ok {
			return DROP, fmt.Errorf("job: type %s does not implement Job via pointer receiver", t.Name())
		}
		if err := json.Unmarshal(m.Payload, instance); err != nil {
			return DROP, fmt.Errorf("unmarshal %s: %w", job.GetType(), err)
		}
		return instance.Handle(ctx)
	}
}
