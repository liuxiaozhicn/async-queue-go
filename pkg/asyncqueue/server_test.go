package asyncqueue

import (
	"context"
	"encoding/json"
	"github.com/redis/go-redis/v9"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type bindTestJob struct {
	Value string `json:"value"`
}

func (j *bindTestJob) GetType() string { return "bindTestJob" }
func (j *bindTestJob) Handle(ctx context.Context) (Result, error) {
	return ACK, nil
}

type bindTestJob2 struct {
}

func (j *bindTestJob2) GetType() string { return "bindTestJob2" }
func (j *bindTestJob2) Handle(ctx context.Context) (Result, error) {
	return ACK, nil
}

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

		handler := func(context.Context, *Message) (Result, error) { return ACK, nil }
		s.Handle("default", handler)

		got, ok := s.serveMux.Get("default")
		if !ok {
			t.Fatal("handler not registered")
		}
		if got == nil {
			t.Fatal("handler is nil")
		}
	})

	t.Run("start without handler returns error", func(t *testing.T) {
		s, err := NewServer(&Config{
			Queues: map[string]QueueConfig{
				"default": {Channel: "{queue}", Enabled: true},
			},
		}, client)
		if err != nil {
			t.Fatalf("NewServer failed: %v", err)
		}

		err = s.StartWorker()
		if err == nil || !strings.Contains(err.Error(), "handler not registered") {
			t.Fatalf("expected missing handler error, got %v", err)
		}
	})

	t.Run("can restart after stop", func(t *testing.T) {
		s, err := NewServer(&Config{
			Queues: map[string]QueueConfig{
				"default": {Channel: "{queue}", Enabled: false},
			},
		}, client)
		if err != nil {
			t.Fatalf("NewServer failed: %v", err)
		}

		s.Handle("default", func(context.Context, *Message) (Result, error) { return ACK, nil })
		if err := s.StartWorker(); err != nil {
			t.Fatalf("first start failed: %v", err)
		}
		if err := s.Stop(time.Second); err != nil {
			t.Fatalf("first stop failed: %v", err)
		}

		if err := s.StartWorker(); err != nil {
			t.Fatalf("second start failed: %v", err)
		}
		if err := s.Stop(time.Second); err != nil {
			t.Fatalf("second stop failed: %v", err)
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

// initTestJobs mirrors the user's InitQueue pattern.
// Adding a new job field here is the only change needed.
type initTestJobs struct {
	Job1 *bindTestJob
	Job2 *bindTestJob2
}

func TestServerRun(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()
	t.Run("run with registry merges handlers before start", func(t *testing.T) {
		s, err := NewServer(&Config{Queues: map[string]QueueConfig{}}, client)
		if err != nil {
			t.Fatalf("NewServer failed: %v", err)
		}

		reg := NewServeMux()
		reg.Register("bindTestJob", WrapJob(&bindTestJob{}))
		reg.Register("bindTestJob2", WrapJob(&bindTestJob2{}))

		done := make(chan error, 1)
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		go func() { done <- s.Run(ctx, reg) }()
		time.Sleep(10 * time.Millisecond)

		if _, ok := s.serveMux.Get("bindTestJob"); !ok {
			t.Fatal("bindTestJob not registered")
		}
		if _, ok := s.serveMux.Get("bindTestJob2"); !ok {
			t.Fatal("bindTestJob2 not registered")
		}

		if err := s.Stop(time.Second); err != nil {
			t.Fatalf("stop failed: %v", err)
		}
		<-done
	})

	t.Run("run with nil registry is a no-op", func(t *testing.T) {
		s, err := NewServer(&Config{Queues: map[string]QueueConfig{}}, client)
		if err != nil {
			t.Fatalf("NewServer failed: %v", err)
		}
		s.Handle("default", func(context.Context, *Message) (Result, error) { return ACK, nil })

		done := make(chan error, 1)
		go func() { done <- s.Run(nil, nil) }()
		time.Sleep(10 * time.Millisecond)

		if err := s.Stop(time.Second); err != nil {
			t.Fatalf("stop failed: %v", err)
		}
		<-done
	})
}

func TestServerBind(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()
	t.Run("bind registers handler using GetType as queue name", func(t *testing.T) {
		s, err := NewServer(&Config{Queues: map[string]QueueConfig{}}, client)
		if err != nil {
			t.Fatalf("NewServer failed: %v", err)
		}

		s.Bind(&bindTestJob{})

		_, ok := s.serveMux.Get("bindTestJob")
		if !ok {
			t.Fatal("handler not registered under job type name")
		}
	})

	t.Run("bound handler unmarshals payload and calls Handle", func(t *testing.T) {
		s, err := NewServer(&Config{Queues: map[string]QueueConfig{}}, client)
		if err != nil {
			t.Fatalf("NewServer failed: %v", err)
		}

		s.Bind(&bindTestJob{})

		h, _ := s.serveMux.Get("bindTestJob")

		payload, _ := json.Marshal(&bindTestJob{Value: "hello"})
		result, err := h(context.Background(), &Message{Payload: payload})
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		if result != ACK {
			t.Errorf("expected ACK, got %v", result)
		}
	})

	t.Run("bound handler returns DROP on invalid payload", func(t *testing.T) {
		s, err := NewServer(&Config{Queues: map[string]QueueConfig{}}, client)
		if err != nil {
			t.Fatalf("NewServer failed: %v", err)
		}

		s.Bind(&bindTestJob{})

		h, _ := s.serveMux.Get("bindTestJob")

		result, err := h(context.Background(), &Message{Payload: []byte("not-json")})
		if err == nil {
			t.Fatal("expected error for invalid payload")
		}
		if result != DROP {
			t.Errorf("expected DROP, got %v", result)
		}
	})

	t.Run("bind multiple jobs at once", func(t *testing.T) {
		s, err := NewServer(&Config{Queues: map[string]QueueConfig{}}, client)
		if err != nil {
			t.Fatalf("NewServer failed: %v", err)
		}

		s.Bind(&bindTestJob{}, &bindTestJob2{})

		if _, ok := s.serveMux.Get("bindTestJob"); !ok {
			t.Fatal("bindTestJob not registered")
		}
		if _, ok := s.serveMux.Get("bindTestJob2"); !ok {
			t.Fatal("bindTestJob2 not registered")
		}
	})

	t.Run("bind with nil job is a no-op", func(t *testing.T) {
		s, err := NewServer(&Config{Queues: map[string]QueueConfig{}}, client)
		if err != nil {
			t.Fatalf("NewServer failed: %v", err)
		}

		// Must not panic
		s.Bind(nil)
	})
}
