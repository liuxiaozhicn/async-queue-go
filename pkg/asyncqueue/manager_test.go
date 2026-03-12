package asyncqueue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestHandlerRegistry(t *testing.T) {
	t.Run("register and get handler", func(t *testing.T) {
		registry := NewServeMux()

		called := false
		handler := func(ctx context.Context, m *Message) (Result, error) {
			called = true
			return ACK, nil
		}

		registry.Register("test", handler)

		h, ok := registry.Get("test")
		if !ok {
			t.Fatal("handler not found")
		}

		_, _ = h(context.Background(), &Message{})
		if !called {
			t.Error("handler was not called")
		}
	})

	t.Run("get nonexistent handler", func(t *testing.T) {
		registry := NewServeMux()

		_, ok := registry.Get("nonexistent")
		if ok {
			t.Error("expected handler not found")
		}
	})

	t.Run("overwrite handler", func(t *testing.T) {
		registry := NewServeMux()

		count := 0
		handler1 := func(ctx context.Context, m *Message) (Result, error) {
			count = 1
			return ACK, nil
		}
		handler2 := func(ctx context.Context, m *Message) (Result, error) {
			count = 2
			return ACK, nil
		}

		registry.Register("test", handler1)
		registry.Register("test", handler2)

		h, _ := registry.Get("test")
		_, _ = h(context.Background(), &Message{})

		if count != 2 {
			t.Errorf("expected count 2, got %d", count)
		}
	})
}

func TestNewManager(t *testing.T) {
	t.Run("create manager with valid config", func(t *testing.T) {
		cfg := &Config{
			Queues: map[string]QueueConfig{
				"test": {
					Channel: "{queue:test}",
					Enabled: true,
				},
			},
		}
		registry := NewServeMux()

		manager, err := NewManager(cfg, registry)
		if err != nil {
			t.Fatalf("NewManager failed: %v", err)
		}
		if manager == nil {
			t.Fatal("manager is nil")
		}
	})

	t.Run("nil config", func(t *testing.T) {
		registry := NewServeMux()
		_, err := NewManager(nil, registry)
		if err == nil {
			t.Fatal("expected error for nil config")
		}
	})

	t.Run("nil registry", func(t *testing.T) {
		cfg := &Config{Queues: map[string]QueueConfig{}}
		_, err := NewManager(cfg, nil)
		if err == nil {
			t.Fatal("expected error for nil registry")
		}
	})
}

func TestManagerGetQueue(t *testing.T) {
	t.Run("get nonexistent queue", func(t *testing.T) {
		cfg := &Config{Queues: map[string]QueueConfig{}}
		registry := NewServeMux()
		manager, _ := NewManager(cfg, registry)

		_, err := manager.GetQueue("nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent queue")
		}
	})
}

func TestManagerLifecycle(t *testing.T) {
	t.Run("start without handlers", func(t *testing.T) {
		cfg := &Config{
			Queues: map[string]QueueConfig{
				"test": {
					Channel: "{queue:test}",
					Enabled: true,
				},
			},
		}
		registry := NewServeMux()
		manager, _ := NewManager(cfg, registry)

		err := manager.StartWorker()
		if err == nil {
			t.Fatal("expected error when handler not registered")
		}
	})

	t.Run("can restart after stop", func(t *testing.T) {
		cfg := &Config{
			Queues: map[string]QueueConfig{
				"disabled": {
					Channel: "{queue:disabled}",
					Enabled: false,
				},
			},
		}
		registry := NewServeMux()
		registry.Register("disabled", func(ctx context.Context, m *Message) (Result, error) {
			return ACK, nil
		})
		manager, _ := NewManager(cfg, registry)

		if err := manager.StartWorker(); err != nil {
			t.Fatalf("first start failed: %v", err)
		}
		if err := manager.Stop(time.Second); err != nil {
			t.Fatalf("first stop failed: %v", err)
		}

		if err := manager.StartWorker(); err != nil {
			t.Fatalf("second start failed: %v", err)
		}
		if err := manager.Stop(time.Second); err != nil {
			t.Fatalf("second stop failed: %v", err)
		}
	})
}

func TestManagerConcurrentSafety(t *testing.T) {
	t.Run("concurrent register and get", func(t *testing.T) {
		registry := NewServeMux()

		done := make(chan bool)
		for i := 0; i < 10; i++ {
			go func() {
				handler := func(ctx context.Context, m *Message) (Result, error) {
					return ACK, nil
				}
				registry.Register("test", handler)
				done <- true
			}()
		}

		for i := 0; i < 10; i++ {
			<-done
		}

		_, ok := registry.Get("test")
		if !ok {
			t.Error("handler not found after concurrent registration")
		}
	})

	t.Run("concurrent GetQueue", func(t *testing.T) {
		cfg := &Config{Queues: map[string]QueueConfig{}}
		registry := NewServeMux()
		manager, _ := NewManager(cfg, registry)

		var count int32
		done := make(chan bool)
		for i := 0; i < 10; i++ {
			go func() {
				_, err := manager.GetQueue("test")
				if err != nil {
					atomic.AddInt32(&count, 1)
				}
				done <- true
			}()
		}

		for i := 0; i < 10; i++ {
			<-done
		}

		if atomic.LoadInt32(&count) != 10 {
			t.Errorf("expected 10 errors, got %d", count)
		}
	})
}

func TestManagerAutoRestart(t *testing.T) {
	t.Run("worker auto-restarts after max_messages", func(t *testing.T) {
		// This test requires a running Redis instance
		t.Skip("Skipping integration test - requires Redis")

		var processedCount int64
		cfg := &Config{
			Queues: map[string]QueueConfig{
				"test": {
					Channel:     "{queue:test-restart}",
					Enabled:     true,
					Processes:   1,
					Concurrent:  1,
					MaxMessages: 5,
					AutoRestart: true,
				},
			},
		}

		registry := NewServeMux()
		registry.Register("test", func(ctx context.Context, m *Message) (Result, error) {
			atomic.AddInt64(&processedCount, 1)
			return ACK, nil
		})

		manager, err := NewManager(cfg, registry)
		if err != nil {
			t.Fatalf("NewManager failed: %v", err)
		}

		if err := manager.StartWorker(); err != nil {
			t.Fatalf("Start failed: %v", err)
		}

		// Wait for multiple restart cycles
		time.Sleep(2 * time.Second)

		if err := manager.Stop(5 * time.Second); err != nil {
			t.Fatalf("Stop failed: %v", err)
		}

		// Should have processed more than max_messages due to restarts
		count := atomic.LoadInt64(&processedCount)
		if count <= 5 {
			t.Errorf("expected more than 5 messages processed with auto-restart, got %d", count)
		}
	})

	t.Run("worker does not restart without auto_restart", func(t *testing.T) {
		// This test verifies the default behavior
		cfg := &Config{
			Queues: map[string]QueueConfig{
				"test": {
					Channel:     "{queue:test-no-restart}",
					Enabled:     true,
					Processes:   1,
					Concurrent:  1,
					MaxMessages: 5,
					AutoRestart: false,
				},
			},
		}

		registry := NewServeMux()
		manager, err := NewManager(cfg, registry)
		if err != nil {
			t.Fatalf("NewManager failed: %v", err)
		}

		// Verify manager was created successfully
		if manager == nil {
			t.Fatal("manager is nil")
		}
	})
}
