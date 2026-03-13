package main

import (
	"context"
	"testing"
	"time"
)

func TestParseArgs(t *testing.T) {
	opts, err := parseArgs([]string{
		"-redis-addr", "1.2.3.4:6379",
		"-channel", "c1",
		"-retry-seconds", "1,2,3",
		"-max-messages", "9",
		"-shutdown-timeout", "12",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.redisAddr != "1.2.3.4:6379" {
		t.Fatalf("expected redis-addr=1.2.3.4:6379, got %s", opts.redisAddr)
	}
	if opts.channel != "c1" {
		t.Fatalf("expected channel=c1, got %s", opts.channel)
	}
	if opts.maxMessages != 9 {
		t.Fatalf("expected max-messages=9, got %d", opts.maxMessages)
	}
	if opts.shutdownTimeout != 12 {
		t.Fatalf("expected shutdown-timeout=12, got %d", opts.shutdownTimeout)
	}
	if len(opts.retrySeconds) != 3 || opts.retrySeconds[0] != 1 || opts.retrySeconds[1] != 2 || opts.retrySeconds[2] != 3 {
		t.Fatalf("expected retry-seconds=[1,2,3], got %v", opts.retrySeconds)
	}
}

func TestParseArgsDefaults(t *testing.T) {
	opts, err := parseArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.redisAddr != "127.0.0.1:6379" {
		t.Fatalf("expected default redis-addr, got %s", opts.redisAddr)
	}
	if opts.concurrent != 10 {
		t.Fatalf("expected default concurrent=10, got %d", opts.concurrent)
	}
	if opts.shutdownTimeout != 30 {
		t.Fatalf("expected default shutdown-timeout=30, got %d", opts.shutdownTimeout)
	}
	if len(opts.retrySeconds) != 1 || opts.retrySeconds[0] != 5 {
		t.Fatalf("expected default retry-seconds=[5], got %v", opts.retrySeconds)
	}
}

func TestParseArgsInvalidRetry(t *testing.T) {
	_, err := parseArgs([]string{"-retry-seconds", "x"})
	if err == nil {
		t.Fatal("expected error for invalid retry-seconds")
	}
}

func TestParseArgsInvalidShutdownTimeout(t *testing.T) {
	_, err := parseArgs([]string{"-shutdown-timeout", "0"})
	if err == nil {
		t.Fatal("expected error for zero shutdown-timeout")
	}
}

func TestRunRedisConnectError(t *testing.T) {
	// Use an unreachable address to trigger connection error
	err := run(context.Background(), []string{"-redis-addr", "127.0.0.1:1"})
	if err == nil {
		t.Fatal("expected redis connect error")
	}
}

func TestRunContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := run(ctx, []string{"-redis-addr", "127.0.0.1:1"})
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func TestRunParseError(t *testing.T) {
	err := run(context.Background(), []string{"-shutdown-timeout", "0"})
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestParseArgsEmptyRetryFallsBack(t *testing.T) {
	opts, err := parseArgs([]string{"-retry-seconds", ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.retrySeconds) != 1 || opts.retrySeconds[0] != 5 {
		t.Fatalf("expected fallback retry-seconds=[5], got %v", opts.retrySeconds)
	}
}

func TestParseArgsRetryWithSpaces(t *testing.T) {
	opts, err := parseArgs([]string{"-retry-seconds", " 10 , 20 , 30 "})
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.retrySeconds) != 3 || opts.retrySeconds[0] != 10 || opts.retrySeconds[2] != 30 {
		t.Fatalf("expected [10,20,30], got %v", opts.retrySeconds)
	}
}

func TestParseArgsNegativeShutdownTimeout(t *testing.T) {
	_, err := parseArgs([]string{"-shutdown-timeout", "-5"})
	if err == nil {
		t.Fatal("expected error for negative shutdown-timeout")
	}
}

func TestRunRedisConnectTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Non-routable address to force timeout
	err := run(ctx, []string{"-redis-addr", "10.255.255.1:6379"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
