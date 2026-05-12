package queue

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
	"github.com/redis/go-redis/v9"
)

type fakeClock struct {
	now time.Time
}

const testChannel = "test"

func (f *fakeClock) Now() time.Time {
	return f.now
}

func newDriverForTest(t *testing.T) (*RedisDriver, *fakeClock, func()) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		t.Skipf("redis not available: %v", err)
	}
	fc := &fakeClock{now: time.Unix(1700000000, 0)}
	d := NewRedisDriver(
		client,
		WithRedisDriverClock(fc),
	)
	cleanup := func() {
		_ = client.Close()
	}
	return d, fc, cleanup
}

// newDriverForTestWithClient creates a driver with external Redis client for testing
func newDriverForTestWithClient(t *testing.T, client redis.UniversalClient) (*RedisDriver, *fakeClock, func()) {
	t.Helper()
	fc := &fakeClock{now: time.Unix(1700000000, 0)}
	d := NewRedisDriver(
		client,
		WithRedisDriverClock(fc),
	)
	cleanup := func() {
		// Don't close external client, let caller handle it
	}
	return d, fc, cleanup
}

func mustMessage(t *testing.T, id int) *core.Message {
	t.Helper()
	b, _ := json.Marshal(map[string]int{"id": id})
	m := core.NewMessage(b, 2)
	m.ID = fmt.Sprintf("test-msg-%d", id)
	return m
}

func TestPushCancelInfo(t *testing.T) {
	d, _, done := newDriverForTest(t)
	defer done()
	ctx := context.Background()

	m1 := mustMessage(t, 1)
	if err := d.Push(ctx, testChannel, m1, 0, 0); err != nil {
		t.Fatal(err)
	}
	m2 := mustMessage(t, 2)
	if err := d.Push(ctx, testChannel, m2, 10, 0); err != nil {
		t.Fatal(err)
	}

	info, err := d.Info(ctx, testChannel)
	if err != nil {
		t.Fatal(err)
	}
	if info.Waiting != 1 || info.Delayed != 1 {
		t.Fatalf("unexpected info: %+v", info)
	}

	ok, err := d.Cancel(ctx, testChannel, m2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected cancel success")
	}
	info, _ = d.Info(ctx, testChannel)
	if info.Delayed != 0 {
		t.Fatalf("cancel failed, info=%+v", info)
	}
}

