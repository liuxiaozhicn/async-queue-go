package queue

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
	"github.com/liuxiaozhicn/async-queue-go/pkg/logger"
)

type forwardResult struct {
	delayed int64
	timeout int64
	err     error
}

type scriptedForwardDriver struct {
	mu           sync.Mutex
	results      []forwardResult
	callTimes    []time.Time
	cancel       context.CancelFunc
	cancelOnCall int
}

func (d *scriptedForwardDriver) Ping(context.Context) error { return nil }
func (d *scriptedForwardDriver) Push(context.Context, string, *core.Message, int, int) error {
	return nil
}
func (d *scriptedForwardDriver) Get(context.Context, string, string) (*core.Message, error) {
	return nil, nil
}
func (d *scriptedForwardDriver) Cancel(context.Context, string, string) (bool, error) {
	return false, nil
}
func (d *scriptedForwardDriver) Pop(context.Context, string, time.Duration, time.Duration) (string, *core.Message, error) {
	return "", nil, nil
}
func (d *scriptedForwardDriver) Ack(context.Context, string, string) error { return nil }
func (d *scriptedForwardDriver) Requeue(context.Context, string, string) error {
	return nil
}
func (d *scriptedForwardDriver) Retry(context.Context, string, string, int) (bool, error) {
	return false, nil
}
func (d *scriptedForwardDriver) Drop(context.Context, string, string) error { return nil }
func (d *scriptedForwardDriver) Fail(context.Context, string, string) error { return nil }
func (d *scriptedForwardDriver) Reload(context.Context, string, string) (int, error) {
	return 0, nil
}
func (d *scriptedForwardDriver) Flush(context.Context, string, string) error { return nil }
func (d *scriptedForwardDriver) Info(context.Context, string) (Info, error)  { return Info{}, nil }

func (d *scriptedForwardDriver) ForwardMessages(context.Context, string) (int64, int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.callTimes = append(d.callTimes, time.Now())
	callNum := len(d.callTimes)
	if d.cancel != nil && d.cancelOnCall > 0 && callNum == d.cancelOnCall {
		d.cancel()
	}

	if callNum <= len(d.results) {
		res := d.results[callNum-1]
		return res.delayed, res.timeout, res.err
	}
	return 0, 0, nil
}

func (d *scriptedForwardDriver) Times() []time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()

	out := make([]time.Time, len(d.callTimes))
	copy(out, d.callTimes)
	return out
}

type captureLogger struct {
	mu      sync.Mutex
	errors  []string
	infos   []string
	errorCh chan string
}

func newCaptureLogger() *captureLogger {
	return &captureLogger{
		errorCh: make(chan string, 16),
	}
}

func (l *captureLogger) LogMode(logger.LogLevel) logger.Interface { return l }
func (l *captureLogger) Info(_ context.Context, msg string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.infos = append(l.infos, fmt.Sprintf(msg, args...))
}

func (l *captureLogger) Warn(_ context.Context, msg string, args ...interface{}) {
	l.Info(context.Background(), msg, args...)
}

func (l *captureLogger) Error(_ context.Context, msg string, args ...interface{}) {
	formatted := fmt.Sprintf(msg, args...)
	l.mu.Lock()
	l.errors = append(l.errors, formatted)
	l.mu.Unlock()
	l.errorCh <- formatted
}

func (l *captureLogger) Trace(context.Context, time.Time, func() (string, string), error) {}

func (l *captureLogger) ErrorLogs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	out := make([]string, len(l.errors))
	copy(out, l.errors)
	return out
}

