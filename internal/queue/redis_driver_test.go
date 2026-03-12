package queue

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/internal/core"
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
	defer client.Close()
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
	return core.NewMessage(b, 2)
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
	raw, _ := m.Encode()
	if err := d.client.ZAdd(ctx, d.keys.Delayed, redis.Z{Score: float64(fc.Now().Unix() - 1), Member: raw}).Err(); err != nil {
		t.Fatal(err)
	}

	_, popped, err := d.Pop(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if popped == nil {
		t.Fatal("expected popped message moved from delayed")
	}

	if err := d.client.ZAdd(ctx, d.keys.Reserved, redis.Z{Score: float64(fc.Now().Unix() - 1), Member: raw}).Err(); err != nil {
		t.Fatal(err)
	}
	_, _, err = d.Pop(ctx)
	if err != nil {
		t.Fatal(err)
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
