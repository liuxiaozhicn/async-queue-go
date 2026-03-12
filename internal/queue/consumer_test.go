package queue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/internal/core"
)

type fakeDriver struct {
	mu sync.Mutex

	popItems []struct {
		data string
		msg  *core.Message
	}
	ackCalls     int
	failCalls    int
	retryCalls   int
	requeueCalls int

	ackErr     error
	failErr    error
	retryErr   error
	requeueErr error

	maxInFlight int32
	inFlight    int32
}

func (f *fakeDriver) Push(context.Context, *core.Message, int) error { return nil }
func (f *fakeDriver) Delete(context.Context, *core.Message) error    { return nil }
func (f *fakeDriver) Pop(context.Context) (string, *core.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.popItems) == 0 {
		return "", nil, nil
	}
	item := f.popItems[0]
	f.popItems = f.popItems[1:]
	return item.data, item.msg, nil
}
func (f *fakeDriver) Ack(context.Context, string) error {
	f.mu.Lock()
	f.ackCalls++
	err := f.ackErr
	f.mu.Unlock()
	return err
}
func (f *fakeDriver) Fail(context.Context, string) error {
	f.mu.Lock()
	f.failCalls++
	err := f.failErr
	f.mu.Unlock()
	return err
}
func (f *fakeDriver) Requeue(context.Context, string) error {
	f.mu.Lock()
	f.requeueCalls++
	err := f.requeueErr
	f.mu.Unlock()
	return err
}
func (f *fakeDriver) Retry(context.Context, *core.Message) error {
	f.mu.Lock()
	f.retryCalls++
	err := f.retryErr
	f.mu.Unlock()
	return err
}
func (f *fakeDriver) Reload(context.Context, string) (int, error) { return 0, nil }
func (f *fakeDriver) Flush(context.Context, string) error         { return nil }
func (f *fakeDriver) Info(context.Context) (Info, error)          { return Info{}, nil }

func TestConsumerResultRouting(t *testing.T) {
	mk := func() *core.Message { return core.NewMessage([]byte(`{}`), 2) }
	cases := []struct {
		name         string
		result       core.Result
		err          error
		wantAck      int
		wantRetry    int
		wantRequeue  int
		wantFail     int
		maxAttempts  int
		initialTries int
	}{
		{name: "ack", result: core.ACK, wantAck: 1},
		{name: "drop", result: core.DROP, wantAck: 1},
		{name: "requeue", result: core.REQUEUE, wantAck: 1, wantRequeue: 1},
		{name: "retry", result: core.RETRY, wantAck: 1, wantRetry: 1},
		{name: "retry over max", result: core.RETRY, wantAck: 1, maxAttempts: 1, initialTries: 1},
		{name: "error retry", err: errors.New("boom"), wantAck: 1, wantRetry: 1},
		{name: "error fail", err: errors.New("boom"), wantFail: 1, maxAttempts: 1, initialTries: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := mk()
			if tc.maxAttempts > 0 {
				msg.MaxAttempts = tc.maxAttempts
			}
			msg.Attempts = tc.initialTries
			d := &fakeDriver{popItems: []struct {
				data string
				msg  *core.Message
			}{{data: "raw", msg: msg}}}
			c := NewConsumer(d, func(context.Context, *core.Message) (core.Result, error) {
				return tc.result, tc.err
			}, 1, 1)
			if err := c.Run(context.Background()); err != nil {
				t.Fatal(err)
			}

			if d.ackCalls != tc.wantAck || d.retryCalls != tc.wantRetry || d.requeueCalls != tc.wantRequeue || d.failCalls != tc.wantFail {
				t.Fatalf("got ack=%d retry=%d requeue=%d fail=%d", d.ackCalls, d.retryCalls, d.requeueCalls, d.failCalls)
			}
		})
	}
}

