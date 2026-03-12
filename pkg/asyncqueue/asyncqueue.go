package asyncqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/liuxiaozhicn/async-queue-go/internal/core"
	"github.com/liuxiaozhicn/async-queue-go/internal/queue"
	"github.com/redis/go-redis/v9"
)

type Result string

const (
	ACK     Result = Result(core.ACK)
	RETRY   Result = Result(core.RETRY)
	REQUEUE Result = Result(core.REQUEUE)
	DROP    Result = Result(core.DROP)
)

type Message struct {
	Payload     json.RawMessage `json:"payload"`
	Attempts    int             `json:"attempts"`
	MaxAttempts int             `json:"max_attempts"`
}

type Info struct {
	Waiting  int64 `json:"waiting"`
	Reserved int64 `json:"reserved"`
	Delayed  int64 `json:"delayed"`
	Timeout  int64 `json:"timeout"`
	Failed   int64 `json:"failed"`
}

type Queue struct {
	client      redis.UniversalClient
	driver      queue.Driver
	maxAttempts int // Maximum retry attempts from configuration
}

// NewAsyncQueue creates a new async queue with external Redis client
func NewAsyncQueue(client redis.UniversalClient, channel string, timeoutSeconds int, handleTimeoutSeconds int, retrySeconds []int, maxAttempts int) (*Queue, error) {
	if client == nil {
		return nil, errors.New("redis client cannot be nil")
	}

	// Test connection
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	driver := queue.NewRedisDriver(client, channel, timeoutSeconds, handleTimeoutSeconds, retrySeconds)
	return &Queue{client: client, driver: driver, maxAttempts: maxAttempts}, nil
}

// NewAsyncQueueWithAddr creates a new async queue with Redis address (for backward compatibility)
func NewAsyncQueueWithAddr(redisAddr, channel string, timeoutSeconds int, handleTimeoutSeconds int, retrySeconds []int, maxAttempts int) (*Queue, error) {
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	return NewAsyncQueue(client, channel, timeoutSeconds, handleTimeoutSeconds, retrySeconds, maxAttempts)
}

func (q *Queue) Close() error {
	if q == nil || q.client == nil {
		return nil
	}
	return q.client.Close()
}

func (q *Queue) PushJob(ctx context.Context, job Job, delaySeconds int) error {
	if job == nil {
		return fmt.Errorf("push: job must not be nil")
	}
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal job: %w", err)
	}
	return q.PushMessage(ctx, &Message{
		Payload:     data,
		MaxAttempts: q.maxAttempts, // Use configuration max_attempts instead of job's MaxAttempts
	}, delaySeconds)
}

func (q *Queue) PushMessage(ctx context.Context, m *Message, delaySeconds int) error {
	if q == nil || q.driver == nil {
		return errors.New("queue is nil")
	}
	if m == nil {
		return errors.New("message is nil")
	}
	cm := &core.Message{Payload: m.Payload, Attempts: m.Attempts, MaxAttempts: m.MaxAttempts}
	if cm.MaxAttempts <= 0 {
		cm.MaxAttempts = 1
	}
	return q.driver.Push(ctx, cm, delaySeconds)
}

func (q *Queue) Info(ctx context.Context) (Info, error) {
	if q == nil || q.driver == nil {
		return Info{}, errors.New("queue is nil")
	}
	i, err := q.driver.Info(ctx)
	if err != nil {
		return Info{}, err
	}
	return Info{Waiting: i.Waiting, Reserved: i.Reserved, Delayed: i.Delayed, Timeout: i.Timeout, Failed: i.Failed}, nil
}

func (q *Queue) Reload(ctx context.Context, queueName string) (int, error) {
	if q == nil || q.driver == nil {
		return 0, errors.New("queue is nil")
	}
	return q.driver.Reload(ctx, queueName)
}

// DeleteMessage deletes a specific message from all queues
func (q *Queue) DeleteMessage(ctx context.Context, m *Message) error {
	if q == nil || q.driver == nil {
		return errors.New("queue is nil")
	}
	if m == nil {
		return fmt.Errorf("message must not be nil")
	}

	// Convert to core message
	cm := &core.Message{
		Payload:     m.Payload,
		Attempts:    m.Attempts,
		MaxAttempts: m.MaxAttempts,
	}

	// Call driver.Delete with core.Message
	err := q.driver.Delete(ctx, cm)
	if err != nil {
		return fmt.Errorf("delete message: %w", err)
	}

	// Since driver.Delete doesn't return count, we assume 1 if no error
	return nil
}

// DeleteJob deletes a specific job from all queues by converting it to a message first
func (q *Queue) DeleteJob(ctx context.Context, job Job) error {
	if job == nil {
		return fmt.Errorf("job must not be nil")
	}

	// Marshal job to get the data representation
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal job: %w", err)
	}

	// Create message from job data
	message := &Message{
		Payload:     data,
		MaxAttempts: q.maxAttempts,
		Attempts:    0,
	}

	// Use DeleteMessage to do the actual deletion
	return q.DeleteMessage(ctx, message)
}

func (q *Queue) Flush(ctx context.Context, queueName string) error {
	if q == nil || q.driver == nil {
		return errors.New("queue is nil")
	}
	return q.driver.Flush(ctx, queueName)
}

type Handler func(context.Context, *Message) (Result, error)
