package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/pkg/core"
	"github.com/liuxiaozhicn/async-queue-go/pkg/queue"
	"github.com/liuxiaozhicn/async-queue-go/pkg/worker"
	"github.com/redis/go-redis/v9"
)

type workerOptions struct {
	redisAddr       string
	channel         string
	timeout         int
	handleTimeout   int
	retrySeconds    []int
	concurrent      int
	maxMessages     int
	shutdownTimeout int
}

func parseArgs(args []string) (workerOptions, error) {
	fs := flag.NewFlagSet("aq-worker", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := workerOptions{}
	var retry string
	fs.StringVar(&opts.redisAddr, "redis-addr", "127.0.0.1:6379", "redis address")
	fs.StringVar(&opts.channel, "channel", "{queue}", "queue channel")
	fs.IntVar(&opts.timeout, "timeout", 2, "pop timeout seconds")
	fs.IntVar(&opts.handleTimeout, "handle-timeout", 10, "handle timeout seconds")
	fs.StringVar(&retry, "retry-seconds", "5", "retry seconds list, comma separated")
	fs.IntVar(&opts.concurrent, "concurrent", 10, "concurrent worker limit")
	fs.IntVar(&opts.maxMessages, "max-messages", 0, "max messages to consume")
	fs.IntVar(&opts.shutdownTimeout, "shutdown-timeout", 30, "graceful shutdown timeout seconds")

	if err := fs.Parse(args); err != nil {
		return workerOptions{}, err
	}

	parts := strings.Split(strings.TrimSpace(retry), ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		var v int
		if _, err := fmt.Sscanf(p, "%d", &v); err != nil {
			return workerOptions{}, errors.New("invalid retry-seconds")
		}
		opts.retrySeconds = append(opts.retrySeconds, v)
	}
	if len(opts.retrySeconds) == 0 {
		opts.retrySeconds = []int{5}
	}
	if opts.shutdownTimeout <= 0 {
		return workerOptions{}, errors.New("shutdown-timeout must be positive")
	}
	return opts, nil
}

func run(ctx context.Context, args []string) error {
	opts, err := parseArgs(args)
	if err != nil {
		return err
	}

	// Connect to Redis
	client := redis.NewClient(&redis.Options{Addr: opts.redisAddr})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return fmt.Errorf("redis connect: %w", err)
	}
	defer client.Close()

	// Build driver → consumer → worker
	driver := queue.NewRedisDriver(client, opts.channel, opts.timeout, opts.handleTimeout, opts.retrySeconds)
	handler := func(ctx context.Context, m *core.Message) (core.Result, error) {
		return core.ACK, nil
	}
	consumer := queue.NewConsumer(driver, queue.HandlerFunc(handler), opts.concurrent, opts.maxMessages, time.Duration(opts.handleTimeout)*time.Second, "")
	w := worker.NewWorker(consumer)

	// Start worker
	if err := w.Start(ctx); err != nil {
		return err
	}

	// Wait for finish; on signal ctx cancels, then enforce shutdown timeout
	done := make(chan error, 1)
	go func() { done <- w.Wait() }()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return w.Stop(time.Duration(opts.shutdownTimeout) * time.Second)
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		_, _ = os.Stderr.Write(append(b, '\n'))
		os.Exit(1)
	}
}
