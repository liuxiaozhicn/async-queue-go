package asyncqueue

import (
	"context"
	"encoding/json"
	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
	"github.com/liuxiaozhicn/async-queue-go/pkg/logger"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestQueueDeleteJob(t *testing.T) {
	requireRedis(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	q, err := NewAsyncQueue(client, "test-delete-job", 1, 1, []int{1}, 0, 3, "", logger.Default)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()

	// Create and push a test job
	job := &testEmailJob{
		To:      "test@example.com",
		Subject: "Test Delete",
		Body:    "This job will be deleted",
	}

	// Push and capture message_id for id-based deletion.
	messageID, err := q.PushJob(ctx, job, 10)
	if err != nil { // 10 second delay
		t.Fatal(err)
	}

	// Verify job was pushed to delayed queue
	info, err := q.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Delayed != 1 {
		t.Fatalf("expected 1 delayed job, got %d", info.Delayed)
	}

	// Delete by message_id.
	ok, err := q.DeleteByID(ctx, messageID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected delete by id success")
	}

	// Verify job was deleted from delayed queue
	info, err = q.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Delayed != 0 {
		t.Fatalf("expected 0 delayed jobs after deletion, got %d", info.Delayed)
	}
}

func TestQueueDeleteJobNilJob(t *testing.T) {
	requireRedis(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	q, err := NewAsyncQueue(client, "test-delete-nil", 1, 1, []int{1}, 0, 3, "", logger.Default)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()

	// Try to delete nil job
	err = q.DeleteJob(ctx, nil)
	if err == nil {
		t.Fatal("expected error when deleting nil job")
	}
	if err.Error() != "job must not be nil" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestQueueDeleteJobRequiresMessageID(t *testing.T) {
	requireRedis(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	q, err := NewAsyncQueue(client, "test-delete-job-requires-id", 1, 1, []int{1}, 0, 3, "", logger.Default)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()
	job := &testEmailJob{To: "a@example.com", Subject: "s", Body: "b"}
	err = q.DeleteJob(ctx, job)
	if err == nil {
		t.Fatal("expected error when deleting job without message id")
	}
}

func TestQueueDeleteMessage(t *testing.T) {
	requireRedis(t)

	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	q, err := NewAsyncQueue(client, "test-delete-message", 1, 1, []int{1}, 0, 3, "", logger.Default)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()

	// Create test message
	payload, _ := json.Marshal(map[string]string{"test": "delete message"})
	message := &core.Message{
		Payload:     payload,
		MaxAttempts: 3,
		Attempts:    0,
	}

	// Push the message with delay so it goes to delayed queue (can be deleted)
	if _, err := q.PushMessage(ctx, message, 10); err != nil { // 10 second delay
		t.Fatal(err)
	}

	// Verify message was pushed to delayed queue
	info, err := q.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Delayed != 1 {
		t.Fatalf("expected 1 delayed message, got %d", info.Delayed)
	}

	// Delete the message
	err = q.DeleteMessage(ctx, message)
	if err != nil {
		t.Fatal(err)
	}

	// Verify message was deleted from delayed queue
	info, err = q.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Delayed != 0 {
		t.Fatalf("expected 0 delayed messages after deletion, got %d", info.Delayed)
	}
}

func TestQueueDeleteMessageNilMessage(t *testing.T) {
	requireRedis(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	q, err := NewAsyncQueue(client, "test-delete-nil-msg", 1, 1, []int{1}, 0, 3, "", logger.Default)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()

	// Try to delete nil message
	err = q.DeleteMessage(ctx, nil)
	if err == nil {
		t.Fatal("expected error when deleting nil message")
	}
	if err.Error() != "message must not be nil" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestQueueDeleteJobWithDelay(t *testing.T) {
	requireRedis(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	q, err := NewAsyncQueue(client, "test-delete-delayed", 1, 1, []int{1}, 0, 3, "", logger.Default)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()

	// Create and push a delayed job
	job := &testEmailJob{
		To:      "delayed@example.com",
		Subject: "Delayed Job",
		Body:    "This delayed job will be deleted",
	}

	messageID, err := q.PushJob(ctx, job, 10)
	if err != nil {
		t.Fatal(err)
	}

	// Verify job was pushed to delayed queue
	info, err := q.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Delayed != 1 {
		t.Fatalf("expected 1 delayed job, got %d", info.Delayed)
	}

	ok, err := q.DeleteByID(ctx, messageID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected delete by id success")
	}

	// Verify delayed job was deleted
	info, err = q.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Delayed != 0 {
		t.Fatalf("expected 0 delayed jobs after deletion, got %d", info.Delayed)
	}
}

func TestQueueDeleteNonExistentJob(t *testing.T) {
	requireRedis(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	q, err := NewAsyncQueue(client, "test-delete-nonexistent", 1, 1, []int{1}, 0, 3, "", logger.Default)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()

	// Delete a message_id that does not exist.
	ok, err := q.DeleteByID(ctx, "not-exists-id")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected delete false for non-existent id")
	}
}

func TestQueueDeleteMultipleJobs(t *testing.T) {
	requireRedis(t)
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	q, err := NewAsyncQueue(client, "test-delete-multiple", 1, 1, []int{1}, 0, 3, "", logger.Default)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()

	// Create and push multiple jobs
	job1 := &testEmailJob{To: "user1@example.com", Subject: "Job 1", Body: "First job"}
	job2 := &testEmailJob{To: "user2@example.com", Subject: "Job 2", Body: "Second job"}
	job3 := &testEmailJob{To: "user3@example.com", Subject: "Job 3", Body: "Third job"}

	// Push all jobs
	if _, err := q.PushJob(ctx, job1, 0); err != nil {
		t.Fatal(err)
	}
	job2ID, err := q.PushJob(ctx, job2, 5) // delayed
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.PushJob(ctx, job3, 0); err != nil {
		t.Fatal(err)
	}

	// Verify jobs were pushed
	info, err := q.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Waiting != 2 || info.Delayed != 1 {
		t.Fatalf("expected 2 waiting and 1 delayed job, got %d waiting and %d delayed", info.Waiting, info.Delayed)
	}

	// Delete job2 (the delayed one) by id
	ok, err := q.DeleteByID(ctx, job2ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected delete by id success")
	}

	// Verify only job2 was deleted
	info, err = q.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Waiting != 2 || info.Delayed != 0 {
		t.Fatalf("expected 2 waiting and 0 delayed jobs after deletion, got %d waiting and %d delayed", info.Waiting, info.Delayed)
	}
}
