package queue

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/pkg/clock"
	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
	"github.com/redis/go-redis/v9"
)

type RedisDriver struct {
	client        redis.UniversalClient
	keys          Keys
	PopTimeout    time.Duration
	handleTimeout time.Duration
	retrySeconds  []int
	messageTTL    int
	clock         clock.Clock
}

const maxMessageSequence = int64(999999999999)

func NewRedisDriver(client redis.UniversalClient, channel string, popTimeout int, handleTimeout int, retrySeconds []int) *RedisDriver {
	return NewRedisDriverWithClock(client, channel, popTimeout, handleTimeout, retrySeconds, clock.RealClock{})
}

func NewRedisDriverWithClock(client redis.UniversalClient, channel string, popTimeout int, handleTimeout int, retrySeconds []int, c clock.Clock) *RedisDriver {
	if popTimeout <= 0 {
		popTimeout = 1
	}
	if handleTimeout <= 0 {
		handleTimeout = 10
	}
	if c == nil {
		c = clock.RealClock{}
	}
	return &RedisDriver{
		client:        client,
		keys:          NewKeys(channel),
		PopTimeout:    time.Duration(popTimeout) * time.Second,
		handleTimeout: time.Duration(handleTimeout) * time.Second,
		retrySeconds:  retrySeconds,
		messageTTL:    0,
		clock:         c,
	}
}

func (d *RedisDriver) SetMessageTTL(seconds int) {
	if seconds < 0 {
		seconds = 0
	}
	d.messageTTL = seconds
}

// GenerateID creates a globally unique message id per channel using Redis INCR.
// Sequence rollover is handled atomically by Redis script with epoch bump.
// Final id is full md5 hex string: md5("<channel>:<epoch>:<seq>").
func (d *RedisDriver) GenerateID(ctx context.Context) (string, error) {
	res, err := nextSeqScript.Run(
		ctx,
		d.client,
		[]string{d.keys.SequenceKey, d.keys.SequenceEpoch},
		maxMessageSequence,
	).Result()
	if err != nil {
		return "", err
	}

	items, ok := res.([]interface{})
	if !ok || len(items) != 2 {
		return "", fmt.Errorf("unexpected sequence script result: %T", res)
	}
	seqStr, ok := items[0].(string)
	if !ok {
		return "", fmt.Errorf("invalid sequence value type: %T", items[0])
	}
	epochStr, ok := items[1].(string)
	if !ok {
		return "", fmt.Errorf("invalid sequence epoch type: %T", items[1])
	}

	payload := d.keys.Channel + ":" + epochStr + ":" + seqStr
	sum := md5.Sum([]byte(payload))
	return hex.EncodeToString(sum[:]), nil
}

func (d *RedisDriver) Push(ctx context.Context, m *core.Message, delaySeconds int) error {
	if m == nil {
		return errors.New("message is nil")
	}
	id, err := d.GenerateID(ctx)
	if err != nil {
		return err
	}
	m.ID = id
	mode := "waiting"
	score := int64(0)
	if delaySeconds == 0 {
		m.SetStatus(core.StatusWaiting)
	} else {
		mode = "delayed"
		score = d.clock.Now().Unix() + int64(delaySeconds)
		m.SetStatus(core.StatusDelayed)
	}
	raw, err := m.Encode()
	if err != nil {
		return err
	}
	res, err := pushScript.Run(
		ctx,
		d.client,
		[]string{d.keys.Waiting, d.keys.Delayed, d.keys.Message(m.ID)},
		m.ID, raw, mode, score, d.messageTTL,
	).Result()
	if err != nil {
		return err
	}
	_ = res
	return nil
}

func (d *RedisDriver) Delete(ctx context.Context, m *core.Message) error {
	if m == nil {
		return errors.New("message is nil")
	}
	if m.ID == "" {
		return errors.New("message id is empty")
	}
	_, err := d.DeleteMessage(ctx, m.ID)
	return err
}

func (d *RedisDriver) GetMessage(ctx context.Context, id string) (*core.Message, error) {
	if id == "" {
		return nil, errors.New("id is empty")
	}
	return d.loadMessage(ctx, id)
}

func (d *RedisDriver) DeleteMessage(ctx context.Context, id string) (bool, error) {
	if id == "" {
		return false, errors.New("id is empty")
	}
	res, err := deleteByIDScript.Run(
		ctx,
		d.client,
		[]string{
			d.keys.Waiting, d.keys.Reserved, d.keys.Delayed,
			d.keys.Timeout, d.keys.Failed, d.keys.Message(id),
		},
		id,
	).Result()
	if err != nil {
		return false, err
	}
	return asBoolResult(res), nil
}

func (d *RedisDriver) RetryMessage(ctx context.Context, id string, delaySeconds int) (bool, error) {
	if id == "" {
		return false, errors.New("id is empty")
	}
	msg, err := d.loadMessage(ctx, id)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return false, nil
		}
		return false, err
	}
	msg.SetStatus(core.StatusDelayed)
	if msg.Attempts < msg.MaxAttempts {
		msg.Attempts++
	}
	raw, err := msg.Encode()
	if err != nil {
		return false, err
	}
	score := d.clock.Now().Unix() + int64(delaySeconds)
	res, err := retryByIDScript.Run(
		ctx,
		d.client,
		[]string{
			d.keys.Waiting, d.keys.Reserved, d.keys.Delayed,
			d.keys.Timeout, d.keys.Failed, d.keys.Message(id),
		},
		id, score, raw,
	).Result()
	if err != nil {
		return false, err
	}
	return asBoolResult(res), nil
}