func TestPopAckFailMove(t *testing.T) {
	d, _, done := newDriverForTest(t)
	defer done()
	ctx := context.Background()

	m := mustMessage(t, 1)
	if err := d.Push(ctx, testChannel, m, 0, 0); err != nil {
		t.Fatal(err)
	}
	data, _, err := d.Pop(ctx, testChannel, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if data == "" {
		t.Fatal("expected data")
	}

	info, _ := d.Info(ctx, testChannel)
	if info.Reserved != 1 {
		t.Fatalf("expected reserved=1 got %+v", info)
	}

	if err := d.Ack(ctx, testChannel, data); err != nil {
		t.Fatal(err)
	}
	info, _ = d.Info(ctx, testChannel)
	if info.Reserved != 0 {
		t.Fatalf("expected reserved=0 got %+v", info)
	}

	if err := d.Push(ctx, testChannel, mustMessage(t, 2), 0, 0); err != nil {
		t.Fatal(err)
	}
	data2, _, err := d.Pop(ctx, testChannel, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Fail(ctx, testChannel, data2); err != nil {
		t.Fatal(err)
	}
	info, _ = d.Info(ctx, testChannel)
	if info.Failed != 1 || info.Reserved != 0 {
		t.Fatalf("unexpected fail info: %+v", info)
	}
}

func TestDropMarksMessageDropped(t *testing.T) {
	d, _, done := newDriverForTest(t)
	defer done()
	ctx := context.Background()

	m := mustMessage(t, 3)
	if err := d.Push(ctx, testChannel, m, 0, 120); err != nil {
		t.Fatal(err)
	}
	messageID, _, err := d.Pop(ctx, testChannel, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if messageID == "" {
		t.Fatal("expected message id")
	}
	if err := d.Drop(ctx, testChannel, messageID); err != nil {
		t.Fatal(err)
	}

	info, err := d.Info(ctx, testChannel)
	if err != nil {
		t.Fatal(err)
	}
	if info.Reserved != 0 {
		t.Fatalf("expected reserved=0 after drop, got %+v", info)
	}

	msg, err := d.Get(ctx, testChannel, messageID)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Status != core.StatusDropped {
		t.Fatalf("expected dropped status, got %s", msg.Status)
	}
}

func TestCancelByIDMarksMessageCanceled(t *testing.T) {
	d, _, done := newDriverForTest(t)
	defer done()
	ctx := context.Background()

	m := mustMessage(t, 4)
	if err := d.Push(ctx, testChannel, m, 10, 120); err != nil {
		t.Fatal(err)
	}
	ok, err := d.Cancel(ctx, testChannel, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected cancel success")
	}

	info, err := d.Info(ctx, testChannel)
	if err != nil {
		t.Fatal(err)
	}
	if info.Delayed != 0 {
		t.Fatalf("expected delayed=0 after cancel, got %+v", info)
	}

	msg, err := d.Get(ctx, testChannel, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Status != core.StatusCanceled {
		t.Fatalf("expected canceled status, got %s", msg.Status)
	}
}

func TestCancelByIDReservedReturnsInExecutionError(t *testing.T) {
	d, _, done := newDriverForTest(t)
	defer done()
	ctx := context.Background()

	m := mustMessage(t, 6)
	if err := d.Push(ctx, testChannel, m, 0, 120); err != nil {
		t.Fatal(err)
	}
	messageID, _, err := d.Pop(ctx, testChannel, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if messageID == "" {
		t.Fatal("expected message id")
	}

	ok, err := d.Cancel(ctx, testChannel, messageID)
	if !errors.Is(err, ErrMessageAlreadyInExecution) {
		t.Fatalf("expected ErrMessageAlreadyInExecution, got %v", err)
	}
	if ok {
		t.Fatal("expected cancel false for reserved message")
	}

	info, err := d.Info(ctx, testChannel)
	if err != nil {
		t.Fatal(err)
	}
	if info.Reserved != 1 {
		t.Fatalf("expected reserved=1 after cancel miss, got %+v", info)
	}
}

func TestCancelByIDWaitingReturnsReadyForDispatchError(t *testing.T) {
	d, _, done := newDriverForTest(t)
	defer done()
	ctx := context.Background()

	m := mustMessage(t, 7)
	if err := d.Push(ctx, testChannel, m, 0, 120); err != nil {
		t.Fatal(err)
	}

	ok, err := d.Cancel(ctx, testChannel, m.ID)
	if !errors.Is(err, ErrMessageAlreadyReadyForDispatch) {
		t.Fatalf("expected ErrMessageAlreadyReadyForDispatch, got %v", err)
	}
	if ok {
		t.Fatal("expected cancel false for waiting message")
	}

	info, err := d.Info(ctx, testChannel)
	if err != nil {
		t.Fatal(err)
	}
	if info.Waiting != 1 {
		t.Fatalf("expected waiting=1 after cancel reject, got %+v", info)
	}
}

func TestMoveDelayedAndReservedTimeoutAndReloadFlush(t *testing.T) {
	d, fc, done := newDriverForTest(t)
	defer done()
	ctx := context.Background()
	keys := NewKeys(testChannel)

	m := mustMessage(t, 5)
	raw, err := m.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.client.Set(ctx, keys.Message(m.ID), raw, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := d.client.ZAdd(ctx, keys.Delayed, redis.Z{Score: float64(fc.Now().Unix() - 1), Member: m.ID}).Err(); err != nil {
		t.Fatal(err)
	}

	forwardedDelayed, forwardedTimeout, err := d.ForwardMessages(ctx, testChannel)
	if err != nil {
		t.Fatal(err)
	}
	if forwardedDelayed == 0 {
		t.Fatal("expected delayed message moved to waiting")
	}
	if forwardedTimeout != 0 {
		t.Fatalf("expected no timeout move yet, got %d", forwardedTimeout)
	}

	if err := d.client.ZAdd(ctx, keys.Reserved, redis.Z{Score: float64(fc.Now().Unix() - 1), Member: m.ID}).Err(); err != nil {
		t.Fatal(err)
	}
	_, forwardedTimeout, err = d.ForwardMessages(ctx, testChannel)
	if err != nil {
		t.Fatal(err)
	}
	if forwardedTimeout == 0 {
		t.Fatal("expected expired reserved message moved to timeout")
	}
	info, _ := d.Info(ctx, testChannel)
	if info.Timeout == 0 {
		t.Fatalf("expected timeout to increase, info=%+v", info)
	}

	if _, err := d.Reload(ctx, testChannel, "bad"); err == nil {
		t.Fatal("expected reload invalid queue error")
	}

	n, err := d.Reload(ctx, testChannel, "timeout")
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected timeout messages to be reloaded")
	}

	if err := d.Flush(ctx, testChannel, "waiting"); err != nil {
		t.Fatal(err)
	}
	info, _ = d.Info(ctx, testChannel)
	if info.Waiting != 0 {
		t.Fatalf("expected waiting=0 after flush, got %+v", info)
	}
}

func TestRetryZeroDelayMovesToWaiting(t *testing.T) {
	d, _, done := newDriverForTest(t)
	defer done()
	ctx := context.Background()

	m := mustMessage(t, 55)
	if err := d.Push(ctx, testChannel, m, 0, 120); err != nil {
		t.Fatal(err)
	}
	messageID, _, err := d.Pop(ctx, testChannel, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if messageID == "" {
		t.Fatal("expected message id")
	}

	ok, err := d.Retry(ctx, testChannel, messageID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected retry success")
	}

	info, err := d.Info(ctx, testChannel)
	if err != nil {
		t.Fatal(err)
	}
	if info.Waiting != 1 || info.Delayed != 0 || info.Reserved != 0 {
		t.Fatalf("unexpected queue state after retry zero delay: %+v", info)
	}

	msg, err := d.Get(ctx, testChannel, messageID)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Status != core.StatusWaiting {
		t.Fatalf("expected waiting status after retry zero delay, got %s", msg.Status)
	}
}

func TestRetryPositiveDelayMovesToDelayed(t *testing.T) {
	d, _, done := newDriverForTest(t)
	defer done()
	ctx := context.Background()

	m := mustMessage(t, 56)
	if err := d.Push(ctx, testChannel, m, 0, 120); err != nil {
		t.Fatal(err)
	}
	messageID, _, err := d.Pop(ctx, testChannel, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if messageID == "" {
		t.Fatal("expected message id")
	}

	ok, err := d.Retry(ctx, testChannel, messageID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected retry success")
	}

	info, err := d.Info(ctx, testChannel)
	if err != nil {
		t.Fatal(err)
	}
	if info.Delayed != 1 || info.Waiting != 0 || info.Reserved != 0 {
		t.Fatalf("unexpected queue state after retry positive delay: %+v", info)
	}

	msg, err := d.Get(ctx, testChannel, messageID)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Status != core.StatusDelayed {
		t.Fatalf("expected delayed status after retry positive delay, got %s", msg.Status)
	}
}

func TestMessageTTLPreservedAcrossStatusUpdates(t *testing.T) {
	d, _, done := newDriverForTest(t)
	defer done()
	ctx := context.Background()
	keys := NewKeys(testChannel)

	d = NewRedisDriver(
		d.client,
		WithRedisDriverClock(d.clock),
	)
	m := mustMessage(t, 42)
	if err := d.Push(ctx, testChannel, m, 0, 120); err != nil {
		t.Fatal(err)
	}

	ttlAfterPush, err := d.client.TTL(ctx, keys.Message(m.ID)).Result()
	if err != nil {
		t.Fatal(err)
	}
	if ttlAfterPush <= 0 {
		t.Fatalf("expected positive ttl after push, got %v", ttlAfterPush)
	}

	messageID, _, err := d.Pop(ctx, testChannel, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if messageID == "" {
		t.Fatal("expected message id from pop")
	}

	ttlAfterPop, err := d.client.TTL(ctx, keys.Message(m.ID)).Result()
	if err != nil {
		t.Fatal(err)
	}
	if ttlAfterPop <= 0 {
		t.Fatalf("expected positive ttl after pop, got %v", ttlAfterPop)
	}

	if err := d.Ack(ctx, testChannel, messageID); err != nil {
		t.Fatal(err)
	}
	ttlAfterAck, err := d.client.TTL(ctx, keys.Message(m.ID)).Result()
	if err != nil {
		t.Fatal(err)
	}
	if ttlAfterAck <= 0 {
		t.Fatalf("expected positive ttl after ack, got %v", ttlAfterAck)
	}
}

func TestGenerateID(t *testing.T) {
	d, _, done := newDriverForTest(t)
	defer done()
	ctx := context.Background()

	id1, err := d.GenerateID(ctx, testChannel)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := d.GenerateID(ctx, testChannel)
	if err != nil {
		t.Fatal(err)
	}
	if id1 == "" || id2 == "" {
		t.Fatal("expected non-empty ids")
	}
	if id1 == id2 {
		t.Fatalf("expected unique ids, got same id=%s", id1)
	}
	if len(id1) != 32 || len(id2) != 32 {
		t.Fatalf("unexpected id length: id1=%s id2=%s", id1, id2)
	}
	if _, err := hex.DecodeString(id1); err != nil {
		t.Fatalf("id1 is not md5 hex: %s", id1)
	}
	if _, err := hex.DecodeString(id2); err != nil {
		t.Fatalf("id2 is not md5 hex: %s", id2)
	}
}

func TestGenerateIDRollover(t *testing.T) {
	d, _, done := newDriverForTest(t)
	defer done()
	ctx := context.Background()
	keys := NewKeys(testChannel)

	if err := d.client.Set(ctx, keys.SequenceKey, fmt.Sprintf("%d", maxMessageSequence), 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := d.client.Set(ctx, keys.SequenceEpoch, "0", 0).Err(); err != nil {
		t.Fatal(err)
	}

	id1, err := d.GenerateID(ctx, testChannel)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := d.GenerateID(ctx, testChannel)
	if err != nil {
		t.Fatal(err)
	}
	if id1 == id2 {
		t.Fatalf("expected unique ids across rollover, got %s", id1)
	}

	seq, err := d.client.Get(ctx, keys.SequenceKey).Result()
	if err != nil {
		t.Fatal(err)
	}
	if seq != "1" {
		t.Fatalf("expected sequence to continue from 0 to 1 after rollover, got %s", seq)
	}
	epoch, err := d.client.Get(ctx, keys.SequenceEpoch).Result()
	if err != nil {
		t.Fatal(err)
	}
	if epoch != "1" {
		t.Fatalf("expected epoch increment after rollover, got %s", epoch)
	}
}
