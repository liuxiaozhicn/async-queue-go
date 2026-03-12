package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/internal/core"
	"github.com/liuxiaozhicn/async-queue-go/internal/queue"
)

type fakeWorkerDriver struct{}

func (f *fakeWorkerDriver) Push(context.Context, *core.Message, int) error { return nil }
func (f *fakeWorkerDriver) Delete(context.Context, *core.Message) error    { return nil }
func (f *fakeWorkerDriver) Pop(context.Context) (string, *core.Message, error) {
	return "", nil, nil
}
func (f *fakeWorkerDriver) Ack(context.Context, string) error           { return nil }
func (f *fakeWorkerDriver) Fail(context.Context, string) error          { return nil }
func (f *fakeWorkerDriver) Requeue(context.Context, string) error       { return nil }
func (f *fakeWorkerDriver) Retry(context.Context, *core.Message) error  { return nil }
func (f *fakeWorkerDriver) Reload(context.Context, string) (int, error) { return 0, nil }
func (f *fakeWorkerDriver) Flush(context.Context, string) error         { return nil }
func (f *fakeWorkerDriver) Info(context.Context) (queue.Info, error)    { return queue.Info{}, nil }

type fakeRuntime struct {
	startCalled         bool
	waitCalled          bool
	runWithSignalCalled bool
	waitErr             error
	runWithSignalErr    error
	lastTimeout         time.Duration
}

func (f *fakeRuntime) Start(context.Context) error {
	f.startCalled = true
	return nil
}

func (f *fakeRuntime) Wait() error {
	f.waitCalled = true
	return f.waitErr
}

func (f *fakeRuntime) RunWithSignals(_ context.Context, timeout time.Duration) error {
	f.runWithSignalCalled = true
	f.lastTimeout = timeout
	return f.runWithSignalErr
}

func TestParseWorkerArgs(t *testing.T) {
	opts, err := parseWorkerArgs([]string{"-redis-addr", "1.2.3.4:6379", "-channel", "c1", "-retry-seconds", "1,2,3", "-max-messages", "9", "-shutdown-timeout", "12"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.redisAddr != "1.2.3.4:6379" || opts.channel != "c1" || opts.maxMessages != 9 || opts.shutdownTimeout != 12 {
		t.Fatalf("unexpected opts: %+v", opts)
	}
	if len(opts.retrySeconds) != 3 || opts.retrySeconds[2] != 3 {
		t.Fatalf("unexpected retry seconds: %#v", opts.retrySeconds)
	}
}

func TestRunWorkerCallsRuntime(t *testing.T) {
	origDriver := newWorkerDriver
	origRuntime := newWorkerRuntime
	defer func() {
		newWorkerDriver = origDriver
		newWorkerRuntime = origRuntime
	}()

	newWorkerDriver = func(workerOptions) (queue.Driver, error) {
		return &fakeWorkerDriver{}, nil
	}
	fr := &fakeRuntime{}
	newWorkerRuntime = func(queue.Driver, int, int) workerRuntime { return fr }

	if err := runWorker(context.Background(), []string{"-max-messages", "1"}); err != nil {
		t.Fatal(err)
	}
	if !fr.startCalled || !fr.waitCalled {
		t.Fatal("expected runtime start and wait to be called")
	}
}

func TestParseWorkerArgsInvalidRetry(t *testing.T) {
	_, err := parseWorkerArgs([]string{"-retry-seconds", "x"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseWorkerArgsInvalidShutdownTimeout(t *testing.T) {
	_, err := parseWorkerArgs([]string{"-shutdown-timeout", "0"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunWorkerDriverError(t *testing.T) {
	origDriver := newWorkerDriver
	defer func() { newWorkerDriver = origDriver }()
	newWorkerDriver = func(workerOptions) (queue.Driver, error) {
		return nil, errors.New("dial error")
	}
	if err := runWorker(context.Background(), nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestRunWorkerWithSignals(t *testing.T) {
	origDriver := newWorkerDriver
	origRuntime := newWorkerRuntime
	defer func() {
		newWorkerDriver = origDriver
		newWorkerRuntime = origRuntime
	}()

	newWorkerDriver = func(workerOptions) (queue.Driver, error) {
		return &fakeWorkerDriver{}, nil
	}
	fr := &fakeRuntime{}
	newWorkerRuntime = func(queue.Driver, int, int) workerRuntime { return fr }

	if err := runWorkerWithSignals([]string{"-max-messages", "1"}); err != nil {
		t.Fatal(err)
	}
	if !fr.runWithSignalCalled {
		t.Fatal("expected runtime RunWithSignals to be called")
	}
	if fr.lastTimeout != 30*time.Second {
		t.Fatalf("expected timeout=30s, got %v", fr.lastTimeout)
	}
}

func TestRunWorkerWithSignalsShutdownTimeout(t *testing.T) {
	origDriver := newWorkerDriver
	origRuntime := newWorkerRuntime
	defer func() {
		newWorkerDriver = origDriver
		newWorkerRuntime = origRuntime
	}()

	newWorkerDriver = func(workerOptions) (queue.Driver, error) {
		return &fakeWorkerDriver{}, nil
	}
	fr := &fakeRuntime{runWithSignalErr: errors.New("worker shutdown timeout exceeded")}
	newWorkerRuntime = func(queue.Driver, int, int) workerRuntime { return fr }

	err := runWorkerWithSignals([]string{"-shutdown-timeout", "1"})
	if err == nil || err.Error() != "worker shutdown timeout exceeded" {
		t.Fatalf("expected shutdown timeout error, got %v", err)
	}
}
