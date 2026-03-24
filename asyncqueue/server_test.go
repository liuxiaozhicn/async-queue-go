package asyncqueue

import (
	"context"
	"encoding/json"
	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
	"github.com/liuxiaozhicn/async-queue-go/pkg/queue"
	"github.com/redis/go-redis/v9"
	"os"
	"path/filepath"
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
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()
	t.Run("load server from config file", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "config.json")
		configData := map[string]any{
			"queues": map[string]any{
				"default": map[string]any{
					"channel": "{queue}",
					"enabled": false,
				},
			},
		}
		data, _ := json.Marshal(configData)
		if err := os.WriteFile(configFile, data, 0o644); err != nil {
			t.Fatal(err)
		}

		s, err := LoadServer(configFile, client)
		if err != nil {
			t.Fatalf("LoadServer failed: %v", err)
		}
		if s == nil || s.config == nil || s.serveMux == nil || s.manager == nil {
			t.Fatal("server not initialized correctly")
		}
	})

	t.Run("new server from config alias", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "config.json")
		configData := map[string]any{
			"queues": map[string]any{
				"default": map[string]any{
					"redis_addr": "127.0.0.1:6379",
					"channel":    "{queue}",
					"enabled":    false,
				},
			},
		}
		data, _ := json.Marshal(configData)
		if err := os.WriteFile(configFile, data, 0o644); err != nil {
			t.Fatal(err)
		}

		s, err := NewServerFromConfig(configFile, client)
		if err != nil {
			t.Fatalf("NewServerFromConfig failed: %v", err)
		}
		if s == nil {
			t.Fatal("expected server")
		}
	})

	t.Run("handle registers handler", func(t *testing.T) {
		s, err := NewServer(&Config{Queues: map[string]QueueConfig{}}, client)
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
		s, err := NewServer(&Config{Queues: map[string]QueueConfig{}}, client)
		if err != nil {
			t.Fatalf("NewServer failed: %v", err)
		}

		_, err = s.Queue("missing")
		if err == nil {
			t.Fatal("expected queue error")
		}
	})
}
