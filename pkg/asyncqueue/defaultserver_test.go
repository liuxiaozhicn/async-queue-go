package asyncqueue

import (
	"bytes"
	"context"
	"github.com/redis/go-redis/v9"
	"log"
	"os"
	"strings"
	"testing"
)

// helper: minimal config that needs no real Redis (no enabled queues)
func minimalConfig() *Config {
	return &Config{Queues: map[string]QueueConfig{}}
}

func resetDefault(t *testing.T) {
	t.Helper()
	SetDefaultServer(nil)
	t.Cleanup(func() { SetDefaultServer(nil) })
}

func TestDefault_NilBeforeNewServer(t *testing.T) {
	resetDefault(t)
	if DefaultServer() != nil {
		t.Fatal("expected nil before any NewServer call")
	}
}

func TestDefault_SetDefault(t *testing.T) {
	resetDefault(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()
	s, err := NewServer(minimalConfig(), client)
	if err != nil {
		t.Fatal(err)
	}
	SetDefaultServer(s)
	if DefaultServer() != s {
		t.Fatal("Default() should return the set server")
	}
}

func TestDefault_PushWithNilGlobal(t *testing.T) {
	resetDefault(t)
	err := Push(context.Background(), "q", &testDefaultJob{}, 0)
	if err == nil {
		t.Fatal("expected error when no default server")
	}
}

func TestDefault_PushMessageWithNilGlobal(t *testing.T) {
	resetDefault(t)
	err := PushMessage(context.Background(), "q", &Message{}, 0)
	if err == nil {
		t.Fatal("expected error when no default server")
	}
}

func TestDefault_GetQueueWithNilGlobal(t *testing.T) {
	resetDefault(t)
	_, err := GetQueue("q")
	if err == nil {
		t.Fatal("expected error when no default server")
	}
}

// testDefaultJob is a minimal Job for use in this test file.
type testDefaultJob struct{}

func (j *testDefaultJob) GetType() string                          { return "testDefaultJob" }
func (j *testDefaultJob) Handle(_ context.Context) (Result, error) { return ACK, nil }

func TestSetDefaultWithWarn_Overwrite(t *testing.T) {
	resetDefault(t)

	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	s1, err := NewServer(minimalConfig(), client)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := NewServer(minimalConfig(), client)
	if err != nil {
		t.Fatal(err)
	}

	// Capture log output
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	SetDefaultServer(s1) // first: no warn (global was nil after resetDefault)
	buf.Reset()

	setDefaultWithWarn(s2) // second: global is s1 → should warn
	if !strings.Contains(buf.String(), "warn") {
		t.Fatal("expected overwrite warning log when replacing non-nil default server")
	}
	if DefaultServer() != s2 {
		t.Fatal("global should be s2 after setDefaultWithWarn(s2)")
	}
}

func TestDefault_NewServerSetsGlobal(t *testing.T) {
	resetDefault(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	s, err := NewServer(minimalConfig(), client)
	if err != nil {
		t.Fatal(err)
	}
	if DefaultServer() != s {
		t.Fatal("NewServer should auto-set the global default")
	}
}

func TestDefault_NewServerOverwritesGlobal(t *testing.T) {
	resetDefault(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()
	s1, _ := NewServer(minimalConfig(), client)
	s2, _ := NewServer(minimalConfig(), client)
	if DefaultServer() != s2 {
		t.Fatalf("second NewServer should overwrite global: got %p want %p", DefaultServer(), s2)
	}
	_ = s1
}

func TestDefault_StopClearsGlobal(t *testing.T) {
	resetDefault(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()
	s, err := NewServer(minimalConfig(), client)
	if err != nil {
		t.Fatal(err)
	}
	if DefaultServer() != s {
		t.Fatal("NewServer should set global")
	}
	if err := s.StartWorker(); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(0); err != nil {
		t.Fatal(err)
	}
	if DefaultServer() != nil {
		t.Fatal("Stop should clear global default")
	}
}

func TestDefault_StopNonDefaultDoesNotClearGlobal(t *testing.T) {
	resetDefault(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()
	s1, _ := NewServer(minimalConfig(), client)
	s2, _ := NewServer(minimalConfig(), client) // s2 is now the global
	_ = s1

	if DefaultServer() != s2 {
		t.Fatal("s2 should be global")
	}

	// Stop s1 — s2 is still global, should not be cleared
	if err := s1.StartWorker(); err != nil {
		t.Fatal(err)
	}
	if err := s1.Stop(0); err != nil {
		t.Fatal(err)
	}
	if DefaultServer() != s2 {
		t.Fatal("stopping non-default server should not clear global")
	}
}
