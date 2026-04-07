package logger_test

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/pkg/logger"
)

// captureLog redirects the standard logger output to a buffer for assertions.
func captureLog(f func()) string {
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)
	f()
	return buf.String()
}

// --- LogMode ---

func TestDefaultLoggerLogModeReturnsSameInterface(t *testing.T) {
	l := logger.Default.LogMode(logger.Warn)
	if l == nil {
		t.Fatal("LogMode returned nil")
	}
}

// --- Info ---

func TestDefaultLoggerInfoWritesAtInfoLevel(t *testing.T) {
	l := logger.Default.LogMode(logger.Info)
	out := captureLog(func() {
		l.Info(context.Background(), "hello %s", "world")
	})
	if !strings.Contains(out, "hello world") {
		t.Fatalf("expected Info output to contain 'hello world', got: %q", out)
	}
}

func TestDefaultLoggerInfoSilencedBelowInfoLevel(t *testing.T) {
	l := logger.Default.LogMode(logger.Warn)
	out := captureLog(func() {
		l.Info(context.Background(), "should not appear")
	})
	if strings.Contains(out, "should not appear") {
		t.Fatalf("expected Info to be suppressed at Warn level, got: %q", out)
	}
}

// --- Warn ---

func TestDefaultLoggerWarnWritesAtWarnLevel(t *testing.T) {
	l := logger.Default.LogMode(logger.Warn)
	out := captureLog(func() {
		l.Warn(context.Background(), "something fishy %d", 42)
	})
	if !strings.Contains(out, "something fishy 42") {
		t.Fatalf("expected Warn output, got: %q", out)
	}
}

func TestDefaultLoggerWarnSilencedAtSilentLevel(t *testing.T) {
	l := logger.Default.LogMode(logger.Silent)
	out := captureLog(func() {
		l.Warn(context.Background(), "should not appear")
	})
	if strings.Contains(out, "should not appear") {
		t.Fatalf("expected Warn to be suppressed at Silent level, got: %q", out)
	}
}

// --- Error ---

func TestDefaultLoggerErrorWritesAtErrorLevel(t *testing.T) {
	l := logger.Default.LogMode(logger.Error)
	out := captureLog(func() {
		l.Error(context.Background(), "something broke: %v", errors.New("boom"))
	})
	if !strings.Contains(out, "something broke: boom") {
		t.Fatalf("expected Error output, got: %q", out)
	}
}

func TestDefaultLoggerErrorSilencedAtSilentLevel(t *testing.T) {
	l := logger.Default.LogMode(logger.Silent)
	out := captureLog(func() {
		l.Error(context.Background(), "should not appear")
	})
	if strings.Contains(out, "should not appear") {
		t.Fatalf("expected Error to be suppressed at Silent level, got: %q", out)
	}
}

// --- Trace ---

func TestDefaultLoggerTraceWritesQueueAndElapsed(t *testing.T) {
	l := logger.Default.LogMode(logger.Info)
	begin := time.Now().Add(-10 * time.Millisecond)
	out := captureLog(func() {
		l.Trace(context.Background(), begin, func() (string, string) {
			return "my-queue", ""
		}, nil)
	})
	if !strings.Contains(out, "queue=my-queue") {
		t.Fatalf("expected Trace to contain queue name, got: %q", out)
	}
}

func TestDefaultLoggerTraceIncludesErrorWhenPresent(t *testing.T) {
	l := logger.Default.LogMode(logger.Info)
	begin := time.Now()
	err := errors.New("handler failed")
	out := captureLog(func() {
		l.Trace(context.Background(), begin, func() (string, string) {
			return "q", ""
		}, err)
	})
	if !strings.Contains(out, "handler failed") {
		t.Fatalf("expected Trace to contain error message, got: %q", out)
	}
}

func TestDefaultLoggerTraceSkipsClosureAtSilentLevel(t *testing.T) {
	l := logger.Default.LogMode(logger.Silent)
	called := false
	captureLog(func() {
		l.Trace(context.Background(), time.Now(), func() (string, string) {
			called = true
			return "q", ""
		}, nil)
	})
	if called {
		t.Fatal("expected fc closure to NOT be called at Silent level")
	}
}

// --- Silent suppresses everything ---

func TestSilentLevelSuppressesAllOutput(t *testing.T) {
	l := logger.Default.LogMode(logger.Silent)
	out := captureLog(func() {
		ctx := context.Background()
		l.Info(ctx, "info msg")
		l.Warn(ctx, "warn msg")
		l.Error(ctx, "error msg")
		l.Trace(ctx, time.Now(), func() (string, string) { return "q", "" }, nil)
	})
	if out != "" {
		t.Fatalf("expected no output at Silent level, got: %q", out)
	}
}

// --- Interface compliance ---

func TestDefaultImplementsInterface(t *testing.T) {
	var _ logger.Interface = logger.Default
}
