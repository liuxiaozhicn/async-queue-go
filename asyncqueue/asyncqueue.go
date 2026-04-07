package asyncqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
	"github.com/liuxiaozhicn/async-queue-go/pkg/logger"
	"github.com/liuxiaozhicn/async-queue-go/pkg/queue"
	"github.com/redis/go-redis/v9"
)

type Info struct {
	Waiting  int64 `json:"waiting"`
	Reserved int64 `json:"reserved"`
	Delayed  int64 `json:"delayed"`
	Timeout  int64 `json:"timeout"`
	Failed   int64 `json:"failed"`
}

type Queue struct {
	client               redis.UniversalClient
	driver               queue.Driver
	timeoutSeconds       int
	handleTimeoutSeconds int
	retrySeconds         []int
	maxAttempts          int // Maximum retry attempts from configuration
	name                 string
	logger               logger.Interface
}

// NewAsyncQueue creates a new async queue with external Redis client
func NewAsyncQueue(client redis.UniversalClient, channel string, popTimeout int, handleTimeout int, retrySeconds []int, maxAttempts int, name string, l logger.Interface) (*Queue, error) {
	if client == nil {
		return nil, errors.New("redis client cannot be nil")
	}

	// Test connection
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	driver := queue.NewRedisDriver(client, channel, popTimeout, handleTimeout, retrySeconds)
	return &Queue{client: client, driver: driver, handleTimeoutSeconds: handleTimeout, retrySeconds: retrySeconds, maxAttempts: maxAttempts, name: name, logger: l}, nil
}

// Close releases queue-local resources.
// It does NOT close the shared Redis client — the caller who created
// the client is responsible for closing it.
func (q *Queue) Close() error {
	return nil
}

func (q *Queue) PushJob(ctx context.Context, job Job, delaySeconds int) error {
	if job == nil {
		return fmt.Errorf("push: job must not be nil")
	}
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal job: %w", err)
	}
	return q.PushMessage(ctx, &core.Message{
		Payload:     data,
		MaxAttempts: q.maxAttempts, // Use configuration max_attempts instead of job's MaxAttempts
	}, delaySeconds)
}

func (q *Queue) PushMessage(ctx context.Context, m *core.Message, delaySeconds int) error {
	if q == nil || q.driver == nil {
		return errors.New("queue is nil")
	}
	if m == nil {
		return errors.New("message is nil")
	}

	if m.MaxAttempts <= 0 {
		m.MaxAttempts = 1
	}
	err := q.driver.Push(ctx, m, delaySeconds)
	if err != nil {
		q.logger.Warn(ctx, "[Producer:q:%s] PUSH|error payload:%s delay:%ds error:%v", q.name, m.Payload, delaySeconds, err)
		return err
	}

	q.logger.Info(ctx, "[Producer:q:%s] PUSH|payload:%s delay:%ds maxAttempts:%d", q.name, m.Payload, delaySeconds, m.MaxAttempts)
	return nil
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
func (q *Queue) DeleteMessage(ctx context.Context, m *core.Message) error {
	if q == nil || q.driver == nil {
		return errors.New("queue is nil")
	}
	if m == nil {
		return fmt.Errorf("message must not be nil")
	}

	// Call driver.Delete with core.Message
	err := q.driver.Delete(ctx, m)
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
	message := &core.Message{
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
