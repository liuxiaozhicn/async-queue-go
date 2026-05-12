package queue

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/pkg/clock"
	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
	"github.com/redis/go-redis/v9"
)

type RedisDriver struct {
	client   redis.UniversalClient
	clock    clock.Clock
	keyCache sync.Map
}

const maxMessageSequence = int64(999999999999)

func (d *RedisDriver) Ping(ctx context.Context) error {
	return d.client.Ping(ctx).Err()
}

func NewRedisDriver(client redis.UniversalClient, opts ...RedisDriverOption) *RedisDriver {
	driverOptions := defaultRedisDriverOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&driverOptions)
		}
	}

	return &RedisDriver{
		client: client,
		clock:  driverOptions.clock,
	}
}

func (d *RedisDriver) getKeys(channel string) Keys {
	if cachedKeys, ok := d.keyCache.Load(channel); ok {
		return cachedKeys.(Keys)
	}

	keys := NewKeys(channel)
	cachedKeys, _ := d.keyCache.LoadOrStore(channel, keys)
	return cachedKeys.(Keys)
}

// GenerateID creates a globally unique message id per channel using Redis INCR.
// Sequence rollover is handled atomically by Redis script with epoch bump.
// Final id is full md5 hex string: md5("<channel>:<epoch>:<seq>").
func (d *RedisDriver) GenerateID(ctx context.Context, channel string) (string, error) {
	keys := d.getKeys(channel)
	res, err := nextSeqScript.Run(
		ctx,
		d.client,
		[]string{keys.SequenceKey, keys.SequenceEpoch},
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

	payload := keys.Channel + ":" + epochStr + ":" + seqStr
	sum := md5.Sum([]byte(payload))
	return hex.EncodeToString(sum[:]), nil
}

func (d *RedisDriver) Push(ctx context.Context, channel string, m *core.Message, delaySeconds int, messageTTL int) error {
	if m == nil {
		return errors.New("message is nil")
	}
	if messageTTL < 0 {
		messageTTL = 0
	}
	keys := d.getKeys(channel)
	id, err := d.GenerateID(ctx, channel)
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
		[]string{keys.Waiting, keys.Delayed, keys.Message(m.ID)},
		m.ID, raw, mode, score, messageTTL,
	).Result()
	if err != nil {
		return err
	}
	_ = res
	return nil
}

func (d *RedisDriver) Get(ctx context.Context, channel string, id string) (*core.Message, error) {
	if id == "" {
		return nil, errors.New("id is empty")
	}
	return d.loadMessage(ctx, channel, id)
}

func (d *RedisDriver) Cancel(ctx context.Context, channel string, id string) (bool, error) {
	if id == "" {
		return false, errors.New("id is empty")
	}
	keys := d.getKeys(channel)
	res, err := cancelByIDScript.Run(
		ctx,
		d.client,
		[]string{keys.Waiting, keys.Reserved, keys.Delayed, keys.Message(id)},
		id, d.clock.Now().Unix(),
	).Result()
	if err != nil {
		return false, err
	}
	code := asIntResult(res)
	switch code {
	case 1:
		return true, nil
	case 0:
		return false, nil
	case -1:
		return false, ErrMessageAlreadyReadyForDispatch
	case -2:
		return false, ErrMessageAlreadyInExecution
	default:
		return false, fmt.Errorf("queue: unexpected cancel script result: %d", code)
	}
}

func (d *RedisDriver) Retry(ctx context.Context, channel string, id string, delaySeconds int) (bool, error) {
	if id == "" {
		return false, errors.New("id is empty")
	}
	keys := d.getKeys(channel)
	msg, err := d.loadMessage(ctx, channel, id)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return false, nil
		}
		return false, err
	}
	if delaySeconds <= 0 {
		msg.SetStatus(core.StatusWaiting)
	} else {
		msg.SetStatus(core.StatusDelayed)
	}
	if msg.Attempts < msg.MaxAttempts {
		msg.Attempts++
	}
	raw, err := msg.Encode()
	if err != nil {
		return false, err
	}
	score := d.clock.Now().Unix() + int64(delaySeconds)
	mode := "delayed"
	if delaySeconds <= 0 {
		mode = "waiting"
	}
	res, err := retryByIDScript.Run(
		ctx,
		d.client,
		[]string{
			keys.Waiting, keys.Reserved, keys.Delayed,
			keys.Timeout, keys.Failed, keys.Message(id),
		},
		id, score, raw, mode,
	).Result()
	if err != nil {
		return false, err
	}
	return asBoolResult(res), nil
}

