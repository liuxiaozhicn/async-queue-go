package asyncqueue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
	"github.com/liuxiaozhicn/async-queue-go/pkg/logger"
	"github.com/liuxiaozhicn/async-queue-go/pkg/queue"
	qpkg "github.com/liuxiaozhicn/async-queue-go/pkg/queue"
	"github.com/redis/go-redis/v9"
)

type testEmailJob struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func requireRedis(t *testing.T) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis not available: %v", err)
	}
}

func newTestRedisDriver(client *redis.Client, channel string, popTimeout int, handleTimeout int, retrySeconds []int, messageTTL int) queue.Driver {
	_ = popTimeout
	_ = handleTimeout
	_ = retrySeconds
	_ = messageTTL
	_ = channel
	return queue.NewRedisDriver(client)
}

func TestQueuePushMessageAndInfo(t *testing.T) {
	requireRedis(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	q, err := NewAsyncQueue(newTestRedisDriver(client, "{kit-test}", 1, 1, []int{1, 2}, 0), "{kit-test}",
		WithQueuePopTimeout(1),
		WithQueueHandleTimeout(1),
		WithQueueRetrySeconds([]int{1, 2}),
		WithQueueMessageTTL(0),
		WithQueueMaxAttempts(3),
		WithQueueLogger(logger.Default),
	)
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

func TestNewAsyncQueueWithOptions(t *testing.T) {
	requireRedis(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	q, err := NewAsyncQueue(newTestRedisDriver(client, "{kit-options}", 2, 3, []int{2, 4}, 60), "{kit-options}",
		WithQueuePopTimeout(2),
		WithQueueHandleTimeout(3),
		WithQueueRetrySeconds([]int{2, 4}),
		WithQueueMessageTTL(60),
		WithQueueMaxAttempts(6),
		WithQueueName("opt-queue"),
	)
	if err != nil {
		t.Fatal(err)
	}

	if q.PopTimeout != 2*time.Second {
		t.Fatalf("expected PopTimeout 2s, got %v", q.PopTimeout)
	}
	if q.handleTimeout != 3*time.Second {
		t.Fatalf("expected handleTimeout 3s, got %v", q.handleTimeout)
	}
	if q.maxAttempts != 6 {
		t.Fatalf("expected maxAttempts 6, got %d", q.maxAttempts)
	}
	if q.name != "opt-queue" {
		t.Fatalf("expected name opt-queue, got %s", q.name)
	}
}

func TestNewAsyncQueueNilLoggerFallsBackToDefault(t *testing.T) {
	requireRedis(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	q, err := NewAsyncQueue(newTestRedisDriver(client, "{kit-nil-logger}", 1, 1, []int{1}, 0), "{kit-nil-logger}",
		WithQueuePopTimeout(1),
		WithQueueHandleTimeout(1),
		WithQueueRetrySeconds([]int{1}),
		WithQueueMessageTTL(0),
		WithQueueMaxAttempts(3),
		WithQueueLogger(nil),
	)
	if err != nil {
		t.Fatal(err)
	}
	if q.logger == nil {
		t.Fatal("expected default logger when nil logger is provided")
	}
}

func TestWorkerConsumesMessage(t *testing.T) {
	requireRedis(t)

	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	q, err := NewAsyncQueue(newTestRedisDriver(client, "{kit-worker}", 1, 1, []int{1}, 0), "{kit-worker}",
		WithQueuePopTimeout(1),
		WithQueueHandleTimeout(1),
		WithQueueRetrySeconds([]int{1}),
		WithQueueMessageTTL(0),
		WithQueueMaxAttempts(3),
		WithQueueLogger(logger.Default),
	)
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
				Driver:        "redis",
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
	manager, err := NewManager(cfg, serveMux, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager.RegisterDriver("redis", newTestRedisDriver(client, "{kit-worker}", 1, 1, []int{1}, 0))
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

	q, err := NewAsyncQueue(newTestRedisDriver(client, "kit-pushjob", 1, 1, []int{1, 2}, 0), "kit-pushjob",
		WithQueuePopTimeout(1),
		WithQueueHandleTimeout(1),
		WithQueueRetrySeconds([]int{1, 2}),
		WithQueueMessageTTL(0),
		WithQueueMaxAttempts(3),
		WithQueueLogger(logger.Default),
	)
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
	if _, err := q.PushTask(ctx, job1, 0); err != nil {
		t.Fatal(err)
	}

	// Push job with delay
	job2 := &testEmailJob{
		To:      "admin@example.com",
		Subject: "Delayed",
		Body:    "World",
	}
	if _, err := q.PushTask(ctx, job2, 5); err != nil {
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

	q, err := NewAsyncQueue(newTestRedisDriver(client, "{kit-pushjob-nil}", 1, 1, []int{1}, 0), "{kit-pushjob-nil}",
		WithQueuePopTimeout(1),
		WithQueueHandleTimeout(1),
		WithQueueRetrySeconds([]int{1}),
		WithQueueMessageTTL(0),
		WithQueueMaxAttempts(3),
		WithQueueLogger(logger.Default),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()
	_, err = q.PushTask(ctx, nil, 0)
	if err == nil {
		t.Fatal("expected error for nil job")
	}
}

func TestWorkerConsumesJobMessage(t *testing.T) {
	requireRedis(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	q, err := NewAsyncQueue(newTestRedisDriver(client, "{kit-worker-job}", 1, 1, []int{1}, 0), "{kit-worker-job}",
		WithQueuePopTimeout(1),
		WithQueueHandleTimeout(1),
		WithQueueRetrySeconds([]int{1}),
		WithQueueMessageTTL(0),
		WithQueueMaxAttempts(3),
		WithQueueLogger(logger.Default),
	)
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
	if _, err := q.PushTask(ctx, job, 0); err != nil {
		t.Fatal(err)
	}

	called := 0
	var receivedJob *testEmailJob
	cfg := &Config{
		Queues: map[string]QueueConfig{
			"test": {
				Driver:        "redis",
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
	manager, err := NewManager(cfg, serveMux, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager.RegisterDriver("redis", newTestRedisDriver(client, "{kit-worker-job}", 1, 1, []int{1}, 0))
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

func TestQueueCancel(t *testing.T) {
	requireRedis(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	q, err := NewAsyncQueue(newTestRedisDriver(client, "test-cancel-job", 1, 1, []int{1}, 120), "test-cancel-job",
		WithQueuePopTimeout(1),
		WithQueueHandleTimeout(1),
		WithQueueRetrySeconds([]int{1}),
		WithQueueMessageTTL(120),
		WithQueueMaxAttempts(3),
		WithQueueLogger(logger.Default),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()
	job := &testEmailJob{
		To:      "cancel@example.com",
		Subject: "Cancel Job",
		Body:    "This job will be canceled before dispatch",
	}

	messageID, err := q.PushTask(ctx, job, 10)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := q.Cancel(ctx, messageID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected cancel by id success")
	}

	info, err := q.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Delayed != 0 {
		t.Fatalf("expected 0 delayed jobs after cancel, got %d", info.Delayed)
	}

	msg, err := q.GetMessage(ctx, messageID)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Status != core.StatusCanceled {
		t.Fatalf("expected canceled status, got %s", msg.Status)
	}
}

func TestQueueCancelWaitingReturnsError(t *testing.T) {
	requireRedis(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	q, err := NewAsyncQueue(newTestRedisDriver(client, "test-cancel-waiting", 1, 1, []int{1}, 120), "test-cancel-waiting",
		WithQueuePopTimeout(1),
		WithQueueHandleTimeout(1),
		WithQueueRetrySeconds([]int{1}),
		WithQueueMessageTTL(120),
		WithQueueMaxAttempts(3),
		WithQueueLogger(logger.Default),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()
	job := &testEmailJob{
		To:      "waiting@example.com",
		Subject: "Waiting Job",
		Body:    "This job is already waiting for dispatch",
	}

	messageID, err := q.PushTask(ctx, job, 0)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := q.Cancel(ctx, messageID)
	if !errors.Is(err, qpkg.ErrMessageAlreadyReadyForDispatch) {
		t.Fatalf("expected ErrMessageAlreadyReadyForDispatch, got %v", err)
	}
	if ok {
		t.Fatal("expected cancel false for waiting message")
	}
}

func TestQueueCancelReservedReturnsError(t *testing.T) {
	requireRedis(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	q, err := NewAsyncQueue(newTestRedisDriver(client, "test-cancel-reserved", 1, 1, []int{1}, 120), "test-cancel-reserved",
		WithQueuePopTimeout(1),
		WithQueueHandleTimeout(1),
		WithQueueRetrySeconds([]int{1}),
		WithQueueMessageTTL(120),
		WithQueueMaxAttempts(3),
		WithQueueLogger(logger.Default),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()
	job := &testEmailJob{
		To:      "reserved@example.com",
		Subject: "Reserved Job",
		Body:    "This job is already in execution",
	}

	messageID, err := q.PushTask(ctx, job, 0)
	if err != nil {
		t.Fatal(err)
	}

	poppedID, _, err := q.driver.Pop(ctx, q.channel, q.PopTimeout, q.handleTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if poppedID != messageID {
		t.Fatalf("expected popped id %s, got %s", messageID, poppedID)
	}

	ok, err := q.Cancel(ctx, messageID)
	if !errors.Is(err, qpkg.ErrMessageAlreadyInExecution) {
		t.Fatalf("expected ErrMessageAlreadyInExecution, got %v", err)
	}
	if ok {
		t.Fatal("expected cancel false for reserved message")
	}
}
