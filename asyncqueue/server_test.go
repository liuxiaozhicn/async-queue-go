package asyncqueue

import (
	"context"
	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
	"github.com/liuxiaozhicn/async-queue-go/pkg/queue"
	"testing"
)

type bindTestJob struct {
	Value string `json:"value"`
}

func (j *bindTestJob) GetType() string { return "bindTestJob" }

type bindTestJob2 struct {
}

func (j *bindTestJob2) GetType() string { return "bindTestJob2" }

func TestServerLoadAndLifecycle(t *testing.T) {
	t.Run("create server from config", func(t *testing.T) {
		cfg := &Config{
			Queues: map[string]QueueConfig{
				"default": {
					Channel: "{queue}",
					Enabled: false,
				},
			},
		}
		s, err := NewServer(cfg)
		if err != nil {
			t.Fatalf("NewServer failed: %v", err)
		}
		if s == nil || s.config == nil || s.serveMux == nil || s.manager == nil {
			t.Fatal("server not initialized correctly")
		}
	})

	t.Run("create server from minimal config", func(t *testing.T) {
		cfg := &Config{
			Queues: map[string]QueueConfig{
				"default": {
					Channel: "{queue}",
					Enabled: false,
				},
			},
		}
		s, err := NewServer(cfg)
		if err != nil {
			t.Fatalf("NewServer failed: %v", err)
		}
		if s == nil {
			t.Fatal("expected server")
		}
	})

	t.Run("handle registers handler", func(t *testing.T) {
		s, err := NewServer(&Config{Queues: map[string]QueueConfig{}})
		if err != nil {
			t.Fatalf("NewServer failed: %v", err)
		}

		handler := func(context.Context, *core.Message) (core.Result, error) { return core.ACK, nil }
		s.Handle("default", queue.HandlerFunc(handler))

		got, ok := s.serveMux.Get("default")
		if !ok {
			t.Fatal("handler not registered")
		}
		if got == nil {
			t.Fatal("handler is nil")
		}
	})

	t.Run("queue before start returns error", func(t *testing.T) {
		s, err := NewServer(&Config{Queues: map[string]QueueConfig{}})
		if err != nil {
			t.Fatalf("NewServer failed: %v", err)
		}

		_, err = s.Queue("missing")
		if err == nil {
			t.Fatal("expected queue error")
		}
	})
}
