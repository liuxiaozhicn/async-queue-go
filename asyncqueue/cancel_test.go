package asyncqueue

import (
	"context"
	"errors"
	"testing"

	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
	"github.com/liuxiaozhicn/async-queue-go/pkg/logger"
	qpkg "github.com/liuxiaozhicn/async-queue-go/pkg/queue"
	"github.com/redis/go-redis/v9"
)

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

	messageID, err := q.PushJob(ctx, job, 10)
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

	messageID, err := q.PushJob(ctx, job, 0)
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

	messageID, err := q.PushJob(ctx, job, 0)
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
