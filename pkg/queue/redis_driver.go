package queue

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/pkg/clock"
	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
	"github.com/redis/go-redis/v9"
)

type RedisDriver struct {
	client        redis.UniversalClient
	keys          Keys
	timeout       time.Duration
	handleTimeout time.Duration
	retrySeconds  []int
	clock         clock.Clock
}

func NewRedisDriver(client redis.UniversalClient, channel string, timeoutSeconds int, handleTimeoutSeconds int, retrySeconds []int) *RedisDriver {
	return NewRedisDriverWithClock(client, channel, timeoutSeconds, handleTimeoutSeconds, retrySeconds, clock.RealClock{})
}

func NewRedisDriverWithClock(client redis.UniversalClient, channel string, timeoutSeconds int, handleTimeoutSeconds int, retrySeconds []int, c clock.Clock) *RedisDriver {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 2
	}
	if handleTimeoutSeconds <= 0 {
		handleTimeoutSeconds = 10
	}
	if c == nil {
		c = clock.RealClock{}
	}
	return &RedisDriver{
		client:        client,
		keys:          NewKeys(channel),
		timeout:       time.Duration(timeoutSeconds) * time.Second,
		handleTimeout: time.Duration(handleTimeoutSeconds) * time.Second,
		retrySeconds:  retrySeconds,
		clock:         c,
	}
}

func (d *RedisDriver) Push(ctx context.Context, m *core.Message, delaySeconds int) error {
	data, err := m.Encode()
	if err != nil {
		return err
	}
	if delaySeconds == 0 {
		return d.client.LPush(ctx, d.keys.Waiting, data).Err()
	}
	score := float64(d.clock.Now().Unix() + int64(delaySeconds))
	return d.client.ZAdd(ctx, d.keys.Delayed, redis.Z{Score: score, Member: data}).Err()
}

func (d *RedisDriver) Delete(ctx context.Context, m *core.Message) error {
	data, err := m.Encode()
	if err != nil {
		return err
	}
	return d.client.ZRem(ctx, d.keys.Delayed, data).Err()
}

func (d *RedisDriver) Pop(ctx context.Context) (string, *core.Message, error) {
	if err := d.move(ctx, d.keys.Delayed, d.keys.Waiting); err != nil {
		return "", nil, err
	}
	if err := d.move(ctx, d.keys.Reserved, d.keys.Timeout); err != nil {
		return "", nil, err
	}

	res, err := d.client.BRPop(ctx, d.timeout, d.keys.Waiting).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	if len(res) < 2 {
		return "", nil, nil
	}
	data := res[1]
	if err := d.client.ZAdd(ctx, d.keys.Reserved, redis.Z{Score: float64(d.clock.Now().Add(d.handleTimeout).Unix()), Member: data}).Err(); err != nil {
		return "", nil, err
	}
	msg, err := core.DecodeMessage(data)
	if err != nil {
		return "", nil, nil
	}
	return data, msg, nil
}

// Remove removes data from the reserved queue. Used by RETRY/REQUEUE/DROP
// and internally by Ack/Fail. Mirrors PHP's protected remove() method.
func (d *RedisDriver) Remove(ctx context.Context, data string) error {
	return d.client.ZRem(ctx, d.keys.Reserved, data).Err()
}

// Ack acknowledges successful processing by removing from reserved queue.
func (d *RedisDriver) Ack(ctx context.Context, data string) error {
	return d.Remove(ctx, data)
}

// Fail removes from reserved queue and pushes to failed queue.
func (d *RedisDriver) Fail(ctx context.Context, data string) error {
	if err := d.Remove(ctx, data); err != nil {
		return err
	}
	return d.client.LPush(ctx, d.keys.Failed, data).Err()
}

func (d *RedisDriver) Requeue(ctx context.Context, data string) error {
	return d.client.LPush(ctx, d.keys.Waiting, data).Err()
}

func (d *RedisDriver) Retry(ctx context.Context, m *core.Message) error {
	data, err := m.Encode()
	if err != nil {
		return err
	}
	delay := core.RetrySeconds(d.retrySeconds, m.Attempts)
	score := float64(d.clock.Now().Unix() + int64(delay))
	return d.client.ZAdd(ctx, d.keys.Delayed, redis.Z{Score: score, Member: data}).Err()
}

func (d *RedisDriver) Reload(ctx context.Context, queue string) (int, error) {
	source := d.keys.Failed
	if queue != "" {
		if queue != "timeout" && queue != "failed" {
			return 0, fmt.Errorf("queue %s is not supported", queue)
		}
		k, _ := d.keys.Get(queue)
		source = k
	}

	n := 0
	for {
		_, err := d.client.RPopLPush(ctx, source, d.keys.Waiting).Result()
		if errors.Is(err, redis.Nil) {
			break
		}
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (d *RedisDriver) Flush(ctx context.Context, queue string) error {
	key := d.keys.Failed
	if queue != "" {
		k, err := d.keys.Get(queue)
		if err != nil {
			return err
		}
		key = k
	}
	return d.client.Del(ctx, key).Err()
}

func (d *RedisDriver) Info(ctx context.Context) (Info, error) {
	waiting, err := d.client.LLen(ctx, d.keys.Waiting).Result()
	if err != nil {
		return Info{}, err
	}
	reserved, err := d.client.ZCard(ctx, d.keys.Reserved).Result()
	if err != nil {
		return Info{}, err
	}
	delayed, err := d.client.ZCard(ctx, d.keys.Delayed).Result()
	if err != nil {
		return Info{}, err
	}
	timeout, err := d.client.LLen(ctx, d.keys.Timeout).Result()
	if err != nil {
		return Info{}, err
	}
	failed, err := d.client.LLen(ctx, d.keys.Failed).Result()
	if err != nil {
		return Info{}, err
	}
	return Info{Waiting: waiting, Reserved: reserved, Delayed: delayed, Timeout: timeout, Failed: failed}, nil
}

func (d *RedisDriver) move(ctx context.Context, from string, to string) error {
	now := fmt.Sprintf("%d", d.clock.Now().Unix())
	expired, err := d.client.ZRevRangeByScore(ctx, from, &redis.ZRangeBy{Max: now, Min: "-inf", Offset: 0, Count: 100}).Result()
	if err != nil {
		return err
	}
	for _, job := range expired {
		removed, err := d.client.ZRem(ctx, from, job).Result()
		if err != nil {
			return err
		}
		if removed > 0 {
			if err := d.client.LPush(ctx, to, job).Err(); err != nil {
				return err
			}
		}
	}
	return nil
}