// ForwardMessages forwards due delayed messages to waiting and expired reserved messages to timeout.
func (d *RedisDriver) ForwardMessages(ctx context.Context, channel string) (forwardedDelayed int64, forwardedTimeout int64, err error) {
	keys := d.getKeys(channel)
	now := d.clock.Now().Unix()

	forwardedDelayed, err = moveScript.Run(
		ctx,
		d.client,
		[]string{keys.Delayed, keys.Waiting, keys.MessagePrefix},
		now, string(core.StatusWaiting),
	).Int64()
	if err != nil {
		return 0, 0, err
	}

	forwardedTimeout, err = moveTimeoutScript.Run(
		ctx,
		d.client,
		[]string{keys.Reserved, keys.Timeout, keys.MessagePrefix},
		now,
	).Int64()
	if err != nil {
		return forwardedDelayed, 0, err
	}

	return forwardedDelayed, forwardedTimeout, nil
}

func (d *RedisDriver) Pop(ctx context.Context, channel string, popTimeout time.Duration, handleTimeout time.Duration) (string, *core.Message, error) {
	if popTimeout <= 0 {
		popTimeout = time.Second
	}
	if handleTimeout <= 0 {
		handleTimeout = 10 * time.Second
	}
	keys := d.getKeys(channel)

	nowTs := d.clock.Now()
	now := nowTs.Unix()
	deadline := nowTs.Add(handleTimeout).Unix()
	res, err := popScript.Run(
		ctx,
		d.client,
		[]string{keys.Waiting, keys.Reserved, keys.MessagePrefix},
		now, deadline,
	).Result()
	if errors.Is(err, redis.Nil) || res == nil {
		timer := time.NewTimer(popTimeout)
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

	msg, err := d.loadMessage(ctx, channel, id)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			_ = d.client.ZRem(ctx, keys.Reserved, id).Err()
			return "", nil, nil
		}
		return "", nil, err
	}
	return id, msg, nil
}

func (d *RedisDriver) Ack(ctx context.Context, channel string, messageID string) error {
	keys := d.getKeys(channel)
	_, err := ackScript.Run(
		ctx,
		d.client,
		[]string{keys.Reserved, keys.Message(messageID)},
		messageID, d.clock.Now().Unix(),
	).Result()
	return err
}

func (d *RedisDriver) Fail(ctx context.Context, channel string, messageID string) error {
	keys := d.getKeys(channel)
	_, err := failScript.Run(
		ctx,
		d.client,
		[]string{keys.Reserved, keys.Failed, keys.Message(messageID)},
		messageID, d.clock.Now().Unix(),
	).Result()
	return err
}

func (d *RedisDriver) Drop(ctx context.Context, channel string, messageID string) error {
	keys := d.getKeys(channel)
	_, err := dropScript.Run(
		ctx,
		d.client,
		[]string{keys.Reserved, keys.Message(messageID)},
		messageID, d.clock.Now().Unix(),
	).Result()
	return err
}

func (d *RedisDriver) Requeue(ctx context.Context, channel string, messageID string) error {
	keys := d.getKeys(channel)
	_, err := requeueScript.Run(
		ctx,
		d.client,
		[]string{keys.Reserved, keys.Waiting, keys.Message(messageID)},
		messageID, d.clock.Now().Unix(),
	).Result()
	return err
}

func (d *RedisDriver) Reload(ctx context.Context, channel string, queue string) (int, error) {
	keys := d.getKeys(channel)
	source := keys.Failed
	skip := "0"
	if queue != "" {
		if queue != "timeout" && queue != "failed" {
			return 0, fmt.Errorf("queue %s is not supported", queue)
		}
		k, _ := keys.Get(queue)
		source = k
		if queue == "timeout" {
			skip = "1"
		}
	}

	total := 0
	for {
		res, err := reloadScript.Run(
			ctx,
			d.client,
			[]string{source, keys.Waiting, keys.MessagePrefix},
			d.clock.Now().Unix(), skip,
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

func (d *RedisDriver) Flush(ctx context.Context, channel string, queue string) error {
	keys := d.getKeys(channel)
	key := keys.Failed
	if queue != "" {
		k, err := keys.Get(queue)
		if err != nil {
			return err
		}
		key = k
	}
	return d.client.Del(ctx, key).Err()
}

func (d *RedisDriver) Info(ctx context.Context, channel string) (Info, error) {
	keys := d.getKeys(channel)
	waiting, err := d.client.LLen(ctx, keys.Waiting).Result()
	if err != nil {
		return Info{}, err
	}
	reserved, err := d.client.ZCard(ctx, keys.Reserved).Result()
	if err != nil {
		return Info{}, err
	}
	delayed, err := d.client.ZCard(ctx, keys.Delayed).Result()
	if err != nil {
		return Info{}, err
	}
	timeout, err := d.client.LLen(ctx, keys.Timeout).Result()
	if err != nil {
		return Info{}, err
	}
	failed, err := d.client.LLen(ctx, keys.Failed).Result()
	if err != nil {
		return Info{}, err
	}
	return Info{Waiting: waiting, Reserved: reserved, Delayed: delayed, Timeout: timeout, Failed: failed}, nil
}

func (d *RedisDriver) loadMessage(ctx context.Context, channel string, id string) (*core.Message, error) {
	keys := d.getKeys(channel)
	raw, err := d.client.Get(ctx, keys.Message(id)).Result()
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