func TestForwarderErrorRetryBackoffIncreases(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log := newCaptureLogger()
	driver := &scriptedForwardDriver{
		results: []forwardResult{
			{err: errors.New("redis down")},
			{err: errors.New("redis down")},
			{err: errors.New("redis down")},
		},
	}
	f := &forwarder{
		driver:               driver,
		queueName:            "test",
		channel:              "ch",
		logger:               log,
		errorRetryBackoff:    5 * time.Millisecond,
		errorRetryMaxBackoff: 20 * time.Millisecond,
		idlePollInterval:     time.Hour,
		busyPollInterval:     time.Hour,
	}

	done := make(chan error, 1)
	go func() {
		done <- f.Run(ctx)
	}()

	for i := 0; i < 3; i++ {
		<-log.errorCh
	}
	cancel()

	if err := <-done; err != nil {
		t.Fatal(err)
	}

	logs := log.ErrorLogs()
	if len(logs) != 3 {
		t.Fatalf("expected 3 error logs, got %d", len(logs))
	}
	if want := "retrying in 5ms"; !contains(logs[0], want) {
		t.Fatalf("expected first retry log to contain %q, got %q", want, logs[0])
	}
	if want := "retrying in 10ms"; !contains(logs[1], want) {
		t.Fatalf("expected second retry log to contain %q, got %q", want, logs[1])
	}
	if want := "retrying in 20ms"; !contains(logs[2], want) {
		t.Fatalf("expected third retry log to contain %q, got %q", want, logs[2])
	}
}

func TestForwarderErrorRetryBackoffResetsAfterSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log := newCaptureLogger()
	driver := &scriptedForwardDriver{
		results: []forwardResult{
			{err: errors.New("redis down")},
			{err: errors.New("redis down")},
			{},
			{err: errors.New("redis down")},
		},
	}
	f := &forwarder{
		driver:               driver,
		queueName:            "test",
		channel:              "ch",
		logger:               log,
		errorRetryBackoff:    5 * time.Millisecond,
		errorRetryMaxBackoff: 20 * time.Millisecond,
		idlePollInterval:     5 * time.Millisecond,
		busyPollInterval:     time.Hour,
	}

	done := make(chan error, 1)
	go func() {
		done <- f.Run(ctx)
	}()

	for i := 0; i < 3; i++ {
		<-log.errorCh
	}
	cancel()

	if err := <-done; err != nil {
		t.Fatal(err)
	}

	logs := log.ErrorLogs()
	if len(logs) != 3 {
		t.Fatalf("expected 3 error logs, got %d", len(logs))
	}
	if want := "retrying in 5ms"; !contains(logs[0], want) {
		t.Fatalf("expected first retry log to contain %q, got %q", want, logs[0])
	}
	if want := "retrying in 10ms"; !contains(logs[1], want) {
		t.Fatalf("expected second retry log to contain %q, got %q", want, logs[1])
	}
	if want := "retrying in 5ms"; !contains(logs[2], want) {
		t.Fatalf("expected retry backoff reset log to contain %q, got %q", want, logs[2])
	}
}

func TestForwarderSwitchesBetweenBusyAndIdlePollIntervals(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	driver := &scriptedForwardDriver{
		results: []forwardResult{
			{delayed: 1},
			{},
			{timeout: 1},
		},
		cancel:       cancel,
		cancelOnCall: 3,
	}
	f := &forwarder{
		driver:               driver,
		queueName:            "test",
		channel:              "ch",
		logger:               logger.Default.LogMode(logger.Silent),
		idlePollInterval:     25 * time.Millisecond,
		busyPollInterval:     5 * time.Millisecond,
		errorRetryBackoff:    time.Second,
		errorRetryMaxBackoff: time.Second,
	}

	if err := f.Run(ctx); err != nil {
		t.Fatal(err)
	}

	times := driver.Times()
	if len(times) != 3 {
		t.Fatalf("expected 3 forward calls, got %d", len(times))
	}

	busyGap := times[1].Sub(times[0])
	idleGap := times[2].Sub(times[1])
	if busyGap < 3*time.Millisecond {
		t.Fatalf("expected busy poll gap >= 3ms, got %v", busyGap)
	}
	if idleGap < 15*time.Millisecond {
		t.Fatalf("expected idle poll gap >= 15ms, got %v", idleGap)
	}
	if idleGap <= busyGap {
		t.Fatalf("expected idle poll gap > busy poll gap, got busy=%v idle=%v", busyGap, idleGap)
	}
}

func contains(s string, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) && strings.Contains(s, substr))
}
