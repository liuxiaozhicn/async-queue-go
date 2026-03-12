package asyncqueue

import (
	"context"
	"testing"
)

type testJobA struct {
	Value string `json:"value"`
}

func (j *testJobA) GetType() string { return "test_queue_a" }
func (j *testJobA) Handle(ctx context.Context) (Result, error) {
	return ACK, nil
}

type testJobB struct {
	Count int `json:"count"`
}

func (j *testJobB) GetType() string { return "test_queue_b" }
func (j *testJobB) Handle(ctx context.Context) (Result, error) {
	return ACK, nil
}

func TestNewJobs(t *testing.T) {
	jobA := &testJobA{}
	jobB := &testJobB{}

	jobs := NewJobs(jobA, jobB)
	if jobs == nil {
		t.Fatal("expected non-nil Jobs")
	}

	reg := jobs.HandleServeMux()

	// Verify both jobs are registered
	handlerA, okA := reg.Get("test_queue_a")
	if !okA {
		t.Fatal("expected test_queue_a to be registered")
	}
	if handlerA == nil {
		t.Fatal("expected non-nil handler for test_queue_a")
	}

	handlerB, okB := reg.Get("test_queue_b")
	if !okB {
		t.Fatal("expected test_queue_b to be registered")
	}
	if handlerB == nil {
		t.Fatal("expected non-nil handler for test_queue_b")
	}

	// Verify non-existent queue returns false
	_, okC := reg.Get("non_existent")
	if okC {
		t.Fatal("expected non_existent queue to not be registered")
	}
}

func TestNewJobs_Empty(t *testing.T) {
	jobs := NewJobs()
	if jobs == nil {
		t.Fatal("expected non-nil Jobs")
	}

	reg := jobs.HandleServeMux()
	_, ok := reg.Get("any_queue")
	if ok {
		t.Fatal("expected empty registry")
	}
}

func TestNewJobs_SingleJob(t *testing.T) {
	job := &testJobA{}
	jobs := NewJobs(job)

	reg := jobs.HandleServeMux()
	handler, ok := reg.Get("test_queue_a")
	if !ok {
		t.Fatal("expected test_queue_a to be registered")
	}
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
}