func TestConsumerMaxMessagesAndConcurrency(t *testing.T) {
	d := &fakeDriver{}
	for i := 0; i < 20; i++ {
		d.popItems = append(d.popItems, struct {
			data string
			msg  *core.Message
		}{data: "x", msg: core.NewMessage([]byte(`{}`), 2)})
	}

	c := NewConsumer(d, func(context.Context, *core.Message) (core.Result, error) {
		in := atomic.AddInt32(&d.inFlight, 1)
		for {
			old := atomic.LoadInt32(&d.maxInFlight)
			if in <= old || atomic.CompareAndSwapInt32(&d.maxInFlight, old, in) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		atomic.AddInt32(&d.inFlight, -1)
		return core.ACK, nil
	}, 3, 7)

	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d.ackCalls != 7 {
		t.Fatalf("expected 7 acks, got %d", d.ackCalls)
	}
	if d.maxInFlight > 3 {
		t.Fatalf("expected max concurrency <=3, got %d", d.maxInFlight)
	}
}

func TestConsumerRunReturnsAggregatedErrors(t *testing.T) {
	d := &fakeDriver{ackErr: errors.New("ack failed")}
	for i := 0; i < 3; i++ {
		d.popItems = append(d.popItems, struct {
			data string
			msg  *core.Message
		}{data: "x", msg: core.NewMessage([]byte(`{}`), 2)})
	}

	c := NewConsumer(d, func(context.Context, *core.Message) (core.Result, error) {
		return core.ACK, nil
	}, 2, 3)

	err := c.Run(context.Background())
	if err == nil {
		t.Fatal("expected aggregated error")
	}
	re, ok := err.(*ConsumerRunError)
	if !ok {
		t.Fatalf("expected ConsumerRunError, got %T", err)
	}
	if re.Count != 3 {
		t.Fatalf("expected error count=3, got %d", re.Count)
	}
	stats := c.Stats()
	if stats.Errors != 3 || stats.Processed != 3 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestConsumerHooksAndStats(t *testing.T) {
	mkMsg := func() *core.Message { return core.NewMessage([]byte(`{}`), 2) }
	d := &fakeDriver{popItems: []struct {
		data string
		msg  *core.Message
	}{
		{data: "a", msg: mkMsg()},
		{data: "b", msg: mkMsg()},
		{data: "c", msg: mkMsg()},
		{data: "d", msg: mkMsg()},
	}}

	ackN, retryN, requeueN, dropN := 0, 0, 0, 0
	results := []core.Result{core.ACK, core.RETRY, core.REQUEUE, core.DROP}
	idx := 0
	c := NewConsumerWithHooks(d, func(context.Context, *core.Message) (core.Result, error) {
		res := results[idx]
		idx++
		return res, nil
	}, 1, 4, ConsumerHooks{
		OnAck: func(context.Context, *core.Message) { ackN++ },
		OnRetry: func(context.Context, *core.Message) {
			retryN++
		},
		OnRequeue: func(context.Context, *core.Message) {
			requeueN++
		},
		OnDrop: func(context.Context, *core.Message) {
			dropN++
		},
	})

	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ackN != 4 || retryN != 1 || requeueN != 1 || dropN != 1 {
		t.Fatalf("unexpected hook counts ack=%d retry=%d requeue=%d drop=%d", ackN, retryN, requeueN, dropN)
	}
	stats := c.Stats()
	if stats.Acked != 4 || stats.Retried != 1 || stats.Requeued != 1 || stats.Dropped != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestConsumerShutdownDrainsInFlight(t *testing.T) {
	d := &fakeDriver{popItems: []struct {
		data string
		msg  *core.Message
	}{
		{data: "a", msg: core.NewMessage([]byte(`{}`), 2)},
		{data: "b", msg: core.NewMessage([]byte(`{}`), 2)},
	}}

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	c := NewConsumer(d, func(context.Context, *core.Message) (core.Result, error) {
		started <- struct{}{}
		<-release
		return core.ACK, nil
	}, 2, 2)

	done := make(chan error, 1)
	go func() {
		done <- c.Run(ctx)
	}()

	<-started
	<-started
	cancel()
	close(release)

	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if d.ackCalls != 2 {
		t.Fatalf("expected in-flight messages to drain, got ack=%d", d.ackCalls)
	}
}