// ForwardMessages forwards due delayed messages to waiting and expired reserved messages to timeout.
func (d *RedisDriver) ForwardMessages(ctx context.Context) (forwardedDelayed int64, forwardedTimeout int64, err error) {
	now := d.clock.Now().Unix()

	forwardedDelayed, err = moveScript.Run(
		ctx,
		d.client,
		[]string{d.keys.Delayed, d.keys.Waiting, d.keys.MessagePrefix},
		now, string(core.StatusWaiting),
	).Int64()
	if err != nil {
		return 0, 0, err
	}

	forwardedTimeout, err = moveScript.Run(
		ctx,
		d.client,
		[]string{d.keys.Reserved, d.keys.Timeout, d.keys.MessagePrefix},
		now, string(core.StatusTimeout),
	).Int64()
	if err != nil {
		return forwardedDelayed, 0, err
	}

	return forwardedDelayed, forwardedTimeout, nil
}

func (d *RedisDriver) Pop(ctx context.Context) (string, *core.Message, error) {
	nowTs := d.clock.Now()
	now := nowTs.Unix()
	deadline := nowTs.Add(d.handleTimeout).Unix()
	res, err := popScript.Run(
		ctx,
		d.client,
		[]string{d.keys.Waiting, d.keys.Reserved, d.keys.MessagePrefix},
		now, deadline,
	).Result()
	if errors.Is(err, redis.Nil) || res == nil {
		timer := time.NewTimer(d.PopTimeout)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return "", nil, ctx.Err()
		case <-timer.C:
			return "", nil, nil
		}
	}
	if err != nil {
		return "", nil, err
	}
	id, ok := res.(string)
	if !ok || id == "" {
		return "", nil, nil
	}

	msg, err := d.loadMessage(ctx, id)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			_ = d.Remove(ctx, id)
			return "", nil, nil
		}
		return "", nil, err
	}
	return id, msg, nil
}

func (d *RedisDriver) Remove(ctx context.Context, messageID string) error {
	return d.client.ZRem(ctx, d.keys.Reserved, messageID).Err()
}

func (d *RedisDriver) Ack(ctx context.Context, messageID string) error {
	_, err := ackScript.Run(
		ctx,
		d.client,
		[]string{d.keys.Reserved, d.keys.Message(messageID)},
		messageID, d.clock.Now().Unix(),
	).Result()
	return err
}

func (d *RedisDriver) Fail(ctx context.Context, messageID string) error {
	_, err := failScript.Run(
		ctx,
		d.client,
		[]string{d.keys.Reserved, d.keys.Failed, d.keys.Message(messageID)},
		messageID, d.clock.Now().Unix(),
	).Result()
	return err
}

func (d *RedisDriver) Requeue(ctx context.Context, messageID string) error {
	_, err := requeueScript.Run(
		ctx,
		d.client,
		[]string{d.keys.Reserved, d.keys.Waiting, d.keys.Message(messageID)},
		messageID, d.clock.Now().Unix(),
	).Result()
	return err
}

func (d *RedisDriver) Retry(ctx context.Context, m *core.Message) error {
	if m == nil {
		return errors.New("message is nil")
	}
	if m.ID == "" {
		return errors.New("message id is empty")
	}
	m.SetStatus(core.StatusDelayed)
	raw, err := m.Encode()
	if err != nil {
		return err
	}
	score := d.clock.Now().Unix() + int64(core.RetrySeconds(d.retrySeconds, m.Attempts))
	_, err = retryScript.Run(
		ctx,
		d.client,
		[]string{d.keys.Reserved, d.keys.Delayed, d.keys.Message(m.ID)},
		m.ID, score, raw,
	).Result()
	return err
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

	total := 0
	for {
		res, err := reloadScript.Run(
			ctx,
			d.client,
			[]string{source, d.keys.Waiting, d.keys.MessagePrefix},
			d.clock.Now().Unix(),
		).Result()
		if err != nil {
			return total, err
		}
		moved := asIntResult(res)
		if moved <= 0 {
			return total, nil
		}
		total += moved
	}
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

func (d *RedisDriver) storeMessage(ctx context.Context, m *core.Message) error {
	if m == nil || m.ID == "" {
		return errors.New("invalid message")
	}
	raw, err := m.Encode()
	if err != nil {
		return err
	}
	return d.client.Set(ctx, d.keys.Message(m.ID), raw, 0).Err()
}

func (d *RedisDriver) loadMessage(ctx context.Context, id string) (*core.Message, error) {
	raw, err := d.client.Get(ctx, d.keys.Message(id)).Result()
	if err != nil {
		return nil, err
	}
	return core.DecodeMessage(raw)
}

func asBoolResult(v interface{}) bool {
	switch t := v.(type) {
	case int64:
		return t != 0
	case int:
		return t != 0
	case string:
		return t != "" && t != "0"
	default:
		return false
	}
}

func asIntResult(v interface{}) int {
	switch t := v.(type) {
	case int64:
		return int(t)
	case int:
		return t
	case string:
		n, _ := strconv.Atoi(t)
		return n
	default:
		return 0
	}
}
