package queue

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
	"github.com/redis/go-redis/v9"
)

type fakeClock struct {
	now time.Time
}

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
	d := NewRedisDriverWithClock(client, "test", 1, 1, []int{1, 2, 3}, fc)
	cleanup := func() {
		_ = client.Close()
	}
	return d, fc, cleanup
}

// newDriverForTestWithClient creates a driver with external Redis client for testing
func newDriverForTestWithClient(t *testing.T, client redis.UniversalClient) (*RedisDriver, *fakeClock, func()) {
	t.Helper()
	fc := &fakeClock{now: time.Unix(1700000000, 0)}
	d := NewRedisDriverWithClock(client, "test", 1, 1, []int{1, 2, 3}, fc)
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

func TestPushDeleteInfo(t *testing.T) {
	d, _, done := newDriverForTest(t)
	defer done()
	ctx := context.Background()

	m1 := mustMessage(t, 1)
	if err := d.Push(ctx, m1, 0); err != nil {
		t.Fatal(err)
	}
	m2 := mustMessage(t, 2)
	if err := d.Push(ctx, m2, 10); err != nil {
		t.Fatal(err)
	}

	info, err := d.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Waiting != 1 || info.Delayed != 1 {
		t.Fatalf("unexpected info: %+v", info)
	}

	if err := d.Delete(ctx, m2); err != nil {
		t.Fatal(err)
	}
	info, _ = d.Info(ctx)
	if info.Delayed != 0 {
		t.Fatalf("delete failed, info=%+v", info)
	}
}

func TestPopAckFailMove(t *testing.T) {
	d, _, done := newDriverForTest(t)
	defer done()
	ctx := context.Background()

	m := mustMessage(t, 1)
	if err := d.Push(ctx, m, 0); err != nil {
		t.Fatal(err)
	}
	data, _, err := d.Pop(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if data == "" {
		t.Fatal("expected data")
	}

	info, _ := d.Info(ctx)
	if info.Reserved != 1 {
		t.Fatalf("expected reserved=1 got %+v", info)
	}

	if err := d.Ack(ctx, data); err != nil {
		t.Fatal(err)
	}
	info, _ = d.Info(ctx)
	if info.Reserved != 0 {
		t.Fatalf("expected reserved=0 got %+v", info)
	}

	if err := d.Push(ctx, mustMessage(t, 2), 0); err != nil {
		t.Fatal(err)
	}
	data2, _, err := d.Pop(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Fail(ctx, data2); err != nil {
		t.Fatal(err)
	}
	info, _ = d.Info(ctx)
	if info.Failed != 1 || info.Reserved != 0 {
		t.Fatalf("unexpected fail info: %+v", info)
	}
}

func TestMoveDelayedAndReservedTimeoutAndReloadFlush(t *testing.T) {
	d, fc, done := newDriverForTest(t)
	defer done()
	ctx := context.Background()

	m := mustMessage(t, 5)
	if err := d.storeMessage(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := d.client.ZAdd(ctx, d.keys.Delayed, redis.Z{Score: float64(fc.Now().Unix() - 1), Member: m.ID}).Err(); err != nil {
		t.Fatal(err)
	}

	forwardedDelayed, forwardedTimeout, err := d.ForwardMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if forwardedDelayed == 0 {
		t.Fatal("expected delayed message moved to waiting")
	}
	if forwardedTimeout != 0 {
		t.Fatalf("expected no timeout move yet, got %d", forwardedTimeout)
	}

	if err := d.client.ZAdd(ctx, d.keys.Reserved, redis.Z{Score: float64(fc.Now().Unix() - 1), Member: m.ID}).Err(); err != nil {
		t.Fatal(err)
	}
	_, forwardedTimeout, err = d.ForwardMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if forwardedTimeout == 0 {
		t.Fatal("expected expired reserved message moved to timeout")
	}
	info, _ := d.Info(ctx)
	if info.Timeout == 0 {
		t.Fatalf("expected timeout to increase, info=%+v", info)
	}

	if _, err := d.Reload(ctx, "bad"); err == nil {
		t.Fatal("expected reload invalid queue error")
	}

	n, err := d.Reload(ctx, "timeout")
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected timeout messages to be reloaded")
	}

	if err := d.Flush(ctx, "waiting"); err != nil {
		t.Fatal(err)
	}
	info, _ = d.Info(ctx)
	if info.Waiting != 0 {
		t.Fatalf("expected waiting=0 after flush, got %+v", info)
	}
}

func TestMessageTTLPreservedAcrossStatusUpdates(t *testing.T) {
	d, _, done := newDriverForTest(t)
	defer done()
	ctx := context.Background()

	d.SetMessageTTL(120)
	m := mustMessage(t, 42)
	if err := d.Push(ctx, m, 0); err != nil {
		t.Fatal(err)
	}

	ttlAfterPush, err := d.client.TTL(ctx, d.keys.Message(m.ID)).Result()
	if err != nil {
		t.Fatal(err)
	}
	if ttlAfterPush <= 0 {
		t.Fatalf("expected positive ttl after push, got %v", ttlAfterPush)
	}

	messageID, _, err := d.Pop(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if messageID == "" {
		t.Fatal("expected message id from pop")
	}

	ttlAfterPop, err := d.client.TTL(ctx, d.keys.Message(m.ID)).Result()
	if err != nil {
		t.Fatal(err)
	}
	if ttlAfterPop <= 0 {
		t.Fatalf("expected positive ttl after pop, got %v", ttlAfterPop)
	}

	if err := d.Ack(ctx, messageID); err != nil {
		t.Fatal(err)
	}
	ttlAfterAck, err := d.client.TTL(ctx, d.keys.Message(m.ID)).Result()
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

	id1, err := d.GenerateID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := d.GenerateID(ctx)
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

	if err := d.client.Set(ctx, d.keys.SequenceKey, fmt.Sprintf("%d", maxMessageSequence), 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := d.client.Set(ctx, d.keys.SequenceEpoch, "0", 0).Err(); err != nil {
		t.Fatal(err)
	}

	id1, err := d.GenerateID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := d.GenerateID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if id1 == id2 {
		t.Fatalf("expected unique ids across rollover, got %s", id1)
	}

	seq, err := d.client.Get(ctx, d.keys.SequenceKey).Result()
	if err != nil {
		t.Fatal(err)
	}
	if seq != "1" {
		t.Fatalf("expected sequence to continue from 0 to 1 after rollover, got %s", seq)
	}
	epoch, err := d.client.Get(ctx, d.keys.SequenceEpoch).Result()
	if err != nil {
		t.Fatal(err)
	}
	if epoch != "1" {
		t.Fatalf("expected epoch increment after rollover, got %s", epoch)
	}
}
