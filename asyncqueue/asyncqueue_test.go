package asyncqueue

import (
	"context"
	"encoding/json"
	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
	"github.com/liuxiaozhicn/async-queue-go/pkg/logger"
	"testing"

	"github.com/liuxiaozhicn/async-queue-go/pkg/queue"
	"github.com/redis/go-redis/v9"
)

func requireRedis(t *testing.T) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis not available: %v", err)
	}
}

func TestQueuePushMessageAndInfo(t *testing.T) {
	requireRedis(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	q, err := NewAsyncQueue(client, "{kit-test}", 1, 1, []int{1, 2}, 3, "", logger.Default)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()
	payload1, _ := json.Marshal(map[string]int{"id": 1})
	if _, err := q.PushMessage(ctx, &core.Message{Payload: payload1, MaxAttempts: 2}, 0); err != nil {
		t.Fatal(err)
	}
	payload2, _ := json.Marshal(map[string]int{"id": 2})
	if _, err := q.PushMessage(ctx, &core.Message{Payload: payload2, MaxAttempts: 2}, 5); err != nil {
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
	requireRedis(t)

	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	q, err := NewAsyncQueue(client, "{kit-worker}", 1, 1, []int{1}, 3, "", logger.Default)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()
	payload, _ := json.Marshal(map[string]string{"kind": "once"})
	if _, err := q.PushMessage(ctx, &core.Message{Payload: payload, MaxAttempts: 2}, 0); err != nil {
		t.Fatal(err)
	}

	called := 0
	cfg := &Config{
		Queues: map[string]QueueConfig{
			"test": {
				Channel:       "{kit-worker}",
				Enabled:       true,
				Processes:     1,
				Concurrent:    1,
				MaxMessages:   1,
				PopTimeout:    1,
				HandleTimeout: 1,
				RetrySeconds:  []int{1},
			},
		},
	}
	serveMux := NewServeMux()
	serveMux.Handle("test", queue.HandlerFunc(func(_ context.Context, _ *core.Message) (core.Result, error) {
		called++
		return core.ACK, nil
	}))
	manager, err := NewManagerWithRedis(cfg, serveMux, client, nil)
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
	requireRedis(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	q, err := NewAsyncQueue(client, "kit-pushjob", 1, 1, []int{1, 2}, 3, "", logger.Default)
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
	if _, err := q.PushJob(ctx, job1, 0); err != nil {
		t.Fatal(err)
	}

	// Push job with delay
	job2 := &testEmailJob{
		To:      "admin@example.com",
		Subject: "Delayed",
		Body:    "World",
	}
	if _, err := q.PushJob(ctx, job2, 5); err != nil {
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
	requireRedis(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	q, err := NewAsyncQueue(client, "{kit-pushjob-nil}", 1, 1, []int{1}, 3, "", logger.Default)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()
	_, err = q.PushJob(ctx, nil, 0)
	if err == nil {
		t.Fatal("expected error for nil job")
	}
}

func TestWorkerConsumesJobMessage(t *testing.T) {
	requireRedis(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	q, err := NewAsyncQueue(client, "{kit-worker-job}", 1, 1, []int{1}, 3, "", logger.Default)
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
	if _, err := q.PushJob(ctx, job, 0); err != nil {
		t.Fatal(err)
	}

	called := 0
	var receivedJob *testEmailJob
	cfg := &Config{
		Queues: map[string]QueueConfig{
			"test": {
				Channel:       "{kit-worker-job}",
				Enabled:       true,
				Processes:     1,
				Concurrent:    1,
				MaxMessages:   1,
				PopTimeout:    1,
				HandleTimeout: 1,
				RetrySeconds:  []int{1},
			},
		},
	}
	serveMux := NewServeMux()
	serveMux.Handle("test", queue.HandlerFunc(func(ctx context.Context, m *core.Message) (core.Result, error) {
		called++
		receivedJob = &testEmailJob{}
		_ = json.Unmarshal(m.Payload, receivedJob)
		return core.ACK, nil
	}))
	manager, err := NewManagerWithRedis(cfg, serveMux, client, nil)
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
