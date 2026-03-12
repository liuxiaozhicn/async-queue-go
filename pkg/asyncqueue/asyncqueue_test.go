package asyncqueue

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestQueuePushMessageAndInfo(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	q, err := NewAsyncQueue(client, "{kit-test}", 1, 1, []int{1, 2}, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()
	payload1, _ := json.Marshal(map[string]int{"id": 1})
	if err := q.PushMessage(ctx, &Message{Payload: payload1, MaxAttempts: 2}, 0); err != nil {
		t.Fatal(err)
	}
	payload2, _ := json.Marshal(map[string]int{"id": 2})
	if err := q.PushMessage(ctx, &Message{Payload: payload2, MaxAttempts: 2}, 5); err != nil {
		t.Fatal(err)
	}

	info, err := q.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Waiting != 1 || info.Delayed != 1 {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestWorkerConsumesMessage(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	q, err := NewAsyncQueue(client, "{kit-worker}", 1, 1, []int{1}, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()
	payload, _ := json.Marshal(map[string]string{"kind": "once"})
	if err := q.PushMessage(ctx, &Message{Payload: payload, MaxAttempts: 2}, 0); err != nil {
		t.Fatal(err)
	}

	called := 0
	cfg := &Config{
		Queues: map[string]QueueConfig{
			"test": {
				Channel:        "{kit-worker}",
				Enabled:        true,
				Processes:      1,
				Concurrent:     1,
				MaxMessages:    1,
				TimeoutSeconds: 1,
				HandleTimeout:  1,
				RetrySeconds:   []int{1},
			},
		},
	}
	serveMux := NewServeMux()
	serveMux.Register("test", func(_ context.Context, _ *Message) (Result, error) {
		called++
		return ACK, nil
	})
	manager, err := NewManagerWithRedis(cfg, serveMux, client)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.StartWorker(); err != nil {
		t.Fatal(err)
	}
	if err := manager.Wait(); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("expected handler called once, got %d", called)
	}
}

func TestQueuePushJobAndInfo(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	q, err := NewAsyncQueue(client, "kit-pushjob", 1, 1, []int{1, 2}, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()

	// Push job without delay
	job1 := &testEmailJob{
		To:      "user@example.com",
		Subject: "Test",
		Body:    "Hello",
	}
	if err := q.PushJob(ctx, job1, 0); err != nil {
		t.Fatal(err)
	}

	// Push job with delay
	job2 := &testEmailJob{
		To:      "admin@example.com",
		Subject: "Delayed",
		Body:    "World",
	}
	if err := q.PushJob(ctx, job2, 5); err != nil {
		t.Fatal(err)
	}

	info, err := q.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Waiting != 1 || info.Delayed != 1 {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestQueuePushJobNilJob(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	q, err := NewAsyncQueue(client, "{kit-pushjob-nil}", 1, 1, []int{1}, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()
	err = q.PushJob(ctx, nil, 0)
	if err == nil {
		t.Fatal("expected error for nil job")
	}
}

func TestWorkerConsumesJobMessage(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	q, err := NewAsyncQueue(client, "{kit-worker-job}", 1, 1, []int{1}, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()
	job := &testEmailJob{
		To:      "test@example.com",
		Subject: "Worker Test",
		Body:    "Content",
	}
	if err := q.PushJob(ctx, job, 0); err != nil {
		t.Fatal(err)
	}

	called := 0
	var receivedJob *testEmailJob
	cfg := &Config{
		Queues: map[string]QueueConfig{
			"test": {
				Channel:        "{kit-worker-job}",
				Enabled:        true,
				Processes:      1,
				Concurrent:     1,
				MaxMessages:    1,
				TimeoutSeconds: 1,
				HandleTimeout:  1,
				RetrySeconds:   []int{1},
			},
		},
	}
	serveMux := NewServeMux()
	serveMux.Register("test", func(ctx context.Context, m *Message) (Result, error) {
		called++
		var j testEmailJob
		if err := json.Unmarshal(m.Payload, &j); err != nil {
			return DROP, err
		}
		receivedJob = &j
		return ACK, nil
	})
	manager, err := NewManagerWithRedis(cfg, serveMux, client)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.StartWorker(); err != nil {
		t.Fatal(err)
	}
	if err := manager.Wait(); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("expected handler called once, got %d", called)
	}
	if receivedJob == nil {
		t.Fatal("expected receivedJob to be set")
	}
	if receivedJob.To != "test@example.com" || receivedJob.Subject != "Worker Test" || receivedJob.Body != "Content" {
		t.Fatalf("unexpected job data: %+v", receivedJob)
	}
}
